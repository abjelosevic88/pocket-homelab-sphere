// Package ytdlp runs yt-dlp: it keeps its own copy up to date (sites change
// often), reads what a link holds and downloads videos with progress.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const releaseURL = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp"

// Runner finds and runs yt-dlp.
type Runner struct {
	fixed   string // YTDLP: a yt-dlp chosen by the admin (never updated by us)
	own     string // our own copy: DATA_DIR/bin/yt-dlp
	bundled string // the copy in the image, used to start our own
	ua      string

	mu       sync.Mutex
	version  string
	lastErr  string
	updated  time.Time
	updating bool
}

// New returns a runner. fixed = "" keeps an own copy in dataDir/bin.
func New(fixed, dataDir, userAgent string) *Runner {
	return &Runner{fixed: fixed, own: filepath.Join(dataDir, "bin", "yt-dlp"), bundled: "/usr/local/share/pocket-sphere/yt-dlp", ua: userAgent}
}

// Path returns the yt-dlp to run, fetching our own copy when there is none.
func (r *Runner) Path(ctx context.Context) (string, error) {
	if r.fixed != "" {
		return r.fixed, nil
	}
	if fi, err := os.Stat(r.own); err == nil && fi.Size() > 0 {
		return r.own, nil
	}
	if err := os.MkdirAll(filepath.Dir(r.own), 0o755); err != nil {
		return "", err
	}
	if b, err := os.ReadFile(r.bundled); err == nil && len(b) > 0 {
		if err := os.WriteFile(r.own, b, 0o755); err != nil {
			return "", err
		}
		return r.own, nil
	}
	if err := r.fetch(ctx); err != nil {
		if p, lerr := exec.LookPath("yt-dlp"); lerr == nil {
			return p, nil
		}
		return "", fmt.Errorf("yt-dlp could not be fetched: %w", err)
	}
	return r.own, nil
}

func (r *Runner) fetch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	req.Header.Set("User-Agent", r.ua)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub answered HTTP %d", resp.StatusCode)
	}
	tmp := r.own + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 200<<20)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, r.own)
}

// Status is what Settings shows about yt-dlp.
type Status struct {
	Version   string `json:"version"`
	Path      string `json:"path"`
	Own       bool   `json:"own"` // our copy, updated daily
	UpdatedAt string `json:"updated_at"`
	Error     string `json:"error"`
	Updating  bool   `json:"updating"`
}

// Status reports the version (asking yt-dlp once).
func (r *Runner) Status(ctx context.Context) Status {
	r.mu.Lock()
	v, e, up, busy := r.version, r.lastErr, r.updated, r.updating
	r.mu.Unlock()
	p, err := r.Path(ctx)
	if err != nil {
		return Status{Error: err.Error(), Own: r.fixed == ""}
	}
	if v == "" {
		v = r.readVersion(ctx, p)
	}
	st := Status{Version: v, Path: p, Own: r.fixed == "", Error: e, Updating: busy}
	if !up.IsZero() {
		st.UpdatedAt = up.UTC().Format(time.RFC3339)
	}
	return st
}

func (r *Runner) readVersion(ctx context.Context, p string) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p, "--version").Output()
	if err != nil {
		r.mu.Lock()
		r.lastErr = "yt-dlp does not start: " + err.Error()
		r.mu.Unlock()
		return ""
	}
	v := strings.TrimSpace(string(out))
	r.mu.Lock()
	r.version = v
	r.mu.Unlock()
	return v
}

// Update brings our own copy to the newest release (yt-dlp -U).
func (r *Runner) Update(ctx context.Context) (string, error) {
	if r.fixed != "" {
		return "", errors.New("this yt-dlp is managed outside the app (YTDLP)")
	}
	r.mu.Lock()
	if r.updating {
		r.mu.Unlock()
		return "", errors.New("already updating")
	}
	r.updating = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.updating = false
		r.mu.Unlock()
	}()
	p, err := r.Path(ctx)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, p, "-U").CombinedOutput()
	r.mu.Lock()
	r.updated = time.Now()
	r.version = ""
	r.lastErr = ""
	if err != nil {
		r.lastErr = "update failed: " + lastLine(string(out))
	}
	r.mu.Unlock()
	if err != nil {
		// a broken copy: fetch a fresh one
		if ferr := r.fetch(ctx); ferr != nil {
			return "", fmt.Errorf("%s", lastLine(string(out)))
		}
	}
	return r.readVersion(ctx, p), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// ---------- reading a link ----------

