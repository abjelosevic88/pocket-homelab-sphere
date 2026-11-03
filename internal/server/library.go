package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "golang.org/x/image/webp" // gallery pictures and covers

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/media"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// The library folder: every video becomes a scene; a folder (or .zip) of
// pictures without videos becomes a gallery. A picture with the video's name
// next to it is its cover; a yt-dlp .info.json or a Kodi .nfo with the
// video's name fills in title, date, description, studio and tags.

var videoExts = map[string]bool{".mp4": true, ".m4v": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true, ".webm": true,
	".mpg": true, ".mpeg": true, ".ts": true, ".m2ts": true, ".mts": true, ".flv": true, ".3gp": true, ".ogv": true, ".vob": true, ".divx": true}

var imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}

var reSample = regexp.MustCompile(`(?i)(^|[^a-z])sample([^a-z]|$)`)

func isVideo(name string) bool { return videoExts[strings.ToLower(filepath.Ext(name))] }
func isImage(name string) bool { return imageExts[strings.ToLower(filepath.Ext(name))] }

// skipDir is true for folders the scan never enters.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "@eaDir" || name == "#recycle" || name == "$RECYCLE.BIN" || name == "lost+found"
}

// libPath turns a library-relative path into a real one inside the library.
func (s *Server) libPath(rel string) (string, error) {
	clean := filepath.Clean("/" + rel)
	if clean == "/" {
		return "", errors.New("empty path")
	}
	return filepath.Join(s.cfg.LibraryDir, clean), nil
}

func (s *Server) scenePath(sc *store.Scene) string {
	p, _ := s.libPath(sc.Path)
	return p
}

func (s *Server) genPath(kind string, id int64, ext string) string {
	return filepath.Join(s.cfg.GeneratedDir, kind, fmt.Sprintf("%d%s", id, ext))
}

func (s *Server) wakeScan() {
	select {
	case s.scanWake <- struct{}{}:
	default:
	}
}

func (s *Server) wakeWork() {
	select {
	case s.workWake <- struct{}{}:
	default:
	}
}

type scanStatus struct {
	Running   bool   `json:"running"`
	At        string `json:"at"`
	Ms        int64  `json:"ms"`
	Scenes    int    `json:"scenes"`
	Added     int    `json:"added"`
	Moved     int    `json:"moved"`
	Missing   int    `json:"missing"`
	Galleries int    `json:"galleries"`
	Error     string `json:"error"`
}

func (s *Server) scanState() scanStatus {
	s.infoMu.Lock()
	defer s.infoMu.Unlock()
	return s.scanInfo
}

