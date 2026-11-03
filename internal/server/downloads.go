package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/ytdlp"
)

// Downloads from a link, like MeTube: the person pastes a video page (or a
// playlist or channel), yt-dlp fetches it into the library with its picture
// and info file, and the scene appears with title, date, description,
// studio and tags filled in. Channels can be followed: new videos are
// fetched as they come.

func (s *Server) wakeDownloads() {
	select {
	case s.dlWake <- struct{}{}:
	default:
	}
}

// publicURL refuses local-network addresses for people who are not admins.
func (s *Server) publicURL(ctx context.Context, u *store.User, raw string) error {
	pu, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return errors.New("paste a link that starts with http:// or https://")
	}
	if u != nil && u.Role == "admin" {
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, pu.Hostname())
	if err != nil {
		return fmt.Errorf("the site's name does not resolve: %s", pu.Hostname())
	}
	for _, ip := range ips {
		if ip.IP.IsLoopback() || ip.IP.IsPrivate() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || isCGNAT(ip.IP) {
			return errors.New("addresses on the local network can only be fetched by an admin")
		}
	}
	return nil
}

func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64
}

// cleanFolder checks a library sub-folder name.
func cleanFolder(f string) (string, error) {
	f = strings.Trim(strings.TrimSpace(strings.ReplaceAll(f, "\\", "/")), "/")
	if f == "" {
		return "", nil
	}
	for _, part := range strings.Split(f, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return "", errors.New("a folder name cannot start with a dot")
		}
	}
	return f, nil
}

func (s *Server) cookiesFile(uid int64) string {
	return filepath.Join(s.cfg.DataDir, "cookies", fmt.Sprintf("%d.txt", uid))
}

// downloader starts queued downloads, at most MAX_DOWNLOADS at a time.
func (s *Server) downloader(ctx context.Context) {
	s.store.RequeueInterrupted(ctx)
	for {
		ids, _ := s.store.QueuedDownloads(ctx)
		for _, id := range ids {
			s.dlMu.Lock()
			if len(s.dlRunning) >= s.cfg.MaxDownloads || s.dlRunning[id] != nil {
				s.dlMu.Unlock()
				continue
			}
			dctx, cancel := context.WithCancel(ctx)
			s.dlRunning[id] = cancel
			s.dlMu.Unlock()
			go func(id string) {
				defer func() {
					s.dlMu.Lock()
					delete(s.dlRunning, id)
					s.dlMu.Unlock()
					s.wakeDownloads()
				}()
				s.runDownload(dctx, id)
			}(id)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.dlWake:
		case <-time.After(30 * time.Second):
		}
	}
}

func (s *Server) failDownload(ctx context.Context, d *store.Download, err error) {
	msg := err.Error()
	_ = s.store.UpdateDownload(context.WithoutCancel(ctx), d.ID, store.DownloadPatch{Status: strp("failed"), Error: &msg, Speed: strp(""), ETA: strp("")})
	if d.Kind == "video" {
		s.notifyDownload(context.WithoutCancel(ctx), d, false, msg)
	}
}

