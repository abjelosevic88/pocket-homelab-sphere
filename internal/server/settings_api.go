package server

import (
	"context"

	"fmt"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/lookup"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with keys masked.
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	return map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat,
		"grid_size": st.GridSize, "default_sort": st.DefaultSort, "hover_preview": st.HoverPreview, "show_missing": st.ShowMissing,
		"discreet": st.Discreet, "panic_url": st.PanicURL, "lock_enabled": st.LockPINHash != "", "lock_minutes": st.LockMinutes,
		"headset_enabled": st.HeadsetKey != "",
		"auto_play":       st.AutoPlay, "continue_queue": st.ContinueQueue, "resume_play": st.ResumePlay, "player_volume": st.PlayerVolume,
		"stream_quality": st.StreamQuality, "skip_seconds": st.SkipSeconds, "vr_default_mode": st.VRDefaultMode, "count_after": st.CountAfter,
		"download_folder": st.DownloadFolder, "download_quality": st.DownloadQuality, "download_format": st.DownloadFormat,
		"own_folder": st.OwnFolder, "site_tags": st.SiteTags, "uploader_studio": st.UploaderStudio, "auto_identify": st.AutoIdentify,
		"stashdb_key": mask(st.StashDBKey), "stashbox_url": st.StashBoxURL, "tpdb_key": mask(st.TPDBKey),
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"notify_downloads": st.NotifyDownloads, "notify_quiet": st.NotifyQuiet,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel,
		"openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL, "headset_apis": s.cfg.HeadsetAPIs,
		"trash_days": s.cfg.TrashDays, "downloads_open": s.cfg.DownloadsOpen || u.Role == "admin",
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin and silently ignored for everyone else.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	intIn := func(k string, dst *int, v, lo, hi int) {
		if keys[k] && v >= lo && v <= hi {
			*dst = v
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	oneOf := func(k string, dst *string, v string, allowed ...string) {
		if !keys[k] {
			return
		}
		for _, a := range allowed {
			if v == a {
				*dst = v
			}
		}
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	text("date_format", &cur.DateFormat, in.DateFormat)
	text("time_format", &cur.TimeFormat, in.TimeFormat)
	oneOf("grid_size", &cur.GridSize, in.GridSize, "s", "m", "l")
	oneOf("default_sort", &cur.DefaultSort, in.DefaultSort, "added", "date", "title", "rating", "duration", "plays", "played", "o", "size", "resolution", "random", "path", "updated")
	boolIn("hover_preview", &cur.HoverPreview, in.HoverPreview)
	boolIn("show_missing", &cur.ShowMissing, in.ShowMissing)
	boolIn("discreet", &cur.Discreet, in.Discreet)
	if keys["panic_url"] {
		v := strings.TrimSpace(in.PanicURL)
		if v != "" && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
			writeErr(w, http.StatusBadRequest, "the escape page must be a link starting with https://")
			return
		}
		cur.PanicURL = v
	}
	intIn("lock_minutes", &cur.LockMinutes, in.LockMinutes, 0, 24*60)
	boolIn("auto_play", &cur.AutoPlay, in.AutoPlay)
	boolIn("continue_queue", &cur.ContinueQueue, in.ContinueQueue)
	boolIn("resume_play", &cur.ResumePlay, in.ResumePlay)
	intIn("player_volume", &cur.PlayerVolume, in.PlayerVolume, 0, 100)
	oneOf("stream_quality", &cur.StreamQuality, in.StreamQuality, "original", "2160", "1440", "1080", "720", "480")
	intIn("skip_seconds", &cur.SkipSeconds, in.SkipSeconds, 1, 120)
	oneOf("vr_default_mode", &cur.VRDefaultMode, in.VRDefaultMode, "view", "flat")
	intIn("count_after", &cur.CountAfter, in.CountAfter, 5, 3600)
	if keys["download_folder"] {
		f, err := cleanFolder(in.DownloadFolder)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		cur.DownloadFolder = f
	}
	oneOf("download_quality", &cur.DownloadQuality, in.DownloadQuality, "best", "2160", "1440", "1080", "720", "480")
	oneOf("download_format", &cur.DownloadFormat, in.DownloadFormat, "auto", "mp4", "mkv", "webm")
	boolIn("own_folder", &cur.OwnFolder, in.OwnFolder)
	boolIn("site_tags", &cur.SiteTags, in.SiteTags)
	boolIn("uploader_studio", &cur.UploaderStudio, in.UploaderStudio)
	boolIn("auto_identify", &cur.AutoIdentify, in.AutoIdentify)
	secret("stashdb_key", &cur.StashDBKey, in.StashDBKey)
	secret("tpdb_key", &cur.TPDBKey, in.TPDBKey)
	if keys["stashbox_url"] {
		v := strings.TrimRight(strings.TrimSpace(in.StashBoxURL), "/")
		if v != "" && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
			writeErr(w, http.StatusBadRequest, "the stash-box address starts with https://")
			return
		}
		cur.StashBoxURL = v
	}
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	boolIn("notify_downloads", &cur.NotifyDownloads, in.NotifyDownloads)
	boolIn("notify_quiet", &cur.NotifyQuiet, in.NotifyQuiet)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			writeErr(w, http.StatusBadRequest, "AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			writeErr(w, http.StatusBadRequest, "unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	if keys["openai_base_url"] {
		cur.OpenAIBaseURL = strings.TrimRight(strings.TrimSpace(in.OpenAIBaseURL), "/")
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return in.URL
	}
	var msg string
	var err error
	switch in.Which {
	case "openai":
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	case "notify":
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Sphere", Body: "Notifications work. Finished and failed downloads will arrive here.", Priority: "info", Tags: []string{"movie_camera"}})
		msg = "Test message sent via " + n.Format
	case "stashdb":
		sb := &lookup.StashBox{Endpoint: firstNonEmpty(strings.TrimSpace(in.URL), st.StashBoxURL, lookup.StashDB), Key: keyOr(st.StashDBKey), UA: s.cfg.UserAgent}
		var res []lookup.Performer
		if res, err = sb.SearchPerformer(ctx, "Riley Reid"); err == nil {
			msg = fmt.Sprintf("The key works (%d results for a test search)", len(res))
		}
	case "tpdb":
		tp := &lookup.TPDB{Key: keyOr(st.TPDBKey), UA: s.cfg.UserAgent}
		var res []lookup.Performer
		if res, err = tp.SearchPerformer(ctx, "Riley Reid"); err == nil {
			msg = fmt.Sprintf("The key works (%d results for a test search)", len(res))
		}
	default:
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// ---- system / backup ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size, files int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	_ = filepath.WalkDir(s.cfg.FilesDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				files += fi.Size()
			}
		}
		return nil
	})
	var gen int64
	_ = filepath.WalkDir(s.cfg.GeneratedDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				gen += fi.Size()
			}
		}
		return nil
	})
	u := userFrom(r.Context())
	out := map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "db_bytes": size, "files_bytes": files, "generated_bytes": gen, "free_bytes": diskFree(s.cfg.LibraryDir),
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "scan": s.scanState(), "hwaccel": s.cfg.HWAccel,
	}
	if u.Role == "admin" {
		out["data_dir"], out["db_path"], out["library_dir"], out["generated_dir"] = s.cfg.DataDir, s.cfg.DBPath, s.cfg.LibraryDir, s.cfg.GeneratedDir
	}
	writeJSON(w, http.StatusOK, out)
}