// scanner looks at the library on start, every SCAN_INTERVAL and when woken.
func (s *Server) scanner(ctx context.Context) {
	var tick <-chan time.Time
	if s.cfg.ScanInterval > 0 {
		t := time.NewTicker(s.cfg.ScanInterval)
		defer t.Stop()
		tick = t.C
	}
	for {
		if err := s.scanAll(ctx, nil); err != nil && ctx.Err() == nil {
			s.log.Warn("library scan failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick:
		case <-s.scanWake:
		}
	}
}

type found struct {
	rel   string
	size  int64
	mtime string
}

// scanAll reads the library folder and brings the database in line.
// addedBy names who put a file there (downloads), by library path.
func (s *Server) scanAll(ctx context.Context, addedBy map[string]int64) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	start := time.Now()
	s.infoMu.Lock()
	s.scanInfo.Running = true
	s.infoMu.Unlock()
	st := scanStatus{}
	defer func() {
		st.Ms = time.Since(start).Milliseconds()
		st.At = store.Now()
		s.infoMu.Lock()
		s.scanInfo = st
		s.infoMu.Unlock()
	}()

	root := s.cfg.LibraryDir
	var videos []found
	pictures := map[string][]found{} // folder → its pictures
	hasVideo := map[string]bool{}
	var zips []found
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // an unreadable folder: skip it
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return nil
		}
		f := found{rel, info.Size(), info.ModTime().UTC().Format(time.RFC3339)}
		switch {
		case isVideo(d.Name()):
			if reSample.MatchString(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))) && info.Size() < 200<<20 {
				return nil // a release's sample clip
			}
			videos = append(videos, f)
			hasVideo[path.Dir(rel)] = true
		case isImage(d.Name()):
			pictures[path.Dir(rel)] = append(pictures[path.Dir(rel)], f)
		case strings.EqualFold(filepath.Ext(d.Name()), ".zip"):
			zips = append(zips, f)
		}
		return nil
	})
	if err != nil {
		st.Error = err.Error()
		return err
	}
	if len(videos) == 0 {
		// an unmounted share looks like an empty folder: never mark the whole library missing
		if entries, _ := os.ReadDir(root); len(entries) == 0 {
			st.Error = "the library folder is empty (not mounted?)"
		}
	}

	idx, err := s.store.AllSceneFiles(ctx)
	if err != nil {
		return err
	}
	byPath := map[string]*store.SceneIndex{}
	for i := range idx {
		byPath[idx[i].Path] = &idx[i]
	}
	seen := map[string]bool{}
	var newIDs []int64
	for _, f := range videos {
		seen[f.rel] = true
		sf := store.SceneFile{Path: f.rel, SizeBytes: f.size, Mtime: f.mtime}
		if x := byPath[f.rel]; x != nil {
			if x.Trashed {
				continue
			}
			if x.SizeBytes != f.size {
				h, _ := media.OSHash(filepath.Join(root, f.rel))
				_ = s.store.SceneFileChanged(ctx, x.ID, sf, h)
			} else if x.MissingSince != "" {
				_ = s.store.SetSceneMissing(ctx, x.ID, false)
			}
			continue
		}
		h, err := media.OSHash(filepath.Join(root, f.rel))
		if err != nil {
			continue // still being written, or unreadable
		}
		// renamed or moved: the same file (hash and size) whose old place is gone
		moved := false
		for i := range idx {
			x := &idx[i]
			if x.OSHash == h && x.SizeBytes == f.size && !x.Trashed && !seen[x.Path] && !fileExists(filepath.Join(root, x.Path)) {
				if err := s.store.MoveScene(ctx, x.ID, sf); err == nil {
					s.moveGeneratedSidecars(x.Path, f.rel)
					x.Path = f.rel
					moved = true
					st.Moved++
				}
				break
			}
		}
		if moved {
			continue
		}
		id, err := s.store.AddScene(ctx, sf, h, addedBy[f.rel])
		if err != nil {
			s.log.Warn("scan: could not add", "file", f.rel, "err", err)
			continue
		}
		newIDs = append(newIDs, id)
		st.Added++
		// the name alone often says it is VR ("…VR180…", "…Oculus…"); the
		// frame size refines it once the file has been read
		if sh := media.DetectShape(f.rel, 0, 0, "", ""); sh.IsVR() {
			_ = s.store.SetVR(ctx, id, sh.Projection, sh.Stereo, sh.FOV)
		}
	}
	if st.Error == "" {
		for i := range idx {
			x := &idx[i]
			if !seen[x.Path] && !x.Trashed && x.MissingSince == "" {
				_ = s.store.SetSceneMissing(ctx, x.ID, true)
				st.Missing++
			}
		}
	}
	for _, id := range newIDs {
		s.readSidecars(ctx, id)
	}
	st.Scenes = len(videos)

	// galleries: folders of pictures without a video, and zips of pictures
	gidx, err := s.store.AllGalleries(ctx)
	if err != nil {
		return err
	}
	gByPath := map[string]store.GalleryIndex{}
	for _, g := range gidx {
		gByPath[g.Path] = g
	}
	gSeen := map[string]bool{}
	for dir, pics := range pictures {
		if hasVideo[dir] || len(pics) < 3 || dir == "." {
			continue
		}
		sort.Slice(pics, func(i, j int) bool { return naturalLess(pics[i].rel, pics[j].rel) })
		mtime := ""
		for _, p := range pics {
			mtime = max(mtime, p.mtime)
		}
		gSeen[dir] = true
		if g, ok := gByPath[dir]; ok && g.Mtime == mtime && !g.Missing {
			continue
		}
		var ims []store.ImageFile
		for _, p := range pics {
			w, h := imageSize(filepath.Join(root, p.rel))
			ims = append(ims, store.ImageFile{Name: path.Base(p.rel), SizeBytes: p.size, Width: w, Height: h})
		}
		if _, _, err := s.store.SyncGallery(ctx, dir, false, mtime, ims); err == nil {
			st.Galleries++
		}
	}
	for _, z := range zips {
		gSeen[z.rel] = true
		if g, ok := gByPath[z.rel]; ok && g.Mtime == z.mtime && !g.Missing {
			continue
		}
		ims, err := zipImages(filepath.Join(root, z.rel))
		if err != nil || len(ims) < 3 {
			delete(gSeen, z.rel)
			continue
		}
		if _, _, err := s.store.SyncGallery(ctx, z.rel, true, z.mtime, ims); err == nil {
			st.Galleries++
		}
	}
	if st.Error == "" {
		for _, g := range gidx {
			if !gSeen[g.Path] && !g.Missing {
				_ = s.store.SetGalleryMissing(ctx, g.ID, true)
			}
		}
	}
	if len(newIDs) > 0 || st.Moved > 0 {
		s.log.Info("library scan", "added", st.Added, "moved", st.Moved, "missing", st.Missing, "ms", time.Since(start).Milliseconds())
	}
	s.wakeWork()
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func imageSize(p string) (int, int) {
	f, err := os.Open(p)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func zipImages(p string) ([]store.ImageFile, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var out []store.ImageFile
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !isImage(f.Name) || strings.HasPrefix(path.Base(f.Name), ".") || strings.Contains(f.Name, "__MACOSX") {
			continue
		}
		out = append(out, store.ImageFile{Name: f.Name, SizeBytes: int64(f.UncompressedSize64)})
	}
	sort.Slice(out, func(i, j int) bool { return naturalLess(out[i].Name, out[j].Name) })
	return out, nil
}

