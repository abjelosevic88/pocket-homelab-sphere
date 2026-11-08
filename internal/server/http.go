package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	r.Get("/pocket-ask.js", s.core.AskWidget("sphere"))
	r.Get("/app-signin", s.handleAppSignin)
	r.Post("/app-signin", s.handleAppSigninAllow)
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Post("/auth/app/exchange", s.handleAppExchange)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/home", s.handleHome)
			r.Get("/dashboard", s.handleDashboard)
			r.Get("/stats", s.handleStats)
			r.Get("/history", s.handleRecentPlays)

			r.Get("/scenes", s.handleListScenes)
			r.Get("/scenes/ids", s.handleSceneIDs)
			r.Post("/scenes/bulk", s.handleBulk)
			r.Get("/scenes/{id}", s.handleGetScene)
			r.Put("/scenes/{id}", s.handleUpdateScene)
			r.Delete("/scenes/{id}", s.handleDeleteScene)
			r.Put("/scenes/{id}/mine", s.handlePutMine)
			r.Post("/scenes/{id}/o", s.handleAddO)
			r.Delete("/scenes/{id}/o", s.handleRemoveO)
			r.Post("/scenes/{id}/plays", s.handleStartPlay)
			r.Get("/scenes/{id}/history", s.handleHistory)
			r.Get("/scenes/{id}/cover", s.handleSceneCover)
			r.Post("/scenes/{id}/cover", s.handleSetSceneCover)
			r.Delete("/scenes/{id}/cover", s.handleDeleteSceneCover)
			r.Get("/scenes/{id}/stream", s.handleStream)
			r.Head("/scenes/{id}/stream", s.handleStream)
			r.Get("/scenes/{id}/stream.mp4", s.handleConverted)
			r.Get("/scenes/{id}/stream.m3u8", s.handleHLSPlaylist)
			r.Get("/scenes/{id}/hls/{seg}", s.handleHLSSegment)
			r.Get("/scenes/{id}/preview.mp4", s.handlePreview)
			r.Get("/scenes/{id}/sprite.jpg", s.handleSprite)
			r.Get("/scenes/{id}/sprite.json", s.handleSpriteMap)
			r.Get("/scenes/{id}/frame", s.handleFrame)
			r.Post("/scenes/{id}/regenerate", s.handleRegenerate)
			r.Post("/scenes/{id}/rescan", s.handleRescanScene)
			r.Post("/scenes/{id}/restore", s.handleRestoreScene)
			r.Delete("/scenes/{id}/purge", s.handlePurgeScene)
			r.Get("/scenes/{id}/identify", s.handleIdentify)
			r.Post("/scenes/{id}/identify", s.handleApplyIdentify)
			r.Get("/scenes/{id}/markers", s.handleListMarkers)
			r.Post("/scenes/{id}/markers", s.handleSaveMarker)
			r.Put("/plays/{pid}", s.handleBeat)
			r.Delete("/plays/{pid}", s.handleDeletePlay)

			r.Get("/markers", s.handleListMarkers)
			r.Put("/markers/{mid}", s.handleSaveMarker)
			r.Delete("/markers/{id}", s.handleDeleteMarker)
			r.Get("/markers/{id}/clip.mp4", s.handleMarkerMedia)
			r.Get("/markers/{id}/picture.jpg", s.handleMarkerMedia)

			r.Get("/performers", s.handleListPerformers)
			r.Post("/performers", s.handleSavePerformer)
			r.Get("/performers/lookup", s.handlePerformerLookup)
			r.Get("/performers/{id}", s.handleGetPerformer)
			r.Put("/performers/{id}", s.handleSavePerformer)
			r.Delete("/performers/{id}", s.handleDeletePerformer)
			r.Put("/performers/{id}/mine", s.handleMark("performer"))
			r.Post("/performers/{id}/merge", s.handleMergePerformers)
			r.Post("/performers/{id}/apply", s.handlePerformerApply)
			r.Get("/performers/{id}/image", s.handleImage("performers"))
			r.Post("/performers/{id}/image", s.handleSetImage("performers"))
			r.Delete("/performers/{id}/image", s.handleDeleteImage("performers"))

			r.Get("/studios", s.handleListStudios)
			r.Post("/studios", s.handleSaveStudio)
			r.Get("/studios/{id}", s.handleGetStudio)
			r.Put("/studios/{id}", s.handleSaveStudio)
			r.Delete("/studios/{id}", s.handleDeleteStudio)
			r.Put("/studios/{id}/mine", s.handleMark("studio"))
			r.Get("/studios/{id}/image", s.handleImage("studios"))
			r.Post("/studios/{id}/image", s.handleSetImage("studios"))
			r.Delete("/studios/{id}/image", s.handleDeleteImage("studios"))

			r.Get("/tags", s.handleListTags)
			r.Post("/tags", s.handleSaveTag)
			r.Get("/tags/{id}", s.handleGetTag)
			r.Put("/tags/{id}", s.handleSaveTag)
			r.Delete("/tags/{id}", s.handleDeleteTag)
			r.Put("/tags/{id}/mine", s.handleMark("tag"))
			r.Post("/tags/{id}/merge", s.handleMergeTags)
			r.Get("/tags/{id}/image", s.handleImage("tags"))
			r.Post("/tags/{id}/image", s.handleSetImage("tags"))
			r.Delete("/tags/{id}/image", s.handleDeleteImage("tags"))

			r.Get("/groups", s.handleListGroups)
			r.Post("/groups", s.handleSaveGroup)
			r.Get("/groups/{id}", s.handleGetGroup)
			r.Put("/groups/{id}", s.handleSaveGroup)
			r.Delete("/groups/{id}", s.handleDeleteGroup)
			r.Put("/groups/{id}/mine", s.handleMark("group"))
			r.Get("/groups/{id}/front", s.handleGroupImage(false))
			r.Get("/groups/{id}/back", s.handleGroupImage(true))
			r.Post("/groups/{id}/image", s.handleSetGroupImage)
			r.Delete("/groups/{id}/image", s.handleSetGroupImage)

			r.Get("/galleries", s.handleListGalleries)
			r.Get("/galleries/{id}", s.handleGetGallery)
			r.Put("/galleries/{id}", s.handleUpdateGallery)
			r.Put("/galleries/{id}/mine", s.handleMark("gallery"))
			r.Get("/galleries/{id}/cover", s.handleGalleryCover)
			r.Get("/images/{id}", s.handleImageFile)

			r.Get("/performers/{id}/discover", s.handleDiscover("performer"))
			r.Get("/studios/{id}/discover", s.handleDiscover("studio"))
			r.Get("/performers/{id}/follow", s.handleFollowState("performer"))
			r.Get("/studios/{id}/follow", s.handleFollowState("studio"))
			r.Get("/wanted", s.handleListWanted)
			r.Post("/wanted", s.handleAddWanted)
			r.Delete("/wanted/{id}", s.handleDeleteWanted)
			r.Get("/follows", s.handleListFollows)
			r.Post("/follows", s.handleAddFollow)
			r.Delete("/follows/{id}", s.handleDeleteFollow)
			r.Post("/follows/{id}/check", s.handleCheckFollow)
			r.Get("/releases", s.handleListReleases)
			r.Post("/releases/dismiss", s.handleDismissRelease)
			r.Post("/releases/{id}/dismiss", s.handleDismissRelease)

			r.Get("/filters", s.handleListFilters)
			r.Post("/filters", s.handleSaveFilter)
			r.Put("/filters/{id}", s.handleSaveFilter)
			r.Delete("/filters/{id}", s.handleDeleteFilter)

			r.Get("/library", s.handleLibraryStatus)
			r.Post("/library/scan", s.handleScan)
			r.Post("/library/regenerate", s.handleRegenerateAll)
			r.Post("/library/autotag", s.handleAutoTag)
			r.Get("/folders", s.handleFolders)
			r.Get("/duplicates", s.handleDuplicates)

			r.Get("/downloads", s.handleListDownloads)
			r.Post("/downloads", s.handleCreateDownload)
			r.Post("/downloads/clear", s.handleClearDownloads)
			r.Get("/downloads/tool", s.handleYTDLPStatus)
			r.Post("/downloads/tool/update", s.handleYTDLPUpdate)
			r.Put("/downloads/cookies", s.handleCookies)
			r.Delete("/downloads/cookies", s.handleCookies)
			r.Put("/downloads/{id}", s.handleUpdateDownload)
			r.Delete("/downloads/{id}", s.handleDeleteDownload)
			r.Get("/subscriptions", s.handleListSubscriptions)
			r.Post("/subscriptions", s.handleSaveSubscription)
			r.Put("/subscriptions/{id}", s.handleSaveSubscription)
			r.Delete("/subscriptions/{id}", s.handleDeleteSubscription)
			r.Post("/subscriptions/{id}/check", s.handleCheckSubscription)

			r.Post("/import/stash", s.handleImportStash)

			r.Post("/integrations/test", s.handleIntegrationTest)
			r.Get("/ai/status", s.handleAIStatus)
			r.Get("/ai/models", s.handleAIModels)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)
			r.Get("/settings/headset", s.handleHeadset)
			r.Post("/settings/headset", s.handleHeadset)
			r.Post("/lock", s.handleLock)
			r.Put("/lock", s.handleLock)
			r.Delete("/lock", s.handleLock)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.json", s.handleExportJSON)
			r.Post("/import", s.handleImport)
		})
	})

	// VR players on a headset (DeoVR, HereSphere) read their own JSON
	if s.cfg.HeadsetAPIs {
		s.mountHeadset(r)
	}

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/api/v1/plays/") || strings.Contains(r.URL.Path, "/preview.mp4") || strings.Contains(r.URL.Path, "/cover") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict), errors.Is(err, core.ErrOperationConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20))
	return dec.Decode(v)
}