// handleBackup downloads the whole database, so it is a box operation for an
// admin only (everyone can export their own data as JSON).
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-sphere-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// ---- headset address ----

// handleHeadset shows (GET), makes a new (POST {"action":"new"}) or turns off
// ({"action":"off"}) the person's headset address.
func (s *Server) handleHeadset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, err := s.store.GetSettings(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	if r.Method == http.MethodPost {
		var in struct {
			Action string `json:"action"`
		}
		_ = decode(r, &in)
		switch in.Action {
		case "new":
			st.HeadsetKey = store.RandomToken(18)
		case "off":
			st.HeadsetKey = ""
		default:
			writeErr(w, http.StatusBadRequest, "unknown action")
			return
		}
		if err := s.store.SaveUserSettings(ctx, u.ID, st); err != nil {
			s.fail(w, err, "settings")
			return
		}
	}
	out := map[string]any{"enabled": st.HeadsetKey != "", "available": s.cfg.HeadsetAPIs}
	if st.HeadsetKey != "" {
		base := s.origin(r) + "/vr/" + st.HeadsetKey
		out["base"], out["deovr"], out["heresphere"] = base, base+"/deovr", base+"/heresphere"
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- app lock (a PIN on this device) ----

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, err := s.store.GetSettings(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in struct {
		PIN     string `json:"pin"`
		Current string `json:"current"`
	}
	_ = decode(r, &in)
	switch r.Method {
	case http.MethodPut: // set or change
		if st.LockPINHash != "" && bcrypt.CompareHashAndPassword([]byte(st.LockPINHash), []byte(in.Current)) != nil {
			writeErr(w, http.StatusForbidden, "the current PIN is not right")
			return
		}
		if len(in.PIN) < 4 || len(in.PIN) > 12 || strings.Trim(in.PIN, "0123456789") != "" {
			writeErr(w, http.StatusBadRequest, "the PIN is 4 to 12 digits")
			return
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(in.PIN), bcrypt.DefaultCost)
		st.LockPINHash = string(h)
	case http.MethodDelete:
		if st.LockPINHash != "" && bcrypt.CompareHashAndPassword([]byte(st.LockPINHash), []byte(in.Current)) != nil {
			writeErr(w, http.StatusForbidden, "the PIN is not right")
			return
		}
		st.LockPINHash = ""
	case http.MethodPost: // check
		if st.LockPINHash == "" || bcrypt.CompareHashAndPassword([]byte(st.LockPINHash), []byte(in.PIN)) == nil {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		time.Sleep(700 * time.Millisecond)
		writeErr(w, http.StatusForbidden, "wrong PIN")
		return
	}
	if err := s.store.SaveUserSettings(ctx, u.ID, st); err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"lock_enabled": st.LockPINHash != ""})
}
