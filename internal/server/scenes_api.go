package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/media"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// ---------- how a scene is shown ----------

var reRelease = regexp.MustCompile(`(?i)[ ._-]+(xxx|(480|540|576|720|1080|1440|1920|2160|2700|2880|3072|3840|4096|5760|7680)p|4k|5k|6k|8k|hevc|x26[45]|h26[45]|web-?dl|mp4|wrb|ktr|gush|gush|vr180|vr360|oculus|quest[ ._]?\d?|lr|sbs|tb|3dh|180|360)\b.*$`)
var reBrackets = regexp.MustCompile(`\[[^\]]*\]`)

// niceName is a scene's file name made readable ("BaDoinkVR.Lola.Myluv.On.Her.Knees…4K..mp4" → "BaDoinkVR Lola Myluv On Her Knees Begging").
func niceName(p string) string {
	base := path.Base(p)
	base = strings.TrimSuffix(base, path.Ext(base))
	base = reBrackets.ReplaceAllString(base, " ")
	if !strings.Contains(base, " ") {
		base = strings.NewReplacer(".", " ", "_", " ").Replace(base)
	}
	if cut := reRelease.ReplaceAllString(base, ""); len(strings.TrimSpace(cut)) >= 6 {
		base = cut
	}
	base = strings.Join(strings.Fields(strings.Trim(base, " .,-_")), " ")
	if base == "" {
		return path.Base(p)
	}
	return base
}

func coverVersion(rel string) string {
	h := sha1.Sum([]byte(rel))
	return hex.EncodeToString(h[:4])
}

// decorate fills the URLs and names the UI uses.
func (s *Server) decorate(sc *store.Scene) {
	sc.Name = sc.Title
	if sc.Name == "" {
		sc.Name = niceName(sc.Path)
	}
	base := fmt.Sprintf("/api/v1/scenes/%d", sc.ID)
	if sc.CoverPath != "" {
		sc.Cover = base + "/cover?v=" + coverVersion(sc.CoverPath)
	}
	if sc.HasPreview {
		sc.Preview = fmt.Sprintf("%s/preview.mp4?v=%d", base, sc.GenVersion)
	}
	if sc.HasSprite {
		sc.Sprite = fmt.Sprintf("%s/sprite.jpg?v=%d", base, sc.GenVersion)
	}
	sh := shapeOf(sc)
	sc.IsVR = sh.IsVR()
	if sc.IsVR {
		sc.VRLabel = vrLabel(sh)
	}
	if sc.Studio != nil && sc.Studio.Image != "" {
		sc.Studio.Image = fmt.Sprintf("/api/v1/studios/%d/image?v=%s", sc.Studio.ID, coverVersion(sc.Studio.Image))
	}
	for i := range sc.Performers {
		if sc.Performers[i].Image != "" {
			sc.Performers[i].Image = fmt.Sprintf("/api/v1/performers/%d/image?v=%s", sc.Performers[i].ID, coverVersion(sc.Performers[i].Image))
		}
	}
	for i := range sc.Tags {
		sc.Tags[i].Image = ""
	}
	for i := range sc.Groups {
		if sc.Groups[i].Image != "" {
			sc.Groups[i].Image = fmt.Sprintf("/api/v1/groups/%d/front?v=%s", sc.Groups[i].ID, coverVersion(sc.Groups[i].Image))
		}
	}
}

func vrLabel(sh media.Shape) string {
	var p string
	switch sh.Projection {
	case "equirect180":
		p = "VR 180°"
	case "equirect360":
		p = "VR 360°"
	case "fisheye":
		p = fmt.Sprintf("VR fisheye %d°", max(sh.FOV, 180))
	}
	switch sh.Stereo {
	case "sbs":
		p += " · side by side"
	case "tb":
		p += " · top/bottom"
	default:
		p += " · mono"
	}
	return p
}