// runDownload reads a link and fetches it (a playlist becomes one row per video).
func (s *Server) runDownload(ctx context.Context, id string) {
	d, err := s.store.GetDownload(ctx, 0, id)
	if err != nil || d.Status != "queued" {
		return
	}
	opts, _ := s.store.DownloadOptionsOf(ctx, id)
	st, _ := s.store.GetSettings(ctx, d.UserID)
	_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{Status: strp("downloading"), Error: strp(""), Percent: f64p(0)})
	yo := ytdlp.Options{Password: opts.Password, Limit: opts.ItemsLimit}
	if fileExists(s.cookiesFile(d.UserID)) {
		yo.Cookies = s.cookiesFile(d.UserID)
	}
	if d.Kind != "video" {
		info, err := s.yt.Inspect(ctx, d.URL, yo)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.failDownload(ctx, d, err)
			return
		}
		if info.IsList() {
			n := len(info.Entries)
			switch {
			case opts.Single:
				s.failDownload(ctx, d, fmt.Errorf("this page is not one video — it shows %d short clips (usually free previews). The full scene needs your account on that site: add its cookies.txt under Settings → Downloads and try again", n))
				return
			case info.Generic() && !opts.AllowList:
				// not a playlist the site has: ask before fetching every clip on the page
				msg := fmt.Sprintf("this page is not a playlist but has %d videos on it (often short previews)", n)
				_ = s.store.UpdateDownload(context.WithoutCancel(ctx), d.ID, store.DownloadPatch{Status: strp("failed"), Error: &msg, Ask: strp("list"), Found: &n,
					Title: strp(firstNonEmpty(info.Title, d.Title)), Speed: strp(""), ETA: strp("")})
				return
			}
			s.expandPlaylist(ctx, d, info, opts)
			return
		}
		d.Kind = "video"
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE downloads SET kind = 'video' WHERE id = ?`, id)
		p := store.DownloadPatch{}
		if info.Title != "" && d.Title == "" {
			p.Title = &info.Title
		}
		if info.Thumbnail != "" {
			p.Thumbnail = &info.Thumbnail
		}
		_ = s.store.UpdateDownload(ctx, id, p)
	}
	folder := opts.Folder
	if folder == "" {
		folder = st.DownloadFolder
	}
	folder, err = cleanFolder(folder)
	if err != nil {
		s.failDownload(ctx, d, err)
		return
	}
	own := st.OwnFolder
	if opts.OwnFolder != nil {
		own = *opts.OwnFolder
	}
	name := "%(title).150B [%(id)s]"
	if d.ParentID != "" {
		// videos of one list can share a title and id (clips found on a page): keep each one
		name += " " + d.ID[:6]
	}
	if p := strings.TrimSpace(opts.NamePrefix); p != "" {
		name = strings.ReplaceAll(p, "%", "%%") + " " + name
	}
	tmpl := name + ".%(ext)s"
	if own {
		tmpl = name + "/" + tmpl
	}
	quality := firstNonEmpty(opts.Quality, st.DownloadQuality, "best")
	format := firstNonEmpty(opts.Format, st.DownloadFormat, "auto")
	dir := filepath.Join(s.cfg.LibraryDir, filepath.FromSlash(folder))
	temp := filepath.Join(s.cfg.LibraryDir, ".downloads", id)
	if free := diskFree(s.cfg.LibraryDir); free >= 0 && free < 2<<30 {
		s.failDownload(ctx, d, fmt.Errorf("less than 2 GB free in the library (%s)", humanBytes(free)))
		return
	}
	var mu sync.Mutex
	last := time.Time{}
	file, err := s.yt.Download(ctx, ytdlp.Job{URL: d.URL, Dir: dir, TempDir: temp, Template: tmpl, Quality: quality, Format: format,
		ClipStart: opts.ClipStart, ClipEnd: opts.ClipEnd, Options: yo},
		func(m ytdlp.Meta) {
			p := store.DownloadPatch{}
			if m.Title != "" {
				p.Title = &m.Title
			}
			if m.Thumbnail != "" {
				p.Thumbnail = &m.Thumbnail
			}
			_ = s.store.UpdateDownload(ctx, id, p)
		},
		func(pr ytdlp.Progress) {
			mu.Lock()
			defer mu.Unlock()
			if time.Since(last) < time.Second && pr.Status != "finished" {
				return
			}
			last = time.Now()
			total := pr.Total
			if total == 0 {
				total = int64(pr.Estimate)
			}
			pct := 0.0
			if total > 0 {
				pct = float64(pr.Downloaded) / float64(total) * 100
			}
			speed, eta := "", ""
			if pr.Speed > 0 {
				speed = humanBytes(int64(pr.Speed)) + "/s"
			}
			if pr.ETA > 0 {
				eta = (time.Duration(pr.ETA) * time.Second).String()
			}
			_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{Percent: &pct, Speed: &speed, ETA: &eta, SizeBytes: &total})
		})
	_ = os.RemoveAll(temp)
	if ctx.Err() != nil {
		return // canceled or shutting down (a restart puts it back in the queue)
	}
	if err != nil {
		s.failDownload(ctx, d, err)
		return
	}
	s.finishDownload(ctx, d, file, opts)
}

func f64p(v float64) *float64 { return &v }

func clockText(sec float64) string {
	t := int(sec)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// finishDownload files the downloaded video as a scene.
func (s *Server) finishDownload(ctx context.Context, d *store.Download, file string, opts store.DownloadOptions) {
	rel, err := filepath.Rel(s.cfg.LibraryDir, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		s.failDownload(ctx, d, fmt.Errorf("yt-dlp put the file outside the library: %s", file))
		return
	}
	rel = filepath.ToSlash(rel)
	// a scene of known length that came much shorter is only its preview
	if opts.ExpectSeconds > 60 {
		if p, err := s.tools.ProbeFile(ctx, file); err == nil && p.Duration > 0 && p.Duration < float64(opts.ExpectSeconds)*0.5 && p.Duration < float64(opts.ExpectSeconds)-120 {
			for _, f := range relatedFiles(file) {
				_ = os.Remove(f)
			}
			if dir := filepath.Dir(file); dir != s.cfg.LibraryDir {
				_ = os.Remove(dir) // only when empty
			}
			s.failDownload(ctx, d, fmt.Errorf("only a short preview came (%s of %s) — the full scene needs your account on that site: add its cookies.txt under Settings → Downloads",
				clockText(p.Duration), clockText(float64(opts.ExpectSeconds))))
			return
		}
	}
	var size int64
	if fi, err := os.Stat(file); err == nil {
		size = fi.Size()
	}
	_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("processing"), FilePath: &rel, Percent: f64p(100), SizeBytes: &size, Speed: strp(""), ETA: strp("")})
	if err := s.scanAll(ctx, map[string]int64{rel: d.UserID}); err != nil {
		s.failDownload(ctx, d, fmt.Errorf("the video is in the library but could not be read: %w", err))
		return
	}
	sc, err := s.store.SceneByPath(ctx, rel)
	if err != nil {
		s.failDownload(ctx, d, errors.New("the video is in the library but was not picked up (is it a video file?)"))
		return
	}
	// the choices made when pasting the link
	if len(opts.Performers) > 0 || len(opts.Tags) > 0 {
		_ = s.store.AddSceneRelations(ctx, sc.ID, opts.Performers, opts.Tags)
	}
	p := store.ScenePatch{SourceURL: strp(d.URL)}
	if opts.StudioID > 0 {
		sid := &opts.StudioID
		p.StudioID = &sid
	}
	_ = s.store.UpdateScene(ctx, sc.ID, p)
	_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("done"), SceneID: &sc.ID, Error: strp("")})
	s.notifyDownload(ctx, d, true, "")
	s.publish(ctx, d.UserID, "sphere.download_finished", fmt.Sprintf("scene:%d", sc.ID), map[string]any{"scene_id": sc.ID, "size_bytes": size})
	s.wakeWork()
}

func (s *Server) notifyDownload(ctx context.Context, d *store.Download, ok bool, errMsg string) {
	st, err := s.store.GetSettings(ctx, d.UserID)
	if err != nil || st.NotifyURL == "" || !st.NotifyDownloads {
		return
	}
	title := "Download finished"
	body := "A new video is in your library."
	prio := "info"
	if !ok {
		title, body, prio = "Download failed", "A download did not work: "+errMsg, "warning"
	}
	if !st.NotifyQuiet && d.Title != "" {
		body = d.Title
		if !ok {
			body = d.Title + " — " + errMsg
		}
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	link := ""
	if s.cfg.BaseURL != "" {
		link = s.cfg.BaseURL + "/downloads"
	}
	if err := n.Send(ctx, notify.Message{Title: title, Body: body, Priority: prio, Tags: []string{"movie_camera"}, Link: link}); err != nil {
		s.log.Debug("notify failed", "err", err)
	}
}

// expandPlaylist turns a playlist/channel row into one queued row per video.
func (s *Server) expandPlaylist(ctx context.Context, d *store.Download, info *ytdlp.Info, opts store.DownloadOptions) {
	added := 0
	for i, e := range info.Entries {
		if opts.ItemsLimit > 0 && i >= opts.ItemsLimit {
			break
		}
		u := e.URL
		if !strings.HasPrefix(u, "http") {
			continue
		}
		if s.store.DownloadedURL(ctx, d.UserID, u) {
			continue
		}
		child := &store.Download{ID: store.RandomToken(16), UserID: d.UserID, ParentID: d.ID, SubscriptionID: d.SubscriptionID, URL: u, Kind: "video",
			Title: e.Title, Options: opts}
		child.Options.ItemsLimit = 0
		if err := s.store.CreateDownload(ctx, child); err == nil {
			added++
		}
	}
	title := firstNonEmpty(info.Title, info.Uploader, info.Channel, d.URL)
	msg := ""
	if added == 0 {
		msg = "nothing new in this list"
	}
	_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("done"), Title: &title, Error: &msg, Percent: f64p(100)})
	_, _ = s.store.DB.ExecContext(ctx, `UPDATE downloads SET kind = 'playlist' WHERE id = ?`, d.ID)
	s.wakeDownloads()
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// updateYTDLP keeps our own yt-dlp at most a day old.
func (s *Server) updateYTDLP(ctx context.Context) {
	if !s.cfg.YTDLPUpdate || s.cfg.YTDLP != "" {
		return
	}
	key := "ytdlp:updated"
	if _, at, ok := s.store.CacheGet(ctx, key); ok {
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 23*time.Hour {
			return
		}
	}
	v, err := s.yt.Update(ctx)
	if err != nil {
		s.log.Warn("yt-dlp update failed", "err", err)
		return
	}
	_ = s.store.CachePut(ctx, key, v)
	s.log.Info("yt-dlp is up to date", "version", v)
}

// ---------- subscriptions ----------

// subscriptions checks followed channels and playlists for new videos.
func (s *Server) subscriptions(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Minute):
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		subs, _ := s.store.ListSubscriptions(ctx, 0)
		for i := range subs {
			x := &subs[i]
			if !x.Enabled {
				continue
			}
			if last, err := time.Parse(time.RFC3339Nano, x.LastCheckedAt); err == nil && time.Since(last) < time.Duration(max(x.CheckMinutes, 15))*time.Minute {
				continue
			}
			s.checkSubscription(ctx, x)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// checkSubscription queues the videos of a channel not seen before. The
// first check only remembers what is there (or fetches the newest N when an
// items limit is set).
func (s *Server) checkSubscription(ctx context.Context, x *store.Subscription) int {
	yo := ytdlp.Options{Password: x.Options.Password, Limit: 50}
	if fileExists(s.cookiesFile(x.UserID)) {
		yo.Cookies = s.cookiesFile(x.UserID)
	}
	info, err := s.yt.Inspect(ctx, x.URL, yo)
	if err != nil {
		_ = s.store.SubscriptionChecked(ctx, x.ID, "", err.Error(), 0)
		return 0
	}
	seen, _ := s.store.SeenVideos(ctx, x.ID)
	first := x.LastCheckedAt == ""
	var filter *regexp.Regexp
	if x.TitleFilter != "" {
		filter, _ = regexp.Compile("(?i)" + x.TitleFilter)
	}
	var mark []string
	queued := 0
	for i, e := range info.Entries {
		key := firstNonEmpty(e.ID, e.URL)
		if seen[key] {
			continue
		}
		mark = append(mark, key)
		if first && (x.Options.ItemsLimit == 0 || i >= x.Options.ItemsLimit) {
			continue
		}
		if filter != nil && !filter.MatchString(e.Title) {
			continue
		}
		if !strings.HasPrefix(e.URL, "http") || s.store.DownloadedURL(ctx, x.UserID, e.URL) {
			continue
		}
		d := &store.Download{ID: store.RandomToken(16), UserID: x.UserID, SubscriptionID: &x.ID, URL: e.URL, Kind: "video", Title: e.Title, Options: x.Options}
		d.Options.ItemsLimit = 0
		if err := s.store.CreateDownload(ctx, d); err == nil {
			queued++
		}
	}
	_ = s.store.MarkSeen(ctx, x.ID, mark)
	_ = s.store.SubscriptionChecked(ctx, x.ID, firstNonEmpty(info.Title, info.Uploader, info.Channel), "", queued)
	if queued > 0 {
		s.wakeDownloads()
	}
	return queued
}

// ---------- API ----------

func (s *Server) canDownload(w http.ResponseWriter, u *store.User) bool {
	if u.Role == "admin" || s.cfg.DownloadsOpen {
		return true
	}
	writeErr(w, http.StatusForbidden, "downloads are for admins on this box")
	return false
}

func (s *Server) handleCreateDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	if !s.canDownload(w, u) {
		return
	}
	var in struct {
		URL     string                `json:"url"`
		URLs    []string              `json:"urls"` // several at once (one per line)
		Options store.DownloadOptions `json:"options"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	links := in.URLs
	if in.URL != "" {
		links = append(links, strings.Fields(in.URL)...)
	}
	if len(links) == 0 {
		writeErr(w, http.StatusBadRequest, "paste a link first")
		return
	}
	if len(links) > 200 {
		writeErr(w, http.StatusBadRequest, "at most 200 links at once")
		return
	}
	if _, err := cleanFolder(in.Options.Folder); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	switch in.Options.Quality {
	case "", "best", "2160", "1440", "1080", "720", "480":
	default:
		writeErr(w, http.StatusBadRequest, "unknown quality")
		return
	}
	switch in.Options.Format {
	case "", "auto", "mp4", "mkv", "webm":
	default:
		writeErr(w, http.StatusBadRequest, "unknown format")
		return
	}
	reTime := regexp.MustCompile(`^(\d+:)?(\d+:)?\d+(\.\d+)?$`)
	if (in.Options.ClipStart != "" && !reTime.MatchString(in.Options.ClipStart)) || (in.Options.ClipEnd != "" && !reTime.MatchString(in.Options.ClipEnd)) {
		writeErr(w, http.StatusBadRequest, "clip times look like 2:26 or 1:02:03")
		return
	}
	status := "queued"
	if in.Options.AutoStart != nil && !*in.Options.AutoStart {
		status = "paused"
	}
	var out []store.Download
	for _, link := range links {
		link = strings.TrimSpace(link)
		if err := s.publicURL(ctx, u, link); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		d := &store.Download{ID: store.RandomToken(16), UserID: u.ID, URL: link, Kind: "auto", Options: in.Options, Status: status}
		if err := s.store.CreateDownload(ctx, d); err != nil {
			s.fail(w, err, "download")
			return
		}
		got, _ := s.store.GetDownload(ctx, u.ID, d.ID)
		out = append(out, *got)
	}
	s.wakeDownloads()
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListDownloads(r.Context(), userFrom(r.Context()).ID, 500)
	if err != nil {
		s.fail(w, err, "downloads")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUpdateDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	d, err := s.store.GetDownload(ctx, u.ID, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err, "download")
		return
	}
	var in struct {
		Action string `json:"action"` // retry | pause | start
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Action {
	case "retry":
		if d.Status == "failed" || d.Status == "canceled" {
			_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("queued"), Error: strp(""), Ask: strp(""), Percent: f64p(0)})
		}
	case "all": // every video on the page after all
		if d.Ask == "list" {
			o, _ := s.store.DownloadOptionsOf(ctx, d.ID)
			o.AllowList, o.Single = true, false
			_ = s.store.SetDownloadOptions(ctx, d.ID, o)
			_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("queued"), Error: strp(""), Ask: strp(""), Percent: f64p(0)})
		}
	case "start":
		if d.Status == "paused" {
			_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("queued")})
		}
	case "pause":
		if d.Status == "queued" {
			_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("paused")})
		}
	default:
		writeErr(w, http.StatusBadRequest, "unknown action")
		return
	}
	s.wakeDownloads()
	d, _ = s.store.GetDownload(ctx, u.ID, d.ID)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	d, err := s.store.GetDownload(ctx, u.ID, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err, "download")
		return
	}
	s.dlMu.Lock()
	if cancel := s.dlRunning[d.ID]; cancel != nil {
		cancel()
	}
	s.dlMu.Unlock()
	_ = os.RemoveAll(filepath.Join(s.cfg.LibraryDir, ".downloads", d.ID))
	_ = s.store.DeleteDownload(ctx, u.ID, d.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClearDownloads(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Failed bool `json:"failed"`
	}
	_ = decode(r, &in)
	n, err := s.store.ClearDownloads(r.Context(), userFrom(r.Context()).ID, in.Failed)
	if err != nil {
		s.fail(w, err, "clear")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"removed": n})
}