var reDigits = regexp.MustCompile(`\d+|\D+`)

// naturalLess sorts "img2" before "img10".
func naturalLess(a, b string) bool {
	pa, pb := reDigits.FindAllString(strings.ToLower(a), -1), reDigits.FindAllString(strings.ToLower(b), -1)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		da, db := pa[i][0] >= '0' && pa[i][0] <= '9', pb[i][0] >= '0' && pb[i][0] <= '9'
		if da && db {
			x, y := strings.TrimLeft(pa[i], "0"), strings.TrimLeft(pb[i], "0")
			if len(x) != len(y) {
				return len(x) < len(y)
			}
			return x < y
		}
		return pa[i] < pb[i]
	}
	return len(pa) < len(pb)
}

// moveGeneratedSidecars is a no-op today: generated files are named by id.
func (s *Server) moveGeneratedSidecars(_, _ string) {}

// ---------- sidecars ----------

// sidecar returns the path of a file next to the video with the same base
// name and one of the extensions (” = none).
func sidecar(video string, exts ...string) string {
	base := strings.TrimSuffix(video, filepath.Ext(video))
	for _, e := range exts {
		for _, cand := range []string{base + e, base + strings.ToUpper(e)} {
			if fileExists(cand) {
				return cand
			}
		}
	}
	return ""
}

// coverSidecar finds a picture for a video: same name, or the folder's
// poster/cover/folder/thumb when the video is alone in its folder.
func coverSidecar(video string) string {
	if p := sidecar(video, ".jpg", ".jpeg", ".png", ".webp", "-poster.jpg", "-thumb.jpg", ".tbn"); p != "" {
		return p
	}
	dir := filepath.Dir(video)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	videos := 0
	for _, e := range entries {
		if !e.IsDir() && isVideo(e.Name()) {
			videos++
		}
	}
	if videos != 1 {
		return ""
	}
	for _, name := range []string{"poster", "cover", "folder", "thumb", "fanart"} {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			if p := filepath.Join(dir, name+ext); fileExists(p) {
				return p
			}
		}
	}
	return ""
}

// ytInfo is the part of a yt-dlp .info.json that is used.
type ytInfo struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	UploadDate  string   `json:"upload_date"`
	Uploader    string   `json:"uploader"`
	Channel     string   `json:"channel"`
	WebpageURL  string   `json:"webpage_url"`
	Tags        []string `json:"tags"`
	Categories  []string `json:"categories"`
	Cast        []string `json:"cast"`
	Thumbnail   string   `json:"thumbnail"`
}

