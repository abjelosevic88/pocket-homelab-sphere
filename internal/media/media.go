// Package media reads video files (ffprobe), makes pictures and short clips
// from them (ffmpeg), fingerprints them (oshash, phash) and works out how a
// VR video is shaped.
package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // covers and sidecar pictures
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/corona10/goimagehash"
	_ "golang.org/x/image/bmp" // ffmpeg hands frames over as BMP for the phash
)

// Tools runs ffmpeg and ffprobe.
type Tools struct {
	FFmpeg  string
	FFprobe string

	once  sync.Once
	ok    bool
	vers  string
	v360  bool
	probe error
}

// New returns the tools at these paths.
func New(ffmpeg, ffprobe string) *Tools { return &Tools{FFmpeg: ffmpeg, FFprobe: ffprobe} }

func (t *Tools) check() {
	t.once.Do(func() {
		out, err := exec.Command(t.FFmpeg, "-hide_banner", "-version").Output()
		if err != nil {
			t.probe = fmt.Errorf("ffmpeg is not installed (%v)", err)
			return
		}
		if _, err := exec.LookPath(t.FFprobe); err != nil {
			t.probe = fmt.Errorf("ffprobe is not installed (%v)", err)
			return
		}
		t.ok = true
		if f := strings.Fields(string(out)); len(f) > 2 {
			t.vers = f[2]
		}
		filters, _ := exec.Command(t.FFmpeg, "-hide_banner", "-filters").Output()
		t.v360 = bytes.Contains(filters, []byte(" v360 "))
	})
}

// Ready reports whether ffmpeg and ffprobe are there.
func (t *Tools) Ready() (bool, string, error) {
	t.check()
	return t.ok, t.vers, t.probe
}

// ErrNoTools is returned when ffmpeg/ffprobe are missing.
var ErrNoTools = errors.New("ffmpeg is not installed")

// ---------- probe ----------

// Probe is what ffprobe found in a file.
type Probe struct {
	Duration   float64
	Width      int
	Height     int
	FPS        float64
	VideoCodec string
	AudioCodec string
	Container  string
	Bitrate    int64
	// spherical metadata (YouTube/Google style)
	Spherical string // '' | equirectangular | fisheye …
	Stereo3D  string // '' | side by side | top and bottom
}