func (s *Server) decorateAll(list []store.Scene) {
	for i := range list {
		s.decorate(&list[i])
	}
}

// ---------- the list ----------

func idsParam(v string) []int64 {
	var out []int64
	for _, p := range strings.Split(v, ",") {
		if n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func (s *Server) sceneFilter(r *http.Request) store.SceneFilter {
	q := r.URL.Query()
	f := store.SceneFilter{UserID: userFrom(r.Context()).ID, Query: q.Get("q"),
		Performers: idsParam(q.Get("performers")), Tags: idsParam(q.Get("tags")), TagsAll: queryBool(r, "tags_all"),
		Studios: idsParam(q.Get("studios")), Groups: idsParam(q.Get("groups")), IDs: idsParam(q.Get("ids")),
		RatingMin: queryInt(r, "rating"), Favorite: queryBool(r, "favorite"), Unwatched: queryBool(r, "unwatched"), Watched: queryBool(r, "watched"),
		InProgress: queryBool(r, "progress"), VR: q.Get("vr"), MinHeight: queryInt(r, "min_h"), MaxHeight: queryInt(r, "max_h"),
		MinDuration: queryInt(r, "min_dur") * 60, MaxDuration: queryInt(r, "max_dur") * 60, Organized: q.Get("organized"),
		HasMarkers: queryBool(r, "markers"), Folder: q.Get("folder"), Missing: queryBool(r, "missing"), Trashed: queryBool(r, "trash"),
		Hidden: queryBool(r, "hidden"), Sort: q.Get("sort"), Desc: q.Get("dir") != "asc", Seed: int64(queryInt(r, "seed"))}
	if f.Sort == "" {
		st, _ := s.store.GetSettings(r.Context(), f.UserID)
		f.Sort = st.DefaultSort
	}
	if f.Sort == "title" || f.Sort == "path" {
		f.Desc = q.Get("dir") == "desc"
	}
	per := queryInt(r, "per")
	if per <= 0 || per > 500 {
		per = 60
	}
	f.Limit = per
	if p := queryInt(r, "page"); p > 1 {
		f.Offset = (p - 1) * per
	}
	return f
}

func (s *Server) handleListScenes(w http.ResponseWriter, r *http.Request) {
	f := s.sceneFilter(r)
	list, total, err := s.store.ListScenes(r.Context(), f)
	if err != nil {
		s.fail(w, err, "scenes")
		return
	}
	s.decorateAll(list)
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total, "page": f.Offset/f.Limit + 1, "per": f.Limit})
}

// handleSceneIDs is the whole filtered list as ids: the player's queue.
func (s *Server) handleSceneIDs(w http.ResponseWriter, r *http.Request) {
	f := s.sceneFilter(r)
	if g := idsParam(r.URL.Query().Get("group")); len(g) == 1 {
		ids, err := s.store.GroupSceneIDs(r.Context(), g[0])
		if err != nil {
			s.fail(w, err, "queue")
			return
		}
		writeJSON(w, http.StatusOK, ids)
		return
	}
	ids, err := s.store.SceneIDs(r.Context(), f, 2000)
	if err != nil {
		s.fail(w, err, "queue")
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	writeJSON(w, http.StatusOK, ids)
}

func (s *Server) sceneFor(w http.ResponseWriter, r *http.Request) (*store.Scene, bool) {
	sc, err := s.store.GetScene(r.Context(), userFrom(r.Context()).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "scene")
		return nil, false
	}
	return sc, true
}

func (s *Server) handleGetScene(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	s.decorate(sc)
	s.workMu.Lock()
	working := s.working[sc.ID]
	s.workMu.Unlock()
	file := map[string]any{"path": sc.Path, "size_bytes": sc.SizeBytes, "duration": sc.Duration, "width": sc.Width, "height": sc.Height,
		"fps": sc.FPS, "video_codec": sc.VideoCodec, "audio_codec": sc.AudioCodec, "container": sc.Container, "bitrate": sc.Bitrate,
		"oshash": sc.OSHash, "phash": strings.Trim(sc.PHash, "-"), "mtime": sc.FileMtime, "working": working}
	playback := s.playback(sc, r)
	writeJSON(w, http.StatusOK, map[string]any{"scene": sc, "file": file, "playback": playback})
}