// kodiNFO is the part of a Kodi/Jellyfin movie .nfo that is used.
type kodiNFO struct {
	XMLName   xml.Name `xml:"movie"`
	Title     string   `xml:"title"`
	Plot      string   `xml:"plot"`
	Premiered string   `xml:"premiered"`
	Aired     string   `xml:"aired"`
	Studio    string   `xml:"studio"`
	Director  string   `xml:"director"`
	Genres    []string `xml:"genre"`
	Tags      []string `xml:"tag"`
	Actors    []struct {
		Name string `xml:"name"`
	} `xml:"actor"`
}

// readSidecars fills a new scene from the files next to it: the cover, a
// yt-dlp info file or a Kodi nfo. Facts already set are kept.
func (s *Server) readSidecars(ctx context.Context, id int64) {
	sc, err := s.store.GetScene(ctx, 0, id)
	if err != nil {
		return
	}
	full := s.scenePath(sc)
	if sc.CoverPath == "" {
		if p := coverSidecar(full); p != "" {
			if b, err := os.ReadFile(p); err == nil {
				_ = s.setSceneCover(ctx, sc, b, "sidecar")
			}
		}
	}
	var st store.Settings
	if sc.AddedBy != nil {
		st, _ = s.store.GetSettings(ctx, *sc.AddedBy)
	} else {
		st, _ = s.store.GetSettings(ctx, s.store.OwnerID(ctx))
	}
	if p := sidecar(full, ".info.json"); p != "" {
		var yi ytInfo
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &yi) == nil {
			s.applyYTInfo(ctx, sc, &yi, st)
		}
		return
	}
	if p := sidecar(full, ".nfo"); p != "" {
		var n kodiNFO
		if b, err := os.ReadFile(p); err == nil && xml.Unmarshal(b, &n) == nil && n.Title != "" {
			s.applyNFO(ctx, sc, &n)
		}
	}
}

func (s *Server) applyYTInfo(ctx context.Context, sc *store.Scene, yi *ytInfo, st store.Settings) {
	p := store.ScenePatch{}
	if sc.Title == "" && yi.Title != "" {
		p.Title = strp(yi.Title)
	}
	if sc.Details == "" && yi.Description != "" {
		p.Details = strp(oneLineKeepBreaks(yi.Description, 4000))
	}
	if sc.Date == "" && len(yi.UploadDate) == 8 {
		p.Date = strp(yi.UploadDate[:4] + "-" + yi.UploadDate[4:6] + "-" + yi.UploadDate[6:])
	}
	if yi.WebpageURL != "" {
		urls := append([]string{}, sc.URLs...)
		urls = append(urls, yi.WebpageURL)
		p.URLs = &urls
		if sc.SourceURL == "" {
			p.SourceURL = strp(yi.WebpageURL)
		}
	}
	uploader := firstNonEmpty(yi.Channel, yi.Uploader)
	if sc.StudioID == nil && st.UploaderStudio && uploader != "" && uploader != "NA" {
		if id, _, err := s.store.EnsureStudio(ctx, uploader); err == nil {
			sid := &id
			p.StudioID = &sid
		}
	}
	_ = s.store.UpdateScene(ctx, sc.ID, p)
	var tags, perfs []int64
	if st.SiteTags {
		names := append(append([]string{}, yi.Categories...), yi.Tags...)
		for i, n := range names {
			if i >= 20 {
				break
			}
			if n = strings.TrimSpace(n); n != "" && len(n) <= 40 {
				if id, _, err := s.store.EnsureTag(ctx, n); err == nil {
					tags = append(tags, id)
				}
			}
		}
	}
	for _, n := range yi.Cast {
		if n = strings.TrimSpace(n); n != "" {
			if id, _, err := s.store.EnsurePerformer(ctx, n); err == nil {
				perfs = append(perfs, id)
			}
		}
	}
	_ = s.store.AddSceneRelations(ctx, sc.ID, perfs, tags)
}

