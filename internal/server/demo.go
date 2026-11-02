package server

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/media"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// Demo data (DEMO_DATA=true): a made-up library — studios, performers,
// tags, flat and VR scenes with markers, a gallery and two months of
// watching. The videos are sparse placeholder files (no disk space, no
// picture); covers are abstract light-and-colour pictures, so screenshots
// show nothing anyone could mind.

var demoStudios = []struct {
	name, url string
	vr        bool
}{{"Aurora Studio", "https://example.com/aurora", false}, {"Velvet Room", "https://example.com/velvet", false}, {"Northlight VR", "https://example.com/northlight", true}, {"Atelier Seven", "https://example.com/atelier", false}}

var demoPerformers = []struct {
	name, country, hair string
	born                string
}{{"Mila Hart", "Czechia", "blonde", "1996-03-14"}, {"Ava Lennox", "United Kingdom", "brunette", "1994-08-02"}, {"Nora Vale", "Hungary", "red", "1998-11-21"},
	{"Lena Brooks", "Germany", "blonde", "1993-05-09"}, {"Iris Moreau", "France", "black", "1997-01-30"}, {"Sofia Lane", "Spain", "brunette", "1995-07-17"},
	{"Clara Wynn", "Netherlands", "blonde", "1999-09-05"}, {"Eva Sterling", "Slovakia", "brunette", "1992-12-12"}}

var demoTags = []string{"Romantic", "Outdoor", "POV", "Solo", "Couple", "Massage", "Behind the scenes", "Interview", "Sunset", "Hotel"}

var demoTitles = []string{"Morning Light", "Golden Hour", "Rooftop Evening", "Lazy Sunday", "Hotel Suite", "By the Lake", "Summer Rain", "City Lights",
	"Quiet Weekend", "First Date", "Garden Party", "Late Night Talk", "Beach House", "Winter Cabin", "Studio Session", "Pool Day",
	"Velvet Hour", "Slow Dance", "After the Show", "Prague Afternoon", "Coffee and Sun", "The Long Bath", "Open Window", "Blue Room",
	"Candle Light", "Road Trip", "Sea Breeze", "Paris Balcony", "Soft Focus", "Midnight Swim", "Picnic", "Breakfast in Bed",
	"Library Corner", "Spa Weekend", "Sunrise Yoga", "Last Dance"}

