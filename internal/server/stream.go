package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// How a scene reaches the player: the file as it is when the browser can
// play it (seeking with Range requests), otherwise a live MP4 made by ffmpeg
// — the streams copied into MP4 when only the container is the problem,
// converted to H.264 (on the GPU with HWACCEL=vaapi) when the codec is.
// A converted stream cannot seek; the player asks for a new one from the
// moment it wants (?start=).

const maxStreams = 4

var directContainers = map[string]bool{"mp4": true, "m4v": true, "mov": true, "webm": true}

// codecMime is what the player asks canPlayType about.
func codecMime(sc *store.Scene) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(sc.Path)), ".")
	box := "video/mp4"
	if ext == "webm" {
		box = "video/webm"
	}
	var codecs []string
	switch sc.VideoCodec {
	case "h264":
		codecs = append(codecs, "avc1.640033")
	case "hevc":
		codecs = append(codecs, "hvc1.1.6.L153.B0")
	case "vp9":
		codecs = append(codecs, "vp09.00.50.08")
		if box == "video/webm" {
			codecs[len(codecs)-1] = "vp9"
		}
	case "vp8":
		codecs = append(codecs, "vp8")
	case "av1":
		codecs = append(codecs, "av01.0.12M.08")
	default:
		return ""
	}
	switch sc.AudioCodec {
	case "", "none":
	case "aac":
		codecs = append(codecs, "mp4a.40.2")
	case "mp3":
		codecs = append(codecs, "mp4a.6B")
	case "opus":
		codecs = append(codecs, "opus")
	case "vorbis":
		codecs = append(codecs, "vorbis")
	case "flac":
		codecs = append(codecs, "flac")
	default:
		return ""
	}
	return fmt.Sprintf(`%s; codecs="%s"`, box, strings.Join(codecs, ","))
}

// playback tells the player its choices for a scene.
func (s *Server) playback(sc *store.Scene, r *http.Request) map[string]any {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(sc.Path)), ".")
	mimeType := codecMime(sc)
	direct := directContainers[ext] && mimeType != ""
	copyOK := (sc.VideoCodec == "h264" || sc.VideoCodec == "hevc") && sc.Duration > 0
	base := fmt.Sprintf("/api/v1/scenes/%d", sc.ID)
	out := map[string]any{
		"direct":     base + "/stream",
		"direct_ok":  direct,
		"mime":       mimeType,
		"converted":  base + "/stream.mp4",
		"remux_ok":   copyOK,
		"duration":   sc.Duration,
		"markers":    base + "/markers",
		"sprite":     "",
		"sprite_map": "",
	}
	if sc.HasSprite {
		out["sprite"] = fmt.Sprintf("%s/sprite.jpg?v=%d", base, sc.GenVersion)
		out["sprite_map"] = fmt.Sprintf("%s/sprite.json?v=%d", base, sc.GenVersion)
	}
	if !fileExists(s.scenePath(sc)) {
		out["missing"] = true
	}
	return out
}

// handleStream sends the file itself (Range requests, for seeking).
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	sc, err := s.store.GetScene(r.Context(), 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "scene")
		return
	}
	s.serveVideo(w, r, sc, queryBool(r, "download"))
}

func (s *Server) serveVideo(w http.ResponseWriter, r *http.Request, sc *store.Scene, attachment bool) {
	p := s.scenePath(sc)
	f, err := os.Open(p)
	if err != nil {
		writeErr(w, http.StatusNotFound, "the file is not there")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(p)))
	switch strings.ToLower(filepath.Ext(p)) {
	case ".mkv":
		ct = "video/x-matroska"
	case ".m4v", ".mp4":
		ct = "video/mp4"
	case ".webm":
		ct = "video/webm"
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if attachment {
		w.Header().Set("Content-Disposition", contentDisposition(filepath.Base(p)))
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, urlPathEscape(name))
}

func urlPathEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// handleConverted streams a live MP4 from ?start= seconds.
func (s *Server) handleConverted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, err := s.store.GetScene(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "scene")
		return
	}
	if ok, _, terr := s.tools.Ready(); !ok {
		writeErr(w, http.StatusServiceUnavailable, terr.Error())
		return
	}
	if s.streams.Load() >= maxStreams {
		writeErr(w, http.StatusServiceUnavailable, "too many videos are being converted right now")
		return
	}
	s.streams.Add(1)
	defer s.streams.Add(-1)
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	if start < 0 || (sc.Duration > 0 && start >= sc.Duration) {
		start = 0
	}
	q := r.URL.Query().Get("q")
	height := 0
	if n, err := strconv.Atoi(q); err == nil && n >= 240 && n <= 4320 {
		height = n
	}
	copyVideo := q == "original" && (sc.VideoCodec == "h264" || sc.VideoCodec == "hevc")

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "none")
	mode := "convert"
	if copyVideo {
		mode = "copy"
	}
	// the GPU first when configured (decoding there too, then decoding on the
	// CPU for codecs the GPU cannot read); all on the CPU when neither starts
	tries := []string{"cpu"}
	if !copyVideo && s.cfg.HWAccel == "vaapi" {
		tries = []string{"gpu", "upload", "cpu"}
	}
	for i, hw := range tries {
		args := s.convertArgs(sc, start, height, copyVideo, hw)
		started, err := s.pipeFFmpeg(ctx, w, args, mode, hw != "cpu")
		if started || ctx.Err() != nil {
			return
		}
		if i == len(tries)-1 {
			s.log.Warn("stream: conversion failed", "scene", sc.ID, "err", err)
			writeErr(w, http.StatusInternalServerError, "the video could not be converted: "+err.Error())
			return
		}
		s.log.Info("stream: conversion did not start, trying the next way", "scene", sc.ID, "way", hw, "err", err)
	}
}

func even(n int) int { return n / 2 * 2 }

// convertArgs builds the ffmpeg command; hw is gpu (decode and encode on the
// GPU), upload (decode on the CPU, encode on the GPU) or cpu.
func (s *Server) convertArgs(sc *store.Scene, start float64, height int, copyVideo bool, way string) []string {
	in := s.scenePath(sc)
	a := []string{"-hide_banner", "-v", "error", "-nostdin"}
	switch way {
	case "gpu":
		a = append(a, "-hwaccel", "vaapi", "-hwaccel_device", s.cfg.VAAPIDevice, "-hwaccel_output_format", "vaapi")
	case "upload":
		a = append(a, "-vaapi_device", s.cfg.VAAPIDevice)
	}
	if start > 0 {
		a = append(a, "-ss", fmt.Sprintf("%.3f", start))
	}
	a = append(a, "-i", in, "-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn", "-map_metadata", "-1")
	// target size: the height asked for, never larger than the file
	tw, th := sc.Width, sc.Height
	if height > 0 && sc.Height > height && sc.Width > 0 {
		th = height
		tw = even(int(float64(sc.Width) * float64(height) / float64(sc.Height)))
	}
	switch {
	case copyVideo:
		a = append(a, "-c:v", "copy")
		if sc.VideoCodec == "hevc" {
			a = append(a, "-tag:v", "hvc1")
		}
	case way == "gpu" || way == "upload":
		vf := "scale_vaapi=format=nv12"
		if tw > 0 && th > 0 && th != sc.Height {
			vf = fmt.Sprintf("scale_vaapi=w=%d:h=%d:format=nv12", tw, th)
		}
		if way == "upload" {
			vf = "format=nv12,hwupload," + vf
		}
		a = append(a, "-vf", vf, "-c:v", "h264_vaapi", "-qp", "23", "-g", "120")
	default:
		vf := "format=yuv420p"
		if tw > 0 && th > 0 && th != sc.Height {
			vf = fmt.Sprintf("scale=%d:%d,format=yuv420p", tw, th)
		}
		preset := "veryfast"
		if th >= 1440 || (th == 0 && sc.Height >= 1440) {
			preset = "ultrafast"
		}
		a = append(a, "-vf", vf, "-c:v", "libx264", "-preset", preset, "-crf", "22", "-g", "120", "-profile:v", "high")
	}
	if sc.AudioCodec == "aac" || sc.AudioCodec == "mp3" {
		a = append(a, "-c:a", "copy")
	} else {
		a = append(a, "-c:a", "aac", "-b:a", "192k", "-ac", "2")
	}
	return append(a, "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-frag_duration", "2000000", "-f", "mp4", "pipe:1")
}