func (s *Server) applyNFO(ctx context.Context, sc *store.Scene, n *kodiNFO) {
	p := store.ScenePatch{}
	if sc.Title == "" {
		p.Title = strp(strings.TrimSpace(n.Title))
	}
	if sc.Details == "" && n.Plot != "" {
		p.Details = strp(strings.TrimSpace(n.Plot))
	}
	if d := firstNonEmpty(n.Premiered, n.Aired); sc.Date == "" && validLooseDate(d) {
		p.Date = &d
	}
	if sc.Director == "" && n.Director != "" {
		p.Director = strp(n.Director)
	}
	if sc.StudioID == nil && strings.TrimSpace(n.Studio) != "" {
		if id, _, err := s.store.EnsureStudio(ctx, n.Studio); err == nil {
			sid := &id
			p.StudioID = &sid
		}
	}
	_ = s.store.UpdateScene(ctx, sc.ID, p)
	var tags, perfs []int64
	for _, t := range append(n.Genres, n.Tags...) {
		if t = strings.TrimSpace(t); t != "" {
			if id, _, err := s.store.EnsureTag(ctx, t); err == nil {
				tags = append(tags, id)
			}
		}
	}
	for _, a := range n.Actors {
		if a.Name = strings.TrimSpace(a.Name); a.Name != "" {
			if id, _, err := s.store.EnsurePerformer(ctx, a.Name); err == nil {
				perfs = append(perfs, id)
			}
		}
	}
	_ = s.store.AddSceneRelations(ctx, sc.ID, perfs, tags)
}

// recheckShapes works the VR shapes out again from names and frame sizes
// (when the rules got better); pictures made for a wrong shape are made again.
func (s *Server) recheckShapes(ctx context.Context) {
	rows, err := s.store.ShapesToCheck(ctx)
	if err != nil {
		return
	}
	changed := 0
	for _, r := range rows {
		sh := media.DetectShape(r.Path, r.Width, r.Height, "", "")
		proj, fov := sh.Projection, sh.FOV
		if proj == "flat" {
			proj, fov = "", 0
		}
		if proj == r.Projection && (proj == "" || (sh.Stereo == r.Stereo && fov == r.FOV)) {
			continue
		}
		_ = s.store.SetVR(ctx, r.ID, proj, sh.Stereo, fov)
		if r.CoverSource == "frame" {
			_ = s.store.SetSceneCover(ctx, r.ID, "", "")
			s.removeFile(r.CoverPath)
		}
		_ = s.store.ResetGenerated(ctx, []int64{r.ID})
		changed++
	}
	if changed > 0 {
		s.log.Info("VR shapes worked out again", "changed", changed)
		s.wakeWork()
	}
}

// ---------- background work ----------

// worker probes new files, makes their pictures and previews and
// fingerprints them, one scene at a time.
func (s *Server) worker(ctx context.Context) {
	for {
		did := s.workOnce(ctx)
		if did {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.workWake:
		case <-time.After(5 * time.Minute):
		}
	}
}

func (s *Server) busyIDs() []int64 {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	ids := make([]int64, 0, len(s.working))
	for id := range s.working {
		ids = append(ids, id)
	}
	return ids
}

func (s *Server) setWorking(id int64, what string) {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if what == "" {
		delete(s.working, id)
	} else {
		s.working[id] = what
	}
}

func (s *Server) workOnce(ctx context.Context) bool {
	if ok, _, _ := s.tools.Ready(); !ok {
		return false
	}
	s.workMu.Lock()
	items, err := s.store.NextWork(ctx, 1, s.busyIDsLocked())
	if err != nil || len(items) == 0 {
		s.workMu.Unlock()
		return false
	}
	it := items[0]
	s.working[it.ID] = "reading"
	s.workMu.Unlock()
	defer s.setWorking(it.ID, "")
	s.processScene(ctx, it)
	return ctx.Err() == nil
}

func (s *Server) busyIDsLocked() []int64 {
	ids := make([]int64, 0, len(s.working))
	for id := range s.working {
		ids = append(ids, id)
	}
	return ids
}

// shapeOf is how a scene's frame is laid out.
func shapeOf(sc *store.Scene) media.Shape {
	return media.Shape{Projection: sc.Projection, Stereo: sc.Stereo, FOV: sc.FOV}
}