// decodeKeys decodes JSON into v and also reports which top-level keys were
// present, so PUT handlers can tell "not sent" from "sent empty".
func decodeKeys(r *http.Request, v any) (map[string]bool, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, json.Unmarshal(b, v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_sphere_info Build info.\n# TYPE pocket_sphere_info gauge\npocket_sphere_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_sphere_uptime_seconds Process uptime.\n# TYPE pocket_sphere_uptime_seconds gauge\npocket_sphere_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_sphere_http_requests_total HTTP requests served.\n# TYPE pocket_sphere_http_requests_total counter\npocket_sphere_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_sphere_http_errors_total HTTP 5xx responses.\n# TYPE pocket_sphere_http_errors_total counter\npocket_sphere_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_sphere_rows Rows per table.\n# TYPE pocket_sphere_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_sphere_rows{table=%q} %d\n", k, v)
	}
	st := s.scanState()
	fmt.Fprintf(w, "# HELP pocket_sphere_last_scan_ms Duration of the last library scan.\n# TYPE pocket_sphere_last_scan_ms gauge\npocket_sphere_last_scan_ms %d\n", st.Ms)
	fmt.Fprintf(w, "# HELP pocket_sphere_streams Videos being converted right now.\n# TYPE pocket_sphere_streams gauge\npocket_sphere_streams %d\n", s.streams.Load())
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

// ---------- files ----------

// saveFile writes bytes under FILES_DIR/<sub>/ with a random name and returns the relative path.
func (s *Server) saveFile(sub, ext string, data []byte) (string, error) {
	dir := filepath.Join(s.cfg.FilesDir, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext = strings.ToLower(filepath.Ext("x" + ext))
	if len(ext) > 8 {
		ext = ""
	}
	name := store.RandomToken(12) + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	return sub + "/" + name, nil
}

func (s *Server) filePath(rel string) string {
	rel = filepath.Clean("/" + rel)
	return filepath.Join(s.cfg.FilesDir, rel)
}

func (s *Server) removeFile(rel string) {
	if rel == "" {
		return
	}
	_ = os.Remove(s.filePath(rel))
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, rel string) {
	if rel == "" {
		writeErr(w, http.StatusNotFound, "no file")
		return
	}
	p := s.filePath(rel)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "file missing")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// a mislabelled file opened directly must not run as a page of this origin
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, p)
}

// oneLine squeezes whitespace and cuts the text to n characters.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}

// oneLineKeepBreaks trims a text (keeping its line breaks) to n characters.
func oneLineKeepBreaks(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}