// ---------- editing ----------

func (s *Server) canDelete(w http.ResponseWriter, r *http.Request, sc *store.Scene) bool {
	u := userFrom(r.Context())
	if u.Role == "admin" || (sc.AddedBy != nil && *sc.AddedBy == u.ID) {
		return true
	}
	writeErr(w, http.StatusForbidden, "only an admin or the person who added it can delete this")
	return false
}

type sceneInput struct {
	Title         string            `json:"title"`
	Code          string            `json:"code"`
	Details       string            `json:"details"`
	Director      string            `json:"director"`
	Date          string            `json:"date"`
	URLs          []string          `json:"urls"`
	StudioID      int64             `json:"studio_id"`
	Studio        string            `json:"studio"` // a new studio by name
	Organized     bool              `json:"organized"`
	Performers    []int64           `json:"performers"`
	NewPerformers []string          `json:"new_performers"`
	Tags          []int64           `json:"tags"`
	NewTags       []string          `json:"new_tags"`
	Groups        map[string]int    `json:"groups"` // group id → scene number
	Projection    string            `json:"projection"`
	Stereo        string            `json:"stereo"`
	FOV           int               `json:"fov"`
	VRAuto        bool              `json:"vr_auto"` // forget the hand-picked shape
	StashIDs      map[string]string `json:"stash_ids"`
}

func validLooseDate(d string) bool {
	if d == "" {
		return true
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if _, err := time.Parse(layout, d); err == nil {
			return true
		}
	}
	return false
}