func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	// seeded during the first request: finish even when that request is gone
	ctx = context.WithoutCancel(ctx)
	if _, _, done := s.store.CacheGet(ctx, "demo:seeded"); done {
		return nil
	}
	var have int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM scenes`).Scan(&have)
	if have > 0 {
		return nil
	}
	rnd := rand.New(rand.NewSource(42))
	var studios []int64
	for i, st := range demoStudios {
		x := &store.Studio{Name: st.name, URL: st.url, Details: "A made-up studio for the demo."}
		if err := s.store.SaveStudio(ctx, x); err != nil {
			return err
		}
		studios = append(studios, x.ID)
		_ = s.setImage(ctx, "studios", x.ID, demoArt(rnd, 400, 200, float64(i)*0.9, true))
	}
	var perfs []int64
	for i, p := range demoPerformers {
		x := &store.Performer{Name: p.name, Gender: "female", Country: p.country, HairColor: p.hair, Birthdate: p.born, HeightCM: 160 + rnd.Intn(18),
			Details: "A made-up performer for the demo.", Career: fmt.Sprintf("%d -", 2015+rnd.Intn(7))}
		if err := s.store.SavePerformer(ctx, x); err != nil {
			return err
		}
		perfs = append(perfs, x.ID)
		_ = s.setImage(ctx, "performers", x.ID, demoArt(rnd, 400, 560, float64(i)*0.7+2, false))
		if i < 3 {
			_ = s.store.SetMark(ctx, uid, "performer", x.ID, boolp(true), intp(8+rnd.Intn(3)))
		}
	}
	var tags []int64
	for _, n := range demoTags {
		id, _, err := s.store.EnsureTag(ctx, n)
		if err != nil {
			return err
		}
		tags = append(tags, id)
	}
	vrTag, _, _ := s.store.EnsureTag(ctx, "VR")

	now := time.Now()
	for i, title := range demoTitles {
		si := rnd.Intn(len(studios))
		vr := demoStudios[si].vr || i%9 == 4
		w, h := 1920, 1080
		dur := float64(600 + rnd.Intn(2400))
		if vr {
			w, h = 5760, 2880
			si = 2
		}
		folder := demoStudios[si].name
		name := fmt.Sprintf("%s - %s.mp4", demoStudios[si].name, title)
		rel := folder + "/" + name
		full := filepath.Join(s.cfg.LibraryDir, rel)
		size := int64(dur * float64(400_000+rnd.Intn(600_000)))
		if vr {
			size *= 4
		}
		if err := sparseFile(full, size); err != nil {
			return err
		}
		h0, _ := media.OSHash(full)
		id, err := s.store.AddScene(ctx, store.SceneFile{Path: rel, SizeBytes: size, Mtime: fileMtime(full)}, h0, uid)
		if err != nil {
			return err
		}
		_ = s.store.SetProbe(ctx, id, store.ProbeResult{Duration: dur, Width: w, Height: h, FPS: []float64{29.97, 30, 59.94, 60}[rnd.Intn(4)],
			VideoCodec: []string{"h264", "hevc"}[rnd.Intn(2)], AudioCodec: "aac", Container: "mov,mp4,m4a,3gp,3g2,mj2", Bitrate: int64(float64(size*8) / dur)})
		if vr {
			_ = s.store.SetVR(ctx, id, "equirect180", "sbs", 180)
		}
		date := now.AddDate(0, 0, -rnd.Intn(900)).Format("2006-01-02")
		sid := studios[si]
		ps := &sid
		pp := []int64{perfs[rnd.Intn(len(perfs))]}
		if rnd.Intn(3) == 0 {
			pp = append(pp, perfs[rnd.Intn(len(perfs))])
		}
		tt := []int64{tags[rnd.Intn(len(tags))], tags[rnd.Intn(len(tags))]}
		if vr {
			tt = append(tt, vrTag)
		}
		if err := s.store.UpdateScene(ctx, id, store.ScenePatch{Title: strp(title), Date: &date, StudioID: &ps,
			Details:   strp("A made-up scene for the demo. The video is a placeholder, so it does not play."),
			Organized: boolp(rnd.Intn(3) > 0), Performers: &pp, Tags: &tt}); err != nil {
			return err
		}
		sc, _ := s.store.GetScene(ctx, 0, id)
		_ = s.setSceneCover(ctx, sc, demoArt(rnd, 960, 540, float64(i)*0.37, vr), "frame")
		_ = s.store.SetGenerated(ctx, id, "done", "", false, false)
		_ = s.store.SetPHash(ctx, id, "-")
		// a few moments
		for m := 0; m < rnd.Intn(4); m++ {
			t := tags[rnd.Intn(len(tags))]
			_ = s.store.SaveMarker(ctx, &store.Marker{SceneID: id, Seconds: math.Round(dur * float64(m+1) / 5), PrimaryTagID: &t}, uid)
		}
		// what this person thinks of it, and watching it
		mine := store.MinePatch{}
		if rnd.Intn(2) == 0 {
			mine.Rating = intp(4 + rnd.Intn(7))
		}
		if rnd.Intn(5) == 0 {
			mine.Favorite = boolp(true)
		}
		_ = s.store.PutMine(ctx, uid, id, mine)
		for p := 0; p < rnd.Intn(4); p++ {
			at := now.Add(-time.Duration(rnd.Intn(60*24)) * time.Hour).Add(-time.Duration(rnd.Intn(3600)) * time.Second)
			secs := int64(120 + rnd.Intn(int(dur)/2))
			pid, err := s.store.StartPlay(ctx, uid, id, []string{"browser", "browser", "headset"}[rnd.Intn(3)])
			if err != nil {
				continue
			}
			_, _ = s.store.DB.ExecContext(ctx, `UPDATE plays SET started_at = ?, day = ? WHERE id = ?`, at.UTC().Format(time.RFC3339Nano), at.Format("2006-01-02"), pid)
			pos := float64(secs)
			if rnd.Intn(3) > 0 {
				pos = dur // watched to the end: nothing to continue
			}
			_, _ = s.store.BeatPlay(ctx, uid, pid, secs, pos, dur, 60)
			_, _ = s.store.DB.ExecContext(ctx, `UPDATE user_scenes SET last_played_at = ? WHERE user_id = ? AND scene_id = ?`, at.UTC().Format(time.RFC3339Nano), uid, id)
			if rnd.Intn(4) == 0 {
				_, _ = s.store.AddO(ctx, uid, id, at.Add(time.Duration(secs)*time.Second).UTC().Format(time.RFC3339Nano))
			}
		}
	}
	// a gallery: a folder of pictures
	gdir := filepath.Join(s.cfg.LibraryDir, "Photo sets", "Golden Hour stills")
	if err := os.MkdirAll(gdir, 0o755); err == nil {
		for i := 0; i < 9; i++ {
			_ = os.WriteFile(filepath.Join(gdir, fmt.Sprintf("still-%02d.jpg", i+1)), demoArt(rnd, 900, 1200, float64(i)*0.5+1, false), 0o644)
		}
	}
	// a few downloads and a followed channel
	for i, d := range []struct {
		title, status string
		pct           float64
		err           string
	}{{"Golden Hour (behind the scenes)", "done", 100, ""}, {"Rooftop Evening — extended", "downloading", 47, ""}, {"City Lights", "done", 100, ""}, {"Summer Rain teaser", "failed", 0, "the video is gone (HTTP 404)"}} {
		dl := &store.Download{ID: fmt.Sprintf("demo%d", i), UserID: uid, URL: fmt.Sprintf("https://example.com/videos/%d", 1000+i), Kind: "video", Title: d.title, Status: d.status}
		if err := s.store.CreateDownload(ctx, dl); err == nil {
			size := int64(300+rnd.Intn(900)) << 20
			p := store.DownloadPatch{Percent: &d.pct, SizeBytes: &size, Error: &d.err}
			if d.status == "downloading" {
				p.Speed, p.ETA = strp("8.4 MB/s"), strp("1m12s")
			}
			_ = s.store.UpdateDownload(ctx, dl.ID, p)
		}
	}
	_ = s.store.SaveSubscription(ctx, &store.Subscription{UserID: uid, URL: "https://example.com/channel/aurora", Name: "Aurora Studio (channel)", CheckMinutes: 120, Enabled: true})
	_ = s.store.SaveFilter(ctx, uid, &store.SavedFilter{Mode: "scenes", Name: "VR, newest first", Query: "vr=yes&sort=added", Pinned: true})
	_ = s.store.SaveFilter(ctx, uid, &store.SavedFilter{Mode: "scenes", Name: "Top rated", Query: "rating=8&sort=rating", Pinned: true})
	_ = s.store.CachePut(ctx, "demo:seeded", "1")
	s.wakeScan() // the gallery folder
	s.log.Info("demo data seeded", "scenes", len(demoTitles))
	return nil
}

func intp(v int) *int { return &v }

func fileMtime(p string) string {
	if fi, err := os.Stat(p); err == nil {
		return fi.ModTime().UTC().Format(time.RFC3339)
	}
	return ""
}

// sparseFile makes a file of this size that takes no space on disk.
func sparseFile(p string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(size)
}

// demoArt is an abstract picture: a soft two-colour gradient with blurred
// circles of light (bokeh). wide = a panorama-like band of light.
func demoArt(rnd *rand.Rand, w, h int, hue float64, wide bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	hsv := func(hh, sat, val float64) (float64, float64, float64) {
		hh = math.Mod(hh, 1)
		if hh < 0 {
			hh++
		}
		i := math.Floor(hh * 6)
		f := hh*6 - i
		p, q, t := val*(1-sat), val*(1-f*sat), val*(1-(1-f)*sat)
		switch int(i) % 6 {
		case 0:
			return val, t, p
		case 1:
			return q, val, p
		case 2:
			return p, val, t
		case 3:
			return p, q, val
		case 4:
			return t, p, val
		}
		return val, p, q
	}
	h1 := math.Mod(hue*0.13+0.02, 1)
	ar, ag, ab := hsv(h1, 0.65, 0.35)
	br, bg, bb := hsv(h1+0.12, 0.55, 0.85)
	type light struct{ x, y, r, a, cr, cg, cb float64 }
	var lights []light
	for i := 0; i < 14; i++ {
		lr, lg, lb := hsv(h1+rnd.Float64()*0.2-0.05, 0.35, 1)
		lights = append(lights, light{rnd.Float64() * float64(w), rnd.Float64() * float64(h), (0.04 + rnd.Float64()*0.12) * float64(max(w, h)), 0.25 + rnd.Float64()*0.35, lr, lg, lb})
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := (float64(x)/float64(w))*0.55 + (float64(y)/float64(h))*0.45
			if wide {
				d := math.Abs(float64(y)/float64(h) - 0.55)
				t = 1 - math.Min(d*2.2, 1)
			}
			r := ar + (br-ar)*t
			g := ag + (bg-ag)*t
			b := ab + (bb-ab)*t
			for _, l := range lights {
				dx, dy := float64(x)-l.x, float64(y)-l.y
				d := math.Sqrt(dx*dx+dy*dy) / l.r
				if d < 1.2 {
					k := l.a * math.Max(0, 1-d*d*0.8)
					r += (l.cr - r) * k
					g += (l.cg - g) * k
					b += (l.cb - b) * k
				}
			}
			img.Set(x, y, color.RGBA{uint8(math.Min(r, 1) * 255), uint8(math.Min(g, 1) * 255), uint8(math.Min(b, 1) * 255), 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}