// Entry is one video of a playlist or channel.
type Entry struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// Info is what a link holds.
type Info struct {
	Type         string  `json:"_type"` // video | playlist | url
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	URL          string  `json:"webpage_url"`
	Thumbnail    string  `json:"thumbnail"`
	Uploader     string  `json:"uploader"`
	Channel      string  `json:"channel"`
	Duration     float64 `json:"duration"`
	Extractor    string  `json:"extractor"`
	ExtractorKey string  `json:"extractor_key"`
	Entries      []Entry `json:"entries"`
}

// Generic reports whether yt-dlp only found videos embedded in a page
// (not a site it knows): such "playlists" are often previews or ads.
func (i *Info) Generic() bool {
	return strings.EqualFold(i.ExtractorKey, "Generic") || strings.HasPrefix(strings.ToLower(i.Extractor), "generic")
}

// IsList reports whether the link is a playlist or channel.
func (i *Info) IsList() bool { return i.Type == "playlist" || len(i.Entries) > 0 }

// Options are the extra switches of one run.
type Options struct {
	Cookies  string // a cookies.txt
	Password string
	Limit    int // playlist items (0 = all)
}

func (o Options) args() []string {
	var a []string
	if o.Cookies != "" {
		a = append(a, "--cookies", o.Cookies)
	}
	if o.Password != "" {
		a = append(a, "--video-password", o.Password)
	}
	return a
}

// Inspect reads a link without downloading (playlists flat).
func (r *Runner) Inspect(ctx context.Context, url string, o Options) (*Info, error) {
	p, err := r.Path(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	args := append([]string{"-J", "--flat-playlist", "--no-warnings", "--no-colors", "--socket-timeout", "30"}, o.args()...)
	if o.Limit > 0 {
		args = append(args, "--playlist-end", fmt.Sprint(o.Limit))
	}
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, p, append(args, "--", url)...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("the site took too long to answer")
		}
		return nil, cleanErr(errb.String(), err)
	}
	var info Info
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("yt-dlp gave an answer that could not be read: %w", err)
	}
	if info.Type == "" {
		info.Type = "video"
	}
	if info.URL == "" {
		info.URL = url
	}
	for i := range info.Entries {
		if info.Entries[i].URL == "" {
			info.Entries[i].URL = info.Entries[i].ID
		}
	}
	return &info, nil
}

func cleanErr(stderr string, err error) error {
	for _, l := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(l, "ERROR:") {
			msg := strings.TrimSpace(strings.TrimPrefix(l, "ERROR:"))
			// "[generic] Unable to download webpage" etc. — drop the extractor tag
			if strings.HasPrefix(msg, "[") {
				if i := strings.Index(msg, "]"); i > 0 {
					msg = strings.TrimSpace(msg[i+1:])
				}
			}
			switch {
			case strings.Contains(msg, "Unsupported URL"):
				return errors.New("this site is not supported, or the link is not a video page")
			case strings.Contains(msg, "HTTP Error 404"):
				return errors.New("the video is gone (HTTP 404)")
			case strings.Contains(strings.ToLower(msg), "sign in") || strings.Contains(strings.ToLower(msg), "login") || strings.Contains(msg, "cookies"):
				return fmt.Errorf("the site wants a sign-in for this video — add your cookies under Settings → Downloads (%s)", oneLine(msg, 160))
			}
			return errors.New(oneLine(msg, 300))
		}
	}
	if l := lastLine(stderr); l != "" {
		return errors.New(oneLine(l, 300))
	}
	return err
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}