type probeJSON struct {
	Format struct {
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
		RFrameRate   string `json:"r_frame_rate"`
		Duration     string `json:"duration"`
		Disposition  struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		Tags         map[string]string `json:"tags"`
		SideDataList []struct {
			SideDataType string `json:"side_data_type"`
			Projection   string `json:"projection"`
			Type         string `json:"type"`
			Rotation     int    `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
}

func rate(s string) float64 {
	a, b, ok := strings.Cut(s, "/")
	x, _ := strconv.ParseFloat(a, 64)
	if !ok {
		return x
	}
	y, _ := strconv.ParseFloat(b, 64)
	if y == 0 {
		return 0
	}
	return math.Round(x/y*100) / 100
}

// ProbeFile reads a video's technical facts.
func (t *Tools) ProbeFile(ctx context.Context, path string) (*Probe, error) {
	if ok, _, err := t.Ready(); !ok {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, t.FFprobe, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("not a video ffmpeg can read: %s", strings.TrimSpace(lastLine(string(ee.Stderr))))
		}
		return nil, err
	}
	var j probeJSON
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, err
	}
	p := &Probe{Container: j.Format.FormatName}
	p.Duration, _ = strconv.ParseFloat(j.Format.Duration, 64)
	p.Bitrate, _ = strconv.ParseInt(j.Format.BitRate, 10, 64)
	for _, s := range j.Streams {
		switch {
		case s.CodecType == "video" && s.Disposition.AttachedPic == 0 && p.VideoCodec == "":
			p.VideoCodec, p.Width, p.Height = s.CodecName, s.Width, s.Height
			p.FPS = rate(s.AvgFrameRate)
			if p.FPS == 0 {
				p.FPS = rate(s.RFrameRate)
			}
			if p.Duration == 0 {
				p.Duration, _ = strconv.ParseFloat(s.Duration, 64)
			}
			rot := 0
			if r, err := strconv.Atoi(s.Tags["rotate"]); err == nil {
				rot = r
			}
			for _, sd := range s.SideDataList {
				switch {
				case strings.Contains(sd.SideDataType, "Spherical"):
					p.Spherical = strings.ToLower(sd.Projection)
				case strings.Contains(sd.SideDataType, "Stereo 3D"):
					p.Stereo3D = strings.ToLower(sd.Type)
				case strings.Contains(sd.SideDataType, "Display Matrix") && sd.Rotation != 0:
					rot = sd.Rotation
				}
			}
			if rot == 90 || rot == -90 || rot == 270 || rot == -270 {
				p.Width, p.Height = p.Height, p.Width
			}
		case s.CodecType == "audio" && p.AudioCodec == "":
			p.AudioCodec = s.CodecName
		}
	}
	if p.VideoCodec == "" {
		return nil, errors.New("the file has no video")
	}
	return p, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// ---------- fingerprints ----------

// OSHash is the OpenSubtitles hash Stash and StashDB use: the size plus the
// sum of the first and last 64 KiB as little-endian 64-bit words.
func OSHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := fi.Size()
	const chunk = 64 * 1024
	if size < 8 {
		return "", errors.New("file too small")
	}
	sum := uint64(size)
	add := func(off int64, n int64) error {
		buf := make([]byte, n)
		if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
			return err
		}
		for i := 0; i+8 <= len(buf); i += 8 {
			sum += binary.LittleEndian.Uint64(buf[i:])
		}
		return nil
	}
	head := min(int64(chunk), size)
	if err := add(0, head); err != nil {
		return "", err
	}
	tailStart := max(size-chunk, 0)
	if err := add(tailStart, size-tailStart); err != nil {
		return "", err
	}
	return fmt.Sprintf("%016x", sum), nil
}

// PHash is Stash's perceptual hash: 25 frames from 5% to 95% of the video,
// 160 pixels wide, laid out 5×5, hashed with goimagehash's PerceptionHash.
// StashDB matches on it, so the steps follow Stash's.
func (t *Tools) PHash(ctx context.Context, path string, duration float64) (string, error) {
	if ok, _, err := t.Ready(); !ok {
		return "", err
	}
	if duration <= 0 {
		return "", errors.New("unknown length")
	}
	const cols, rows, width = 5, 5, 160
	offset := 0.05 * duration
	step := 0.9 * duration / (cols * rows)
	var frames []image.Image
	for i := 0; i < cols*rows; i++ {
		b, err := t.frame(ctx, path, offset+float64(i)*step, "scale="+strconv.Itoa(width)+":-2", "bmp")
		if err != nil {
			return "", err
		}
		img, _, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			return "", fmt.Errorf("frame %d: %w", i, err)
		}
		frames = append(frames, img)
	}
	fw, fh := frames[0].Bounds().Dx(), frames[0].Bounds().Dy()
	sheet := image.NewRGBA(image.Rect(0, 0, fw*cols, fh*rows))
	for i, f := range frames {
		x, y := (i%cols)*fw, (i/cols)*fh
		draw.Draw(sheet, image.Rect(x, y, x+fw, y+fh), f, f.Bounds().Min, draw.Src)
	}
	h, err := goimagehash.PerceptionHash(sheet)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%016x", h.GetHash()), nil
}

// HashDistance is the number of differing bits of two hex phashes (64 = unrelated).
func HashDistance(a, b string) int {
	x, err1 := strconv.ParseUint(a, 16, 64)
	y, err2 := strconv.ParseUint(b, 16, 64)
	if err1 != nil || err2 != nil {
		return 64
	}
	d := 0
	for v := x ^ y; v != 0; v &= v - 1 {
		d++
	}
	return d
}

// ---------- pictures and clips ----------

// frame grabs one frame at a time (seconds) through a filter chain.
func (t *Tools) frame(ctx context.Context, path string, at float64, filter, codec string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	args := []string{"-hide_banner", "-v", "error", "-ss", fmt.Sprintf("%.3f", max(at, 0)), "-i", path, "-frames:v", "1", "-an", "-sn"}
	if filter != "" {
		args = append(args, "-vf", filter)
	}
	switch codec {
	case "bmp":
		args = append(args, "-c:v", "bmp", "-f", "image2pipe", "pipe:1")
	default:
		args = append(args, "-c:v", "mjpeg", "-q:v", "3", "-f", "image2pipe", "pipe:1")
	}
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, t.FFmpeg, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		if at > 1 && out.Len() == 0 && err == nil {
			// past the last key frame: try a little earlier
			return t.frame(ctx, path, at-2, filter, codec)
		}
		msg := strings.TrimSpace(lastLine(errb.String()))
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg: %s", msg)
	}
	return out.Bytes(), nil
}

// Shape is how a video frame is laid out.
type Shape struct {
	Projection string // flat | equirect180 | equirect360 | fisheye
	Stereo     string // mono | sbs | tb
	FOV        int
}

// IsVR reports whether the frame needs reprojecting.
func (s Shape) IsVR() bool { return s.Projection != "" && s.Projection != "flat" }

// FlatView is the filter that turns a frame into a normal-looking picture of
// w×h: VR frames are reprojected (one eye, looking a little down), flat ones
// scaled.
func (t *Tools) FlatView(sh Shape, w, h int) string {
	if !sh.IsVR() {
		return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", w, h, w, h)
	}
	in := "hequirect"
	extra := ""
	switch sh.Projection {
	case "equirect360":
		in = "e"
	case "fisheye":
		fov := sh.FOV
		if fov == 0 {
			fov = 190
		}
		in = "fisheye"
		extra = fmt.Sprintf(":ih_fov=%d:iv_fov=%d", fov, fov)
	}
	stereo := "2d"
	switch sh.Stereo {
	case "sbs":
		stereo = "sbs"
	case "tb":
		stereo = "tb"
	}
	t.check()
	if !t.v360 {
		// an old ffmpeg: the middle of the left (or top) eye
		crop := "crop=iw/2:ih:0:0,"
		if stereo == "tb" {
			crop = "crop=iw:ih/2:0:0,"
		} else if stereo == "2d" {
			crop = ""
		}
		return crop + fmt.Sprintf("crop=iw*0.6:iw*0.6*%d/%d:iw*0.2:ih*0.5-iw*0.3*%d/%d,scale=%d:%d", h, w, h, w, w, h)
	}
	return fmt.Sprintf("v360=%s:flat:in_stereo=%s:out_stereo=2d%s:d_fov=105:pitch=-18:w=%d:h=%d", in, stereo, extra, w, h)
}

// Screenshot is one JPEG frame at a time, shown flat, w×h.
func (t *Tools) Screenshot(ctx context.Context, path string, at float64, sh Shape, w, h int) ([]byte, error) {
	if ok, _, err := t.Ready(); !ok {
		return nil, err
	}
	return t.frame(ctx, path, at, t.FlatView(sh, w, h), "jpg")
}

// SpriteInfo describes a scrubber sheet.
type SpriteInfo struct {
	Cols     int     `json:"cols"`
	Rows     int     `json:"rows"`
	Width    int     `json:"width"` // of one picture
	Height   int     `json:"height"`
	Interval float64 `json:"interval"` // seconds between pictures
	Count    int     `json:"count"`
}

// Sprite makes the scrubber sheet: count pictures (cols wide) evenly over
// the video, each w×h, as one JPEG.
func (t *Tools) Sprite(ctx context.Context, path string, duration float64, sh Shape, count, cols, w, h int) ([]byte, *SpriteInfo, error) {
	if ok, _, err := t.Ready(); !ok {
		return nil, nil, err
	}
	if duration <= 0 {
		return nil, nil, errors.New("unknown length")
	}
	rows := (count + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, w*cols, h*rows))
	interval := duration / float64(count)
	filter := t.FlatView(sh, w, h)
	type res struct {
		i   int
		img image.Image
		err error
	}
	jobs := make(chan int)
	results := make(chan res)
	var wg sync.WaitGroup
	for k := 0; k < 3; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				b, err := t.frame(ctx, path, interval*(float64(i)+0.5), filter, "jpg")
				var img image.Image
				if err == nil {
					img, err = jpeg.Decode(bytes.NewReader(b))
				}
				results <- res{i, img, err}
			}
		}()
	}
	go func() {
		for i := 0; i < count; i++ {
			select {
			case jobs <- i:
			case <-ctx.Done():
			}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	var firstErr error
	got := 0
	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		got++
		x, y := (r.i%cols)*w, (r.i/cols)*h
		draw.Draw(sheet, image.Rect(x, y, x+w, y+h), r.img, r.img.Bounds().Min, draw.Src)
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if got < count/2 {
		if firstErr == nil {
			firstErr = errors.New("no frames")
		}
		return nil, nil, firstErr
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sheet, &jpeg.Options{Quality: 78}); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), &SpriteInfo{Cols: cols, Rows: rows, Width: w, Height: h, Interval: interval, Count: count}, nil
}

// Preview makes the short silent hover clip: segments pieces of segLen
// seconds spread over the video, w×h, H.264 MP4, written to dst.
func (t *Tools) Preview(ctx context.Context, path, dst string, duration float64, sh Shape, segments int, segLen float64, w, h int) error {
	if ok, _, err := t.Ready(); !ok {
		return err
	}
	if duration <= 0 {
		return errors.New("unknown length")
	}
	if duration < float64(segments)*segLen*2 {
		segments = max(int(duration/(segLen*2)), 1)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), ".preview-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	filter := t.FlatView(sh, w, h) + ",fps=30,format=yuv420p"
	var list strings.Builder
	for i := 0; i < segments; i++ {
		at := duration*0.05 + float64(i)*(duration*0.9-segLen)/float64(max(segments-1, 1))
		part := filepath.Join(tmp, fmt.Sprintf("%02d.mp4", i))
		if err := t.run(ctx, 3*time.Minute, "-ss", fmt.Sprintf("%.3f", at), "-t", fmt.Sprintf("%.3f", segLen), "-i", path,
			"-vf", filter, "-an", "-sn", "-c:v", "libx264", "-preset", "veryfast", "-crf", "27", "-profile:v", "high",
			"-video_track_timescale", "15360", "-y", part); err != nil {
			return err
		}
		fmt.Fprintf(&list, "file '%s'\n", part)
	}
	lst := filepath.Join(tmp, "list.txt")
	if err := os.WriteFile(lst, []byte(list.String()), 0o644); err != nil {
		return err
	}
	out := dst + ".tmp.mp4"
	if err := t.run(ctx, 2*time.Minute, "-f", "concat", "-safe", "0", "-i", lst, "-c", "copy", "-movflags", "+faststart", "-y", out); err != nil {
		return err
	}
	return os.Rename(out, dst)
}

// Clip cuts a short silent clip of a moment (a marker), w×h.
func (t *Tools) Clip(ctx context.Context, path, dst string, at, length float64, sh Shape, w, h int, audio bool) error {
	if ok, _, err := t.Ready(); !ok {
		return err
	}
	args := []string{"-ss", fmt.Sprintf("%.3f", max(at, 0)), "-t", fmt.Sprintf("%.3f", length), "-i", path,
		"-vf", t.FlatView(sh, w, h) + ",format=yuv420p", "-sn", "-c:v", "libx264", "-preset", "veryfast", "-crf", "26"}
	if audio {
		args = append(args, "-c:a", "aac", "-b:a", "96k")
	} else {
		args = append(args, "-an")
	}
	out := dst + ".tmp.mp4"
	if err := t.run(ctx, 3*time.Minute, append(args, "-movflags", "+faststart", "-y", out)...); err != nil {
		return err
	}
	return os.Rename(out, dst)
}

func (t *Tools) run(ctx context.Context, limit time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var errb bytes.Buffer
	cmd := exec.CommandContext(ctx, t.FFmpeg, append([]string{"-hide_banner", "-v", "error", "-nostdin"}, args...)...)
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(lastLine(errb.String()))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ffmpeg: %s", msg)
	}
	return nil
}
