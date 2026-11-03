// Package server wires the HTTP API, auth, the library scanner and the
// background work (probing, pictures, previews, fingerprints), the player's
// streams, downloads from links, the headset feeds and the embedded SPA.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/config"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/media"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/ytdlp"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	webFS    fs.FS
	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64

	tools *media.Tools
	yt    *ytdlp.Runner

	// the library scan: one at a time; wake = look now (after a download,
	// "scan now")
	scanMu   sync.Mutex
	scanWake chan struct{}
	scanInfo scanStatus
	infoMu   sync.Mutex
	// background work (probe, pictures, previews, fingerprints)
	workWake chan struct{}
	workMu   sync.Mutex
	working  map[int64]string // scene → what is being made

	// downloads from links: a nudge, and the running ones (to cancel them)
	dlWake    chan struct{}
	dlMu      sync.Mutex
	dlRunning map[string]context.CancelFunc

	// files made on request (marker clips, frames): one maker per file
	makeMu sync.Mutex
	making map[string]*sync.WaitGroup

	// live conversions for the player
	streams atomic.Int64

	oidcMu sync.Mutex
	oidc   *oidcProvider // for the issuer in use; rebuilt when it changes

	core    *core.Client
	coreMu  sync.Mutex
	coreOK  bool
	coreErr string
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	return &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(), oidc: &oidcProvider{},
		tools:     media.New(cfg.FFmpeg, cfg.FFprobe),
		yt:        ytdlp.New(cfg.YTDLP, cfg.DataDir, cfg.UserAgent),
		core:      core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: cfg.UserAgent}),
		scanWake:  make(chan struct{}, 1),
		workWake:  make(chan struct{}, 1),
		working:   map[int64]string{},
		dlWake:    make(chan struct{}, 1),
		dlRunning: map[string]context.CancelFunc{},
		making:    map[string]*sync.WaitGroup{}}
}

// Bootstrap seeds the first admin (if configured), the owner's integration
// settings from the environment and demo data, then starts the background work.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(s.cfg.FilesDir, 0o755); err != nil {
		return fmt.Errorf("files dir: %w", err)
	}
	if err := os.MkdirAll(s.cfg.LibraryDir, 0o755); err != nil {
		return fmt.Errorf("library dir: %w", err)
	}
	if err := os.MkdirAll(s.cfg.GeneratedDir, 0o755); err != nil {
		return fmt.Errorf("generated dir: %w", err)
	}
	if ok, v, err := s.tools.Ready(); ok {
		s.log.Info("ffmpeg found", "version", v)
	} else {
		s.log.Warn("ffmpeg missing: no pictures, previews or conversions", "err", err)
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	if owner := s.store.OwnerID(ctx); owner > 0 {
		if err := s.ownerReady(ctx, owner); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	s.recheckShapes(ctx)
	go s.scanner(ctx)
	for i := 0; i < s.cfg.GenerateWorkers; i++ {
		go s.worker(ctx)
	}
	go s.downloader(ctx)
	go s.subscriptions(ctx)
	go s.followChecker(ctx)
	go s.janitor(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// ownerReady runs once the box owner (the first admin) exists: the
// integration settings from the environment are the owner's — seeded into the
// owner's personal settings only, never into anyone else's — and demo data
// lands in the owner's account.
func (s *Server) ownerReady(ctx context.Context, owner int64) error {
	st, err := s.store.GetSettings(ctx, owner)
	if err != nil {
		return err
	}
	changed := false
	// Each value is copied once: clearing it in Settings must not bring it
	// back at the next start. A value already there counts as copied.
	once := func(name string, empty bool, v string, apply func()) {
		key := fmt.Sprintf("seeded:%d:%s", owner, name)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			return
		}
		if empty && v != "" {
			apply()
			changed = true
			empty = false
		}
		if !empty {
			_ = s.store.CachePut(ctx, key, "1")
		}
	}
	set := func(name string, dst *string, v string) {
		once(name, *dst == "", v, func() { *dst = v })
	}
	set("NotifyURL", &st.NotifyURL, s.cfg.NotifyURL)
	set("OpenAIKey", &st.OpenAIKey, s.cfg.OpenAIKey)
	set("OpenAIModel", &st.OpenAIModel, s.cfg.OpenAIModel)
	set("OpenAIBaseURL", &st.OpenAIBaseURL, s.cfg.OpenAIBaseURL)
	if s.cfg.NotifyFormat != "" && st.NotifyURL == s.cfg.NotifyURL && st.NotifyFormat != s.cfg.NotifyFormat {
		st.NotifyFormat = s.cfg.NotifyFormat
		changed = true
	}
	if changed {
		if err := s.store.SaveUserSettings(ctx, owner, st); err != nil {
			return err
		}
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx, owner); err != nil {
			return err
		}
	}
	return nil
}

// janitor runs the slow background work: the trash, and keeping yt-dlp new.
func (s *Server) janitor(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	lastHourly := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if time.Since(lastHourly) > time.Hour {
			lastHourly = time.Now()
			s.emptyTrash(ctx)
			s.dropStaleTemp()
			s.updateYTDLP(ctx)
		}
	}
}

// ---- Pocket Core ----

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh.
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

// eventTopics are what Sphere publishes on the Core bus. On purpose no
// titles, names or what was watched: only that a download finished, so a
// journal or automation never shows what the video is.
var eventTopics = []string{"sphere.download_finished"}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "sphere", Name: "Pocket Sphere",
		Description: "Your video library with a VR player: scenes, performers, studios, tags and markers, fingerprints that find what a file is, previews when you point at a scene, downloads from a link, and DeoVR/HereSphere on the headset.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "SPHERE",
		Publishes: eventTopics,
		Offers:    linkOffers,
		Uses:      []core.Use{core.InferenceUse()},
	}
	if s.cfg.BaseURL != "" {
		// ask Core for a relying-party client so "Sign in with Pocket Core" needs no configuration
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.oidcCallbackURL()}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// publish sends an event for a person (fire and forget, through the outbox).
func (s *Server) publish(ctx context.Context, uid int64, topic, subject string, payload map[string]any) {
	if !s.core.Configured() {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		payload["user_email"] = u.Email
	}
	if err := s.core.Publish(ctx, topic, subject, payload); err != nil {
		s.log.Debug("pocket core: publish failed", "topic", topic, "err", err)
	}
}
