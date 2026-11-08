package server

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// HLS for players that need it (the iPhone app: AVPlayer cannot play the live
// MP4 of stream.mp4, which has no byte ranges). A VOD playlist of fixed
// segments, each converted on its own when asked for, so seeking anywhere is
// just another segment.

const hlsSegment = 6.0

// GET /scenes/{id}/stream.m3u8?q=
func (s *Server) handleHLSPlaylist(w http.ResponseWriter, r *http.Request) {
	sc, err := s.store.GetScene(r.Context(), 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "scene")
		return
	}
	if sc.Duration <= 0 {
		writeErr(w, http.StatusUnprocessableEntity, "the length of this video is not known yet")
		return
	}
	q := hlsQuality(r.URL.Query().Get("q"))
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n", int(hlsSegment))
	n := int(math.Ceil(sc.Duration / hlsSegment))
	for i := 0; i < n; i++ {
		d := math.Min(hlsSegment, sc.Duration-float64(i)*hlsSegment)
		fmt.Fprintf(&b, "#EXTINF:%.3f,\nhls/%d.ts?q=%s\n", d, i, q)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

func hlsQuality(q string) string {
	if n, err := strconv.Atoi(q); err == nil && n >= 240 && n <= 4320 {
		return strconv.Itoa(n)
	}
	return "original"
}

// GET /scenes/{id}/hls/{n}.ts?q= : one segment, converted (cuts need key frames, so never copied).
func (s *Server) handleHLSSegment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, err := s.store.GetScene(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "scene")
		return
	}
	n, err := strconv.Atoi(strings.TrimSuffix(chi.URLParam(r, "seg"), ".ts"))
	start := float64(n) * hlsSegment
	if err != nil || n < 0 || (sc.Duration > 0 && start >= sc.Duration) {
		writeErr(w, http.StatusNotFound, "no such segment")
		return
	}
	if ok, _, terr := s.tools.Ready(); !ok {
		writeErr(w, http.StatusServiceUnavailable, terr.Error())
		return
	}
	height := 0
	if q := hlsQuality(r.URL.Query().Get("q")); q != "original" {
		height, _ = strconv.Atoi(q)
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-store")
	tries := []string{"cpu"}
	if s.cfg.HWAccel == "vaapi" {
		tries = []string{"gpu", "upload", "cpu"}
	}
	for i, hw := range tries {
		args := hlsArgs(s.convertArgs(sc, start, height, false, hw), start)
		started, err := s.pipeFFmpeg(ctx, w, args, "hls", hw != "cpu")
		if started || ctx.Err() != nil {
			return
		}
		if i == len(tries)-1 {
			s.log.Warn("hls: conversion failed", "scene", sc.ID, "segment", n, "err", err)
			writeErr(w, http.StatusInternalServerError, "the video could not be converted: "+err.Error())
			return
		}
	}
}

// hlsArgs turns stream.mp4's command into one MPEG-TS segment: its length,
// a key frame first, and the timestamps of its place in the video.
func hlsArgs(a []string, start float64) []string {
	for i, v := range a {
		if v == "-movflags" {
			a = a[:i]
			break
		}
	}
	for i := 0; i+1 < len(a); i++ {
		if a[i] == "-c:a" && a[i+1] == "copy" { // a fresh AAC stream per segment joins cleanly
			a = append(a[:i:i], append([]string{"-c:a", "aac", "-b:a", "192k", "-ac", "2"}, a[i+2:]...)...)
			break
		}
	}
	return append(a, "-t", fmt.Sprintf("%.3f", hlsSegment), "-force_key_frames", "expr:eq(n,0)",
		"-output_ts_offset", fmt.Sprintf("%.3f", start), "-muxdelay", "0", "-f", "mpegts", "pipe:1")
}