func (s *Server) handleYTDLPStatus(w http.ResponseWriter, r *http.Request) {
	st := s.yt.Status(r.Context())
	u := userFrom(r.Context())
	if u.Role != "admin" {
		st.Path = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"ytdlp": st, "cookies": fileExists(s.cookiesFile(u.ID)), "members": s.cfg.DownloadsOpen,
		"max": s.cfg.MaxDownloads, "auto_update": s.cfg.YTDLPUpdate})
}

func (s *Server) handleYTDLPUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	v, err := s.yt.Update(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	_ = s.store.CachePut(r.Context(), "ytdlp:updated", v)
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}

// handleCookies stores (PUT, a Netscape cookies.txt) or removes the
// person's own cookies for sites that need a sign-in.
func (s *Server) handleCookies(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	p := s.cookiesFile(u.ID)
	if r.Method == http.MethodDelete {
		_ = os.Remove(p)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the file is too big")
		return
	}
	txt := string(b)
	if !strings.Contains(txt, "\t") || !(strings.Contains(txt, "# Netscape HTTP Cookie File") || strings.Contains(txt, "# HTTP Cookie File") || strings.Count(txt, "\t") >= 6) {
		writeErr(w, http.StatusBadRequest, "this does not look like a cookies.txt (Netscape format) — export it with a browser add-on")
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		s.fail(w, err, "cookies")
		return
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		s.fail(w, err, "cookies")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSubscriptions(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err, "subscriptions")
		return
	}
	for i := range list {
		list[i].Options.Password = ""
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	if !s.canDownload(w, u) {
		return
	}
	var in store.Subscription
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	x := &store.Subscription{UserID: u.ID, Enabled: true, CheckMinutes: 60}
	if id := idParam(r, "id"); id > 0 {
		if x, err = s.store.GetSubscription(ctx, u.ID, id); err != nil {
			s.fail(w, err, "subscription")
			return
		}
		x.UserID = u.ID
	} else {
		x.URL = strings.TrimSpace(in.URL)
		if err := s.publicURL(ctx, u, x.URL); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if keys["name"] {
		x.Name = oneLine(in.Name, 120)
	}
	if keys["options"] {
		pw := x.Options.Password
		x.Options = in.Options
		if x.Options.Password == "" {
			x.Options.Password = pw
		}
		if _, err := cleanFolder(x.Options.Folder); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if keys["check_minutes"] {
		x.CheckMinutes = min(max(in.CheckMinutes, 15), 7*24*60)
	}
	if keys["title_filter"] {
		if _, err := regexp.Compile(in.TitleFilter); err != nil {
			writeErr(w, http.StatusBadRequest, "the title filter is not a valid regular expression")
			return
		}
		x.TitleFilter = in.TitleFilter
	}
	if keys["enabled"] {
		x.Enabled = in.Enabled
	}
	if err := s.store.SaveSubscription(ctx, x); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "you already follow this link")
			return
		}
		s.fail(w, err, "subscription")
		return
	}
	if idParam(r, "id") == 0 {
		go s.checkSubscription(context.WithoutCancel(ctx), x)
	}
	x.Options.Password = ""
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSubscription(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "subscription")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, err := s.store.GetSubscription(ctx, userFrom(ctx).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "subscription")
		return
	}
	n := s.checkSubscription(ctx, x)
	writeJSON(w, http.StatusOK, map[string]int{"queued": n})
}