// pipeFFmpeg runs ffmpeg into the response. started = bytes reached the client.
func (s *Server) pipeFFmpeg(ctx context.Context, w http.ResponseWriter, args []string, mode string, hw bool) (bool, error) {
	cmd := exec.CommandContext(ctx, s.cfg.FFmpeg, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false, err
	}
	var errb strings.Builder
	var errMu sync.Mutex
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return false, err
	}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			errMu.Lock()
			if errb.Len() < 4000 {
				errb.WriteString(sc.Text() + "\n")
			}
			errMu.Unlock()
		}
	}()
	br := bufio.NewReaderSize(out, 256<<10)
	first, perr := br.Peek(1)
	if len(first) == 0 {
		_ = cmd.Wait()
		errMu.Lock()
		msg := strings.TrimSpace(errb.String())
		errMu.Unlock()
		if msg == "" && perr != nil {
			msg = perr.Error()
		}
		return false, errors.New(lastLine(msg))
	}
	label := mode
	if hw {
		label += "+gpu"
	}
	w.Header().Set("X-Stream", label)
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 128<<10)
	for {
		n, rerr := br.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				break // the player went away (a seek, a new scene)
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
	return true, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// ---------- made files ----------

func (s *Server) serveGenerated(w http.ResponseWriter, r *http.Request, p, ct string) {
	f, err := os.Open(p)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not made yet")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusNotFound, "not made yet")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	s.serveGenerated(w, r, s.genPath("previews", idParam(r, "id"), ".mp4"), "video/mp4")
}

func (s *Server) handleSprite(w http.ResponseWriter, r *http.Request) {
	s.serveGenerated(w, r, s.genPath("sprites", idParam(r, "id"), ".jpg"), "image/jpeg")
}

func (s *Server) handleSpriteMap(w http.ResponseWriter, r *http.Request) {
	s.serveGenerated(w, r, s.genPath("sprites", idParam(r, "id"), ".json"), "application/json")
}

// makeOnce makes a file once even when many ask for it at the same time.
func (s *Server) makeOnce(key, dst string, make func() error) error {
	if fileExists(dst) {
		return nil
	}
	s.makeMu.Lock()
	if wg := s.making[key]; wg != nil {
		s.makeMu.Unlock()
		wg.Wait()
		if fileExists(dst) {
			return nil
		}
		return errors.New("could not be made")
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	s.making[key] = wg
	s.makeMu.Unlock()
	defer func() {
		s.makeMu.Lock()
		delete(s.making, key)
		s.makeMu.Unlock()
		wg.Done()
	}()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return make()
}

// markerFiles makes (once) and serves a marker's clip or picture.
func (s *Server) handleMarkerMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := s.store.GetMarker(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "marker")
		return
	}
	sc, err := s.store.GetScene(ctx, 0, m.SceneID)
	if err != nil {
		s.fail(w, err, "scene")
		return
	}
	kind := "clip"
	if strings.HasSuffix(r.URL.Path, ".jpg") {
		kind = "picture"
	}
	stamp := fmt.Sprintf("%d-%d-%d-%s", sc.ID, m.ID, int(m.Seconds*10), sc.Projection+sc.Stereo)
	ext := ".mp4"
	if kind == "picture" {
		ext = ".jpg"
	}
	dst := filepath.Join(s.cfg.GeneratedDir, "markers", stamp+ext)
	err = s.makeOnce(dst, dst, func() error {
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		defer cancel()
		if kind == "picture" {
			b, err := s.tools.Screenshot(mctx, s.scenePath(sc), m.Seconds+1, shapeOf(sc), 640, 360)
			if err != nil {
				return err
			}
			return writeFileAtomic(dst, b)
		}
		length := 10.0
		if m.EndSeconds != nil && *m.EndSeconds-m.Seconds > 2 {
			length = min(*m.EndSeconds-m.Seconds, 20)
		}
		return s.tools.Clip(mctx, s.scenePath(sc), dst, m.Seconds, length, shapeOf(sc), 640, 360, false)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ct := "video/mp4"
	if kind == "picture" {
		ct = "image/jpeg"
	}
	s.serveGenerated(w, r, dst, ct)
}

// handleFrame is one picture of a scene at ?at= seconds (shown flat), for
// picking a cover or a marker.
func (s *Server) handleFrame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, err := s.store.GetScene(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "scene")
		return
	}
	at, _ := strconv.ParseFloat(r.URL.Query().Get("at"), 64)
	b, err := s.tools.Screenshot(ctx, s.scenePath(sc), at, shapeOf(sc), 640, 360)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, strings.NewReader(string(b)))
}