func (s *Server) processScene(ctx context.Context, it store.WorkItem) {
	sc, err := s.store.GetScene(ctx, 0, it.ID)
	if err != nil {
		return
	}
	full := s.scenePath(sc)
	if it.Probe {
		s.setWorking(sc.ID, "reading")
		p, err := s.tools.ProbeFile(ctx, full)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			_ = s.store.SetProbe(ctx, sc.ID, store.ProbeResult{})
			_ = s.store.SetGenerated(ctx, sc.ID, "failed", err.Error(), false, false)
			_ = s.store.SetPHash(ctx, sc.ID, "-")
			return
		}
		_ = s.store.SetProbe(ctx, sc.ID, store.ProbeResult{Duration: p.Duration, Width: p.Width, Height: p.Height, FPS: p.FPS,
			VideoCodec: p.VideoCodec, AudioCodec: p.AudioCodec, Container: p.Container, Bitrate: p.Bitrate})
		if !sc.VRSet {
			sh := media.DetectShape(sc.Path, p.Width, p.Height, p.Spherical, p.Stereo3D)
			fov := sh.FOV
			proj := sh.Projection
			if proj == "flat" {
				proj, fov = "", 0
			}
			_ = s.store.SetVR(ctx, sc.ID, proj, sh.Stereo, fov)
		}
		// read every new file first (seconds each): lengths, quality and VR
		// shapes show up at once; the slow pictures follow in the next passes
		if left, _, _ := s.store.WorkLeft(ctx); left > 0 {
			return
		}
		if sc, err = s.store.GetScene(ctx, 0, it.ID); err != nil {
			return
		}
	}
	if sc.GenState == "" {
		s.generate(ctx, sc)
		if ctx.Err() != nil {
			return
		}
		sc, _ = s.store.GetScene(ctx, 0, it.ID)
	}
	if sc != nil && sc.PHash == "" && sc.Duration > 0 {
		s.setWorking(sc.ID, "fingerprint")
		h, err := s.tools.PHash(ctx, full, sc.Duration)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.log.Debug("phash failed", "scene", sc.ID, "err", err)
			h = "-" // do not try again on every pass
		}
		_ = s.store.SetPHash(ctx, sc.ID, h)
		if err == nil {
			s.autoIdentify(ctx, sc.ID)
		}
	}
}

// generate makes a scene's cover (when it has none), scrubber sheet and preview.
func (s *Server) generate(ctx context.Context, sc *store.Scene) {
	full := s.scenePath(sc)
	sh := shapeOf(sc)
	var errs []string
	if sc.CoverPath == "" {
		s.setWorking(sc.ID, "cover")
		at := sc.Duration * 0.2
		if b, err := s.tools.Screenshot(ctx, full, at, sh, 1280, 720); err == nil {
			_ = s.setSceneCover(ctx, sc, b, "frame")
		} else if ctx.Err() == nil {
			errs = append(errs, "cover: "+err.Error())
		}
	}
	sprite := false
	if sc.Duration > 0 {
		s.setWorking(sc.ID, "scrubber")
		w, h := 160, 90
		if b, info, err := s.tools.Sprite(ctx, full, sc.Duration, sh, 81, 9, w, h); err == nil {
			if err := writeFileAtomic(s.genPath("sprites", sc.ID, ".jpg"), b); err == nil {
				j, _ := json.Marshal(info)
				_ = writeFileAtomic(s.genPath("sprites", sc.ID, ".json"), j)
				sprite = true
			}
		} else if ctx.Err() == nil {
			errs = append(errs, "scrubber: "+err.Error())
		}
	}
	preview := false
	if s.cfg.Previews && sc.Duration > 0 && ctx.Err() == nil {
		s.setWorking(sc.ID, "preview")
		dst := s.genPath("previews", sc.ID, ".mp4")
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := s.tools.Preview(ctx, full, dst, sc.Duration, sh, 12, 0.75, 640, 360); err == nil {
			preview = true
		} else if ctx.Err() == nil {
			errs = append(errs, "preview: "+err.Error())
		}
	}
	if ctx.Err() != nil {
		return
	}
	state := "done"
	if !sprite && !preview && len(errs) > 0 {
		state = "failed"
	}
	_ = s.store.SetGenerated(ctx, sc.ID, state, strings.Join(errs, "; "), preview, sprite)
	_ = s.store.CacheDelete(ctx, fmt.Sprintf("markerclip:%d:", sc.ID))
}