// ---------- downloading ----------

// Job is one video to fetch.
type Job struct {
	URL       string
	Dir       string // where the finished file goes
	TempDir   string // where pieces wait meanwhile
	Template  string // output name template inside Dir
	Quality   string // best | 2160 | 1440 | 1080 | 720 | 480
	Format    string // auto | mp4 | mkv | webm
	ClipStart string
	ClipEnd   string
	Referer   string
	Options
}

// Progress is one report while downloading.
type Progress struct {
	Downloaded int64   `json:"downloaded_bytes"`
	Total      int64   `json:"total_bytes"`
	Estimate   float64 `json:"total_bytes_estimate"`
	Speed      float64 `json:"speed"`
	ETA        float64 `json:"eta"`
	Status     string  `json:"status"`
}

// Meta is what yt-dlp knew about the video right before downloading.
type Meta struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
}

func (j Job) args() []string {
	a := []string{"--newline", "--no-colors", "--progress", "--no-playlist", "--no-mtime", "--socket-timeout", "30", "--retries", "5",
		"--fragment-retries", "10", "-N", "4", "--windows-filenames", "--trim-filenames", "200",
		"--write-thumbnail", "--convert-thumbnails", "jpg", "--write-info-json", "--no-write-playlist-metafiles",
		"-P", "home:" + j.Dir, "-P", "temp:" + j.TempDir, "-o", j.Template,
		"--progress-template", "download:SPHPROG %(progress.{downloaded_bytes,total_bytes,total_bytes_estimate,speed,eta,status})j",
		"--print", "before_dl:SPHINFO %(.{id,title,thumbnail})j",
		"--print", "after_move:SPHFILE %(filepath)s",
	}
	sort := "res,ext:mp4:m4a"
	if q := j.Quality; q != "" && q != "best" {
		sort = "res:" + q + ",ext:mp4:m4a"
	}
	switch j.Format {
	case "mp4":
		a = append(a, "--merge-output-format", "mp4", "--remux-video", "mp4")
	case "mkv":
		a = append(a, "--merge-output-format", "mkv")
	case "webm":
		sort = strings.Replace(sort, "ext:mp4:m4a", "ext:webm:webm", 1)
		a = append(a, "--merge-output-format", "webm")
	default:
		a = append(a, "--merge-output-format", "mp4/mkv")
	}
	a = append(a, "-S", sort)
	if j.Referer != "" {
		a = append(a, "--referer", j.Referer)
	}
	if j.ClipStart != "" || j.ClipEnd != "" {
		start, end := j.ClipStart, j.ClipEnd
		if start == "" {
			start = "0"
		}
		if end == "" {
			end = "inf"
		}
		a = append(a, "--download-sections", "*"+start+"-"+end, "--force-keyframes-at-cuts")
	}
	return append(a, j.Options.args()...)
}

// Download fetches one video; progress and meta are called as yt-dlp reports.
// It returns the finished file's path.
func (r *Runner) Download(ctx context.Context, j Job, onMeta func(Meta), onProgress func(Progress)) (string, error) {
	p, err := r.Path(ctx)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(j.Dir, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(j.TempDir, 0o755); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, p, append(j.args(), "--", j.URL)...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 15 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var errb tailBuffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var file string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "SPHPROG "):
			var pr Progress
			if json.Unmarshal([]byte(line[8:]), &pr) == nil && onProgress != nil {
				onProgress(pr)
			}
		case strings.HasPrefix(line, "SPHINFO "):
			var m Meta
			if json.Unmarshal([]byte(line[8:]), &m) == nil && onMeta != nil {
				onMeta(m)
			}
		case strings.HasPrefix(line, "SPHFILE "):
			file = strings.TrimSpace(line[8:])
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", cleanErr(errb.String(), err)
	}
	if file == "" {
		return "", errors.New("yt-dlp finished but did not say where the file is")
	}
	return file, nil
}

// tailBuffer keeps the last 64 KiB written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 64<<10 {
		t.buf = t.buf[len(t.buf)-64<<10:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