func (s *Server) handleUpdateScene(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	var in sceneInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := store.ScenePatch{}
	text := func(k string, dst **string, v string) {
		if keys[k] {
			*dst = strp(v)
		}
	}
	text("title", &p.Title, in.Title)
	text("code", &p.Code, in.Code)
	text("details", &p.Details, in.Details)
	text("director", &p.Director, in.Director)
	if keys["date"] {
		if !validLooseDate(strings.TrimSpace(in.Date)) {
			writeErr(w, http.StatusBadRequest, "the date should look like 2024-05-31")
			return
		}
		p.Date = strp(in.Date)
	}
	if keys["urls"] {
		p.URLs = &in.URLs
	}
	if keys["organized"] {
		p.Organized = &in.Organized
	}
	if keys["stash_ids"] {
		p.StashIDs = &in.StashIDs
	}
	if keys["studio_id"] || keys["studio"] {
		var sid *int64
		if in.StudioID > 0 {
			sid = &in.StudioID
		} else if name := strings.TrimSpace(in.Studio); name != "" {
			id, _, err := s.store.EnsureStudio(ctx, name)
			if err != nil {
				s.fail(w, err, "studio")
				return
			}
			sid = &id
		}
		p.StudioID = &sid
	}
	if keys["performers"] || keys["new_performers"] {
		ids := append([]int64{}, in.Performers...)
		for _, n := range in.NewPerformers {
			if strings.TrimSpace(n) == "" {
				continue
			}
			id, _, err := s.store.EnsurePerformer(ctx, n)
			if err != nil {
				s.fail(w, err, "performer")
				return
			}
			ids = append(ids, id)
		}
		p.Performers = &ids
	}
	if keys["tags"] || keys["new_tags"] {
		ids := append([]int64{}, in.Tags...)
		for _, n := range in.NewTags {
			if strings.TrimSpace(n) == "" {
				continue
			}
			id, _, err := s.store.EnsureTag(ctx, n)
			if err != nil {
				s.fail(w, err, "tag")
				return
			}
			ids = append(ids, id)
		}
		p.Tags = &ids
	}
	regen := false
	if in.VRAuto {
		p.VRSet = boolp(false)
		sh := media.DetectShape(sc.Path, sc.Width, sc.Height, "", "")
		proj, fov := sh.Projection, sh.FOV
		if proj == "flat" {
			proj, fov = "", 0
		}
		p.Projection, p.Stereo, p.FOV = &proj, &sh.Stereo, &fov
		regen = proj != sc.Projection || sh.Stereo != sc.Stereo
	} else if keys["projection"] || keys["stereo"] || keys["fov"] {
		proj := sc.Projection
		if keys["projection"] {
			switch in.Projection {
			case "flat", "":
				proj = ""
			case "equirect180", "equirect360", "fisheye":
				proj = in.Projection
			default:
				writeErr(w, http.StatusBadRequest, "unknown projection")
				return
			}
		}
		stereo := sc.Stereo
		if keys["stereo"] {
			switch in.Stereo {
			case "mono", "sbs", "tb":
				stereo = in.Stereo
			default:
				writeErr(w, http.StatusBadRequest, "unknown stereo layout")
				return
			}
		}
		fov := sc.FOV
		if keys["fov"] && in.FOV >= 0 && in.FOV <= 360 {
			fov = in.FOV
		}
		if proj == "fisheye" && fov < 180 {
			fov = 190
		}
		p.Projection, p.Stereo, p.FOV, p.VRSet = &proj, &stereo, &fov, boolp(true)
		regen = proj != sc.Projection || stereo != sc.Stereo || fov != sc.FOV
	}
	if err := s.store.UpdateScene(ctx, sc.ID, p); err != nil {
		s.fail(w, err, "save scene")
		return
	}
	if keys["groups"] {
		m := map[int64]int{}
		for k, v := range in.Groups {
			if id, err := strconv.ParseInt(k, 10, 64); err == nil {
				m[id] = v
			}
		}
		if err := s.store.SetSceneGroups(ctx, sc.ID, m); err != nil {
			s.fail(w, err, "groups")
			return
		}
	}
	if regen {
		// the pictures were made for the old shape
		if sc.CoverSource == "frame" {
			_ = s.store.SetSceneCover(ctx, sc.ID, "", "")
			s.removeFile(sc.CoverPath)
		}
		_ = s.store.ResetGenerated(ctx, []int64{sc.ID})
		s.wakeWork()
	}
	s.handleGetScene(w, r)
}

func boolp(b bool) *bool    { return &b }
func strp(v string) *string { return &v }
func i64p(v int64) *int64   { return &v }

func (s *Server) handlePutMine(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Rating        int     `json:"rating"`
		Favorite      bool    `json:"favorite"`
		Hidden        bool    `json:"hidden"`
		ResumeSeconds float64 `json:"resume_seconds"`
		PlayCount     int     `json:"play_count"`
	}
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := store.MinePatch{}
	if keys["rating"] {
		if in.Rating < 0 || in.Rating > 10 {
			writeErr(w, http.StatusBadRequest, "rating is 0 to 10 (half stars)")
			return
		}
		p.Rating = &in.Rating
	}
	if keys["favorite"] {
		p.Favorite = &in.Favorite
	}
	if keys["hidden"] {
		p.Hidden = &in.Hidden
	}
	if keys["resume_seconds"] {
		p.ResumeSeconds = &in.ResumeSeconds
	}
	if keys["play_count"] {
		p.PlayCount = &in.PlayCount
	}
	if err := s.store.PutMine(ctx, userFrom(ctx).ID, sc.ID, p); err != nil {
		s.fail(w, err, "save")
		return
	}
	sc, _ = s.store.GetScene(ctx, userFrom(ctx).ID, sc.ID)
	writeJSON(w, http.StatusOK, sc.Mine)
}

func (s *Server) handleAddO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	n, err := s.store.AddO(ctx, userFrom(ctx).ID, sc.ID, "")
	if err != nil {
		s.fail(w, err, "o")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"o_count": n})
}