func writeFileAtomic(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// setSceneCover stores a picture as the scene's cover (JPEG, at most 1600 px).
func (s *Server) setSceneCover(ctx context.Context, sc *store.Scene, b []byte, source string) error {
	rel, err := s.savePicture("covers", b, 1600)
	if err != nil {
		return err
	}
	old := sc.CoverPath
	if err := s.store.SetSceneCover(ctx, sc.ID, rel, source); err != nil {
		s.removeFile(rel)
		return err
	}
	sc.CoverPath, sc.CoverSource = rel, source
	if old != "" && old != rel {
		s.removeFile(old)
	}
	return nil
}

// savePicture stores a picture as JPEG (scaled down to maxSide) and returns
// its FILES_DIR relative path.
func (s *Server) savePicture(sub string, b []byte, maxSide int) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("not a picture: %w", err)
	}
	img = fit(img, maxSide)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flatten(img), &jpeg.Options{Quality: 86}); err != nil {
		return "", err
	}
	return s.saveFile(sub, ".jpg", buf.Bytes())
}

// ---------- trash ----------

// relatedFiles are a video's file and the files next to it with its name
// (cover, info, nfo, subtitles).
func relatedFiles(video string) []string {
	out := []string{video}
	base := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	entries, err := os.ReadDir(filepath.Dir(video))
	if err != nil {
		return out
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && n != filepath.Base(video) && strings.HasPrefix(n, base+".") && !isVideo(n) {
			out = append(out, filepath.Join(filepath.Dir(video), n))
		}
	}
	return out
}

// trashScene moves a scene's files into the trash folder; it stays
// restorable for TRASH_DAYS.
func (s *Server) trashScene(ctx context.Context, sc *store.Scene) error {
	stamp := fmt.Sprintf("%s_%d", time.Now().Format("2006-01-02_150405"), sc.ID)
	dst := filepath.Join(s.cfg.LibraryDir, ".trash", stamp)
	full := s.scenePath(sc)
	for _, f := range relatedFiles(full) {
		rel, _ := filepath.Rel(s.cfg.LibraryDir, f)
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.Rename(f, target); err != nil {
			if f == full {
				return fmt.Errorf("the file could not be moved to the trash: %w", err)
			}
		}
	}
	// an emptied release folder goes too
	if dir := filepath.Dir(full); dir != s.cfg.LibraryDir {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	return s.store.TrashScene(ctx, sc.ID, filepath.ToSlash(filepath.Join(".trash", stamp)))
}

// restoreScene puts a trashed scene's files back.
func (s *Server) restoreScene(ctx context.Context, sc *store.Scene) error {
	src := filepath.Join(s.cfg.LibraryDir, filepath.FromSlash(sc.TrashPath))
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(s.cfg.LibraryDir, rel)
		if fileExists(target) {
			return fmt.Errorf("%s is in the way", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.Rename(p, target)
	})
	if err != nil {
		return err
	}
	_ = os.RemoveAll(src)
	return s.store.RestoreScene(ctx, sc.ID)
}

// purgeScene deletes a trashed scene for good.
func (s *Server) purgeScene(ctx context.Context, sc *store.Scene) error {
	if sc.TrashPath != "" {
		_ = os.RemoveAll(filepath.Join(s.cfg.LibraryDir, filepath.FromSlash(sc.TrashPath)))
	}
	s.dropGenerated(sc.ID)
	s.removeFile(sc.CoverPath)
	return s.store.DeleteScene(ctx, sc.ID)
}

func (s *Server) dropGenerated(id int64) {
	for _, p := range []string{s.genPath("sprites", id, ".jpg"), s.genPath("sprites", id, ".json"), s.genPath("previews", id, ".mp4")} {
		_ = os.Remove(p)
	}
	matches, _ := filepath.Glob(filepath.Join(s.cfg.GeneratedDir, "markers", fmt.Sprintf("%d-*", id)))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

func (s *Server) emptyTrash(ctx context.Context) {
	if s.cfg.TrashDays <= 0 {
		return
	}
	cut := time.Now().AddDate(0, 0, -s.cfg.TrashDays).UTC().Format(time.RFC3339Nano)
	list, err := s.store.TrashedBefore(ctx, cut)
	if err != nil {
		return
	}
	for i := range list {
		if err := s.purgeScene(ctx, &list[i]); err == nil {
			s.log.Info("trash: deleted for good", "scene", list[i].ID)
		}
	}
}

// dropStaleTemp removes download pieces nobody is working on any more.
func (s *Server) dropStaleTemp() {
	dir := filepath.Join(s.cfg.LibraryDir, ".downloads")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	s.dlMu.Lock()
	defer s.dlMu.Unlock()
	for _, e := range entries {
		if s.dlRunning[e.Name()] != nil {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > 48*time.Hour {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}