func (s *Server) handleRemoveO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	n, err := s.store.RemoveO(ctx, userFrom(ctx).ID, sc.ID)
	if err != nil {
		s.fail(w, err, "o")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"o_count": n})
}

// ---------- watching ----------

func (s *Server) handleStartPlay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Device string `json:"device"`
	}
	_ = decode(r, &in)
	if in.Device != "headset" {
		in.Device = "browser"
	}
	id, err := s.store.StartPlay(ctx, userFrom(ctx).ID, sc.ID, in.Device)
	if err != nil {
		s.fail(w, err, "play")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleBeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Watched  int64   `json:"watched"`  // seconds watched in this sitting
		Position float64 `json:"position"` // where the player is
		Duration float64 `json:"duration"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	counted, err := s.store.BeatPlay(ctx, u.ID, idParam(r, "pid"), in.Watched, in.Position, in.Duration, st.CountAfter)
	if err != nil {
		s.fail(w, err, "play")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"counted": counted})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	h, err := s.store.History(ctx, userFrom(ctx).ID, sc.ID)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) handleDeletePlay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeletePlay(ctx, userFrom(ctx).ID, idParam(r, "pid")); err != nil {
		s.fail(w, err, "play")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRecentPlays(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.RecentPlays(ctx, userFrom(ctx).ID, 200)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// ---------- covers ----------

func (s *Server) handleSceneCover(w http.ResponseWriter, r *http.Request) {
	var rel string
	if err := s.store.DB.QueryRowContext(r.Context(), `SELECT cover_path FROM scenes WHERE id = ?`, idParam(r, "id")).Scan(&rel); err != nil || rel == "" {
		writeErr(w, http.StatusNotFound, "no cover")
		return
	}
	s.servePicture(w, r, rel)
}

func (s *Server) servePicture(w http.ResponseWriter, r *http.Request, rel string) {
	if rel == "" {
		writeErr(w, http.StatusNotFound, "no picture")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, s.filePath(rel))
}

// handleSetSceneCover takes a picture (raw body), a link ({"url"}) or a frame
// of the video ({"at": seconds}).
func (s *Server) handleSetSceneCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	var b []byte
	var err error
	source := "upload"
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var in struct {
			URL string   `json:"url"`
			At  *float64 `json:"at"`
		}
		if err := decode(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		switch {
		case in.At != nil:
			b, err = s.tools.Screenshot(ctx, s.scenePath(sc), *in.At, shapeOf(sc), 1600, 900)
			source = "frame"
		case in.URL != "":
			b, err = s.fetchPicture(ctx, userFrom(ctx), in.URL)
			source = "link"
		default:
			writeErr(w, http.StatusBadRequest, "send a picture, a link or a time")
			return
		}
	} else {
		b, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 25<<20))
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.setSceneCover(ctx, sc, b, source); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.handleGetScene(w, r)
}

func (s *Server) handleDeleteSceneCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	_ = s.store.SetSceneCover(ctx, sc.ID, "", "")
	s.removeFile(sc.CoverPath)
	// a picture from the video comes back with the next pass
	if p := coverSidecar(s.scenePath(sc)); p == "" {
		_ = s.store.ResetGenerated(ctx, []int64{sc.ID})
		s.wakeWork()
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- life ----------

func (s *Server) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Cover bool `json:"cover"` // also a new picture from the video
		Hash  bool `json:"hash"`
	}
	_ = decode(r, &in)
	if in.Cover && sc.CoverSource == "frame" {
		_ = s.store.SetSceneCover(ctx, sc.ID, "", "")
		s.removeFile(sc.CoverPath)
	}
	if in.Hash {
		_ = s.store.SetPHash(ctx, sc.ID, "")
	}
	_ = s.store.ResetGenerated(ctx, []int64{sc.ID})
	s.wakeWork()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleRescanScene(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	_, _ = s.store.DB.ExecContext(ctx, `UPDATE scenes SET probed_at = '' WHERE id = ?`, sc.ID)
	s.readSidecars(ctx, sc.ID)
	s.wakeWork()
	s.handleGetScene(w, r)
}

func (s *Server) handleDeleteScene(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok || !s.canDelete(w, r, sc) {
		return
	}
	if sc.TrashedAt != "" {
		writeErr(w, http.StatusConflict, "already in the trash")
		return
	}
	if sc.MissingSince != "" {
		// the file is gone already: nothing to move
		if err := s.store.TrashScene(ctx, sc.ID, ""); err != nil {
			s.fail(w, err, "delete")
			return
		}
	} else if err := s.trashScene(ctx, sc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestoreScene(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok || !s.canDelete(w, r, sc) {
		return
	}
	if sc.TrashedAt == "" {
		writeErr(w, http.StatusConflict, "not in the trash")
		return
	}
	if err := s.restoreScene(ctx, sc); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.wakeScan()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePurgeScene(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.requireAdmin(w, r) {
		return
	}
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	if sc.TrashedAt == "" && sc.MissingSince == "" {
		writeErr(w, http.StatusConflict, "move it to the trash first")
		return
	}
	if err := s.purgeScene(ctx, sc); err != nil {
		s.fail(w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBulk changes many scenes at once.
func (s *Server) handleBulk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		IDs              []int64 `json:"ids"`
		AddTags          []int64 `json:"add_tags"`
		RemoveTags       []int64 `json:"remove_tags"`
		AddPerformers    []int64 `json:"add_performers"`
		RemovePerformers []int64 `json:"remove_performers"`
		StudioID         *int64  `json:"studio_id"` // 0 = none
		Organized        *bool   `json:"organized"`
		Rating           *int    `json:"rating"`
		Favorite         *bool   `json:"favorite"`
		Hidden           *bool   `json:"hidden"`
		Regenerate       bool    `json:"regenerate"`
		Identify         bool    `json:"identify"`
		Trash            bool    `json:"trash"`
	}
	if err := decode(r, &in); err != nil || len(in.IDs) == 0 || len(in.IDs) > 2000 {
		writeErr(w, http.StatusBadRequest, "choose scenes first")
		return
	}
	done := 0
	var failed []string
	for _, id := range in.IDs {
		sc, err := s.store.GetScene(ctx, u.ID, id)
		if err != nil {
			continue
		}
		if len(in.AddTags) > 0 || len(in.AddPerformers) > 0 {
			_ = s.store.AddSceneRelations(ctx, id, in.AddPerformers, in.AddTags)
		}
		if len(in.RemoveTags) > 0 || len(in.RemovePerformers) > 0 {
			for _, t := range in.RemoveTags {
				_, _ = s.store.DB.ExecContext(ctx, `DELETE FROM scene_tags WHERE scene_id = ? AND tag_id = ?`, id, t)
			}
			for _, p := range in.RemovePerformers {
				_, _ = s.store.DB.ExecContext(ctx, `DELETE FROM scene_performers WHERE scene_id = ? AND performer_id = ?`, id, p)
			}
		}
		p := store.ScenePatch{Organized: in.Organized}
		if in.StudioID != nil {
			var sid *int64
			if *in.StudioID > 0 {
				sid = in.StudioID
			}
			p.StudioID = &sid
		}
		_ = s.store.UpdateScene(ctx, id, p)
		if in.Rating != nil || in.Favorite != nil || in.Hidden != nil {
			_ = s.store.PutMine(ctx, u.ID, id, store.MinePatch{Rating: in.Rating, Favorite: in.Favorite, Hidden: in.Hidden})
		}
		if in.Regenerate {
			_ = s.store.ResetGenerated(ctx, []int64{id})
		}
		if in.Trash && sc.TrashedAt == "" && (u.Role == "admin" || (sc.AddedBy != nil && *sc.AddedBy == u.ID)) {
			var err error
			if sc.MissingSince != "" {
				err = s.store.TrashScene(ctx, id, "")
			} else {
				err = s.trashScene(ctx, sc)
			}
			if err != nil {
				failed = append(failed, sc.Path)
				continue
			}
		}
		done++
	}
	if in.Regenerate {
		s.wakeWork()
	}
	if in.Identify {
		ids := in.IDs
		go s.identifyMany(context.WithoutCancel(ctx), u.ID, ids)
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": done, "failed": failed})
}

// ---------- home, dashboard, folders, duplicates ----------

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.GetDashboard(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err, "dashboard")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := userFrom(ctx).ID
	shelf := func(f store.SceneFilter) []store.Scene {
		f.UserID = uid
		if f.Limit == 0 {
			f.Limit = 18
		}
		list, _, _ := s.store.ListScenes(ctx, f)
		s.decorateAll(list)
		return list
	}
	perfs, _, _ := s.store.ListPerformers(ctx, store.PerformerFilter{UserID: uid, Favorite: true, Sort: "scenes", Desc: true, Limit: 18})
	if len(perfs) == 0 {
		perfs, _, _ = s.store.ListPerformers(ctx, store.PerformerFilter{UserID: uid, Sort: "scenes", Desc: true, Limit: 18})
	}
	s.decoratePerformers(perfs)
	seed := time.Now().YearDay()*24 + time.Now().Hour()
	writeJSON(w, http.StatusOK, map[string]any{
		"continue":   shelf(store.SceneFilter{InProgress: true, Sort: "played", Desc: true}),
		"recent":     shelf(store.SceneFilter{Sort: "added", Desc: true}),
		"vr":         shelf(store.SceneFilter{VR: "yes", Sort: "added", Desc: true}),
		"favorites":  shelf(store.SceneFilter{Favorite: true, Sort: "random", Seed: int64(seed)}),
		"unwatched":  shelf(store.SceneFilter{Unwatched: true, Sort: "random", Seed: int64(seed)}),
		"top_rated":  shelf(store.SceneFilter{RatingMin: 8, Sort: "rating", Desc: true}),
		"performers": perfs,
	})
}

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.Folders(r.Context())
	if err != nil {
		s.fail(w, err, "folders")
		return
	}
	type folder struct {
		Path   string `json:"path"`
		Scenes int    `json:"scenes"`
	}
	// count each folder with everything under it, so parents are listed too
	total := map[string]int{}
	for p, n := range m {
		for {
			total[p] += n
			i := strings.LastIndex(p, "/")
			if i < 0 {
				break
			}
			p = p[:i]
		}
	}
	out := []folder{}
	for p, n := range total {
		out = append(out, folder{p, n})
	}
	sort.Slice(out, func(i, j int) bool { return naturalLess(out[i].Path, out[j].Path) })
	writeJSON(w, http.StatusOK, out)
}

// handleDuplicates groups scenes whose pictures look the same (phash within
// the distance) — the same video in two files, maybe of different quality.
func (s *Server) handleDuplicates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dist := queryInt(r, "distance")
	if dist <= 0 || dist > 16 {
		dist = 4
	}
	hashes, err := s.store.PHashes(ctx)
	if err != nil {
		s.fail(w, err, "duplicates")
		return
	}
	ids := make([]int64, 0, len(hashes))
	for id, h := range hashes {
		if h != "-" {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if p, ok := parent[x]; ok && p != x {
			parent[x] = find(p)
			return parent[x]
		}
		return x
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if media.HashDistance(hashes[ids[i]], hashes[ids[j]]) <= dist {
				a, b := find(ids[i]), find(ids[j])
				if a != b {
					parent[b] = a
				}
			}
		}
	}
	groups := map[int64][]int64{}
	for _, id := range ids {
		root := find(id)
		groups[root] = append(groups[root], id)
	}
	out := [][]store.Scene{}
	uid := userFrom(ctx).ID
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		list, _, err := s.store.ListScenes(ctx, store.SceneFilter{UserID: uid, IDs: members, Hidden: true, Sort: "resolution", Desc: true, Limit: 50})
		if err != nil || len(list) < 2 {
			continue
		}
		s.decorateAll(list)
		out = append(out, list)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0].ID < out[j][0].ID })
	writeJSON(w, http.StatusOK, out)
}

// ---------- stats ----------

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	days := queryInt(r, "days")
	if days <= 0 || days > 3660 {
		days = 30
	}
	st, err := s.store.GetStats(ctx, userFrom(ctx).ID, store.AddDays(store.Today(), -(days-1)))
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	for i := range st.TopPerformers {
		if st.TopPerformers[i].Image != "" {
			st.TopPerformers[i].Image = fmt.Sprintf("/api/v1/performers/%d/image?v=%s", st.TopPerformers[i].ID, coverVersion(st.TopPerformers[i].Image))
		}
	}
	for i := range st.TopStudios {
		if st.TopStudios[i].Image != "" {
			st.TopStudios[i].Image = fmt.Sprintf("/api/v1/studios/%d/image?v=%s", st.TopStudios[i].ID, coverVersion(st.TopStudios[i].Image))
		}
	}
	for i := range st.TopScenes {
		if st.TopScenes[i].Name != "" && strings.Contains(st.TopScenes[i].Name, "/") {
			st.TopScenes[i].Name = niceName(st.TopScenes[i].Name)
		}
	}
	writeJSON(w, http.StatusOK, st)
}

// ---------- saved filters ----------

func (s *Server) handleListFilters(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListFilters(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err, "filters")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveFilter(w http.ResponseWriter, r *http.Request) {
	var f store.SavedFilter
	if err := decode(r, &f); err != nil || strings.TrimSpace(f.Name) == "" {
		writeErr(w, http.StatusBadRequest, "give the view a name")
		return
	}
	f.Name = oneLine(f.Name, 80)
	switch f.Mode {
	case "scenes", "performers", "markers", "galleries":
	default:
		f.Mode = "scenes"
	}
	f.ID = idParam(r, "id")
	if err := s.store.SaveFilter(r.Context(), userFrom(r.Context()).ID, &f); err != nil {
		s.fail(w, err, "filter")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFilter(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFilter(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "filter")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- library ----------

func (s *Server) handleLibraryStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	probe, gen, hash := s.store.WorkLeft(ctx)
	ok, ver, terr := s.tools.Ready()
	s.workMu.Lock()
	working := map[string]string{}
	for id, what := range s.working {
		working[strconv.FormatInt(id, 10)] = what
	}
	s.workMu.Unlock()
	out := map[string]any{"scan": s.scanState(), "to_read": probe, "to_make": gen, "to_hash": hash, "working": working,
		"ffmpeg": ok, "ffmpeg_version": ver, "streams": s.streams.Load(), "hwaccel": s.cfg.HWAccel, "previews": s.cfg.Previews,
		"free_bytes": diskFree(s.cfg.LibraryDir)}
	if terr != nil {
		out["ffmpeg_error"] = terr.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	s.wakeScan()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleRegenerateAll(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var in struct {
		Missing bool `json:"missing"` // only scenes whose pictures failed or are missing
	}
	_ = decode(r, &in)
	q := `UPDATE scenes SET gen_state = '' WHERE trashed_at = ''`
	if in.Missing {
		q += ` AND (gen_state = 'failed' OR has_sprite = 0 OR has_preview = 0)`
	}
	if _, err := s.store.DB.ExecContext(r.Context(), q); err != nil {
		s.fail(w, err, "regenerate")
		return
	}
	s.wakeWork()
	w.WriteHeader(http.StatusAccepted)
}
