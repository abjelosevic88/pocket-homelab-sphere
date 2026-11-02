package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// Headset players: DeoVR and HereSphere read a JSON library from an address
// with the person's secret headset key in it (they cannot sign in the way a
// browser does). Settings → Headset shows the address and can make a new key.
// HereSphere also writes favourites and ratings back and reports where you
// are in a video, so plays and "continue watching" work from the headset.

func (s *Server) mountHeadset(r chi.Router) {
	r.Route("/vr/{key}", func(r chi.Router) {
		r.Use(s.headsetAuth)
		r.HandleFunc("/deovr", s.handleDeoVRLibrary)
		r.HandleFunc("/deovr/{id}", s.handleDeoVRScene)
		r.HandleFunc("/heresphere", s.handleHereSphereLibrary)
		r.HandleFunc("/heresphere/scan", s.handleHereSphereScan)
		r.HandleFunc("/heresphere/events/{id}", s.handleHereSphereEvent)
		r.HandleFunc("/heresphere/{id}", s.handleHereSphereScene)
		r.Get("/scenes/{id}/stream", s.handleStream)
		r.Head("/scenes/{id}/stream", s.handleStream)
		r.Get("/scenes/{id}/stream.mp4", s.handleConverted)
		r.Get("/scenes/{id}/cover", s.handleSceneCover)
		r.Get("/scenes/{id}/preview.mp4", s.handlePreview)
	})
}

func (s *Server) headsetAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.store.UserByHeadsetKey(r.Context(), chi.URLParam(r, "key"))
		if err != nil {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

// origin is the address the headset reached us on.
func (s *Server) origin(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) vrBase(r *http.Request) string {
	return s.origin(r) + "/vr/" + chi.URLParam(r, "key")
}

// headsetShelves are the lists shown on the headset.
func (s *Server) headsetShelves(ctx context.Context, uid int64) []struct {
	name string
	list []store.Scene
} {
	get := func(f store.SceneFilter) []store.Scene {
		f.UserID = uid
		if f.Limit == 0 {
			f.Limit = 1000
		}
		list, _, _ := s.store.ListScenes(ctx, f)
		s.decorateAll(list)
		return list
	}
	shelves := []struct {
		name string
		list []store.Scene
	}{
		{"VR", get(store.SceneFilter{VR: "yes", Sort: "added", Desc: true})},
		{"Continue watching", get(store.SceneFilter{InProgress: true, Sort: "played", Desc: true, Limit: 50})},
		{"Favourites", get(store.SceneFilter{Favorite: true, Sort: "added", Desc: true})},
		{"Recently added", get(store.SceneFilter{Sort: "added", Desc: true, Limit: 100})},
		{"Everything", get(store.SceneFilter{Sort: "date", Desc: true})},
	}
	out := shelves[:0]
	for _, sh := range shelves {
		if len(sh.list) > 0 {
			out = append(out, sh)
		}
	}
	return out
}

// streamFor is the address a headset plays a scene from.
func (s *Server) streamFor(r *http.Request, sc *store.Scene) string {
	base := fmt.Sprintf("%s/scenes/%d", s.vrBase(r), sc.ID)
	ext := strings.ToLower(strings.TrimPrefix(pathExt(sc.Path), "."))
	if directContainers[ext] && codecMime(sc) != "" {
		return base + "/stream"
	}
	if sc.VideoCodec == "h264" || sc.VideoCodec == "hevc" {
		return base + "/stream.mp4?q=original"
	}
	return base + "/stream.mp4?q=2160"
}

func pathExt(p string) string {
	if i := strings.LastIndex(p, "."); i > strings.LastIndex(p, "/") {
		return p[i:]
	}
	return ""
}

// ---------- DeoVR ----------

func deoScreen(sc *store.Scene) (bool, string, string) {
	if !shapeOf(sc).IsVR() {
		return false, "flat", "off"
	}
	screen := "dome"
	switch sc.Projection {
	case "equirect360":
		screen = "sphere"
	case "fisheye":
		switch {
		case sc.FOV >= 220:
			screen = "mkx220"
		case sc.FOV >= 200:
			screen = "mkx200"
		case sc.FOV >= 190:
			screen = "rf52"
		default:
			screen = "fisheye"
		}
	}
	stereo := "off"
	switch sc.Stereo {
	case "sbs":
		stereo = "sbs"
	case "tb":
		stereo = "tb"
	}
	return true, screen, stereo
}

func (s *Server) handleDeoVRLibrary(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	type item struct {
		Title        string `json:"title"`
		VideoLength  int    `json:"videoLength"`
		ThumbnailURL string `json:"thumbnailUrl"`
		VideoURL     string `json:"video_url"`
	}
	type shelf struct {
		Name string `json:"name"`
		List []item `json:"list"`
	}
	out := struct {
		Scenes     []shelf `json:"scenes"`
		Authorized string  `json:"authorized"`
	}{Scenes: []shelf{}, Authorized: "1"}
	base := s.vrBase(r)
	for _, sh := range s.headsetShelves(r.Context(), u.ID) {
		sf := shelf{Name: sh.name, List: []item{}}
		for _, sc := range sh.list {
			sf.List = append(sf.List, item{Title: sc.Name, VideoLength: int(sc.Duration), ThumbnailURL: fmt.Sprintf("%s/scenes/%d/cover", base, sc.ID),
				VideoURL: fmt.Sprintf("%s/deovr/%d", base, sc.ID)})
		}
		out.Scenes = append(out.Scenes, sf)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeoVRScene(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	sc, err := s.store.GetScene(ctx, u.ID, idParam(r, "id"))
	if err != nil || sc.TrashedAt != "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.decorate(sc)
	base := s.vrBase(r)
	is3d, screen, stereo := deoScreen(sc)
	res := sc.Height
	if is3d && sc.Width > 0 {
		res = sc.Width
	}
	marks, _, _ := s.store.ListMarkers(ctx, store.MarkerFilter{SceneID: sc.ID, Limit: 500})
	stamps := []map[string]any{}
	for _, m := range marks {
		name := m.Title
		if name == "" && m.Tag != nil {
			name = m.Tag.Name
		}
		stamps = append(stamps, map[string]any{"ts": int(m.Seconds), "name": name})
	}
	actors := []map[string]any{}
	for _, p := range sc.Performers {
		actors = append(actors, map[string]any{"id": p.ID, "name": p.Name})
	}
	cats := []map[string]any{}
	for _, t := range sc.Tags {
		cats = append(cats, map[string]any{"tag": map[string]any{"id": t.ID, "name": t.Name}})
	}
	out := map[string]any{
		"id": sc.ID, "title": sc.Name, "description": sc.Details, "videoLength": int(sc.Duration),
		"is3d": is3d, "screenType": screen, "stereoMode": stereo, "skipIntro": 0,
		"encodings":    []map[string]any{{"name": firstNonEmpty(sc.VideoCodec, "h264"), "videoSources": []map[string]any{{"resolution": res, "url": s.streamFor(r, sc)}}}},
		"thumbnailUrl": fmt.Sprintf("%s/scenes/%d/cover", base, sc.ID), "timeStamps": stamps, "actors": actors, "categories": cats,
		"fullVideoReady": true, "fullAccess": true, "isFavorite": sc.Mine.Favorite, "rating": float64(sc.Mine.Rating) / 2,
	}
	if sc.HasPreview {
		out["videoPreview"] = fmt.Sprintf("%s/scenes/%d/preview.mp4", base, sc.ID)
	}
	if sc.Studio != nil {
		out["paysite"] = map[string]any{"id": sc.Studio.ID, "name": sc.Studio.Name, "is3rdParty": true}
	}
	if t, err := time.Parse("2006-01-02", sc.Date); err == nil {
		out["date"] = t.Unix()
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- HereSphere ----------

func hereSphereHeaders(w http.ResponseWriter) {
	w.Header().Set("HereSphere-JSON-Version", "1")
}

// hsBody is what HereSphere posts: maybe a sign-in (ignored, the key is in
// the address), whether it needs the video's address, and changes.
type hsBody struct {
	NeedsMediaSource *bool    `json:"needsMediaSource"`
	IsFavorite       *bool    `json:"isFavorite"`
	Rating           *float64 `json:"rating"`
	DeleteFile       *bool    `json:"deleteFile"`
}

func (s *Server) handleHereSphereLibrary(w http.ResponseWriter, r *http.Request) {
	hereSphereHeaders(w)
	u := userFrom(r.Context())
	base := s.vrBase(r)
	type shelf struct {
		Name string   `json:"name"`
		List []string `json:"list"`
	}
	lib := []shelf{}
	for _, sh := range s.headsetShelves(r.Context(), u.ID) {
		sf := shelf{Name: sh.name, List: []string{}}
		for _, sc := range sh.list {
			sf.List = append(sf.List, fmt.Sprintf("%s/heresphere/%d", base, sc.ID))
		}
		lib = append(lib, sf)
	}
	writeJSON(w, http.StatusOK, map[string]any{"access": 1, "library": lib})
}

func (s *Server) hsTags(ctx context.Context, sc *store.Scene) []map[string]any {
	tags := []map[string]any{}
	for _, p := range sc.Performers {
		tags = append(tags, map[string]any{"name": "Talent:" + p.Name})
	}
	if sc.Studio != nil {
		tags = append(tags, map[string]any{"name": "Studio:" + sc.Studio.Name})
	}
	for _, t := range sc.Tags {
		tags = append(tags, map[string]any{"name": "Category:" + t.Name})
	}
	marks, _, _ := s.store.ListMarkers(ctx, store.MarkerFilter{SceneID: sc.ID, Limit: 500})
	for i, m := range marks {
		name := m.Title
		if name == "" && m.Tag != nil {
			name = m.Tag.Name
		}
		end := m.Seconds + 30
		if m.EndSeconds != nil {
			end = *m.EndSeconds
		} else if i+1 < len(marks) && marks[i+1].Seconds > m.Seconds {
			end = marks[i+1].Seconds
		}
		tags = append(tags, map[string]any{"name": "Marker:" + name, "start": m.Seconds * 1000, "end": end * 1000, "track": 0})
	}
	return tags
}

func hsShape(sc *store.Scene) (projection, stereo, lens string, fov float64) {
	projection, stereo, lens, fov = "perspective", "mono", "Linear", 180
	if !shapeOf(sc).IsVR() {
		return
	}
	switch sc.Projection {
	case "equirect180":
		projection = "equirectangular"
	case "equirect360":
		projection, fov = "equirectangular360", 360
	case "fisheye":
		projection, fov = "fisheye", float64(max(sc.FOV, 180))
		switch {
		case sc.FOV >= 220:
			lens = "MKX220"
		case sc.FOV >= 200:
			lens = "MKX200"
		}
	}
	switch sc.Stereo {
	case "sbs":
		stereo = "sbs"
	case "tb":
		stereo = "tb"
	}
	return
}

func (s *Server) handleHereSphereScene(w http.ResponseWriter, r *http.Request) {
	hereSphereHeaders(w)
	ctx := r.Context()
	u := userFrom(ctx)
	sc, err := s.store.GetScene(ctx, u.ID, idParam(r, "id"))
	if err != nil || sc.TrashedAt != "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in hsBody
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in)
	}
	// changes made on the headset
	if in.IsFavorite != nil || in.Rating != nil {
		p := store.MinePatch{Favorite: in.IsFavorite}
		if in.Rating != nil {
			n := int(*in.Rating*2 + 0.5)
			n = min(max(n, 0), 10)
			p.Rating = &n
		}
		_ = s.store.PutMine(ctx, u.ID, sc.ID, p)
		sc, _ = s.store.GetScene(ctx, u.ID, sc.ID)
	}
	s.decorate(sc)
	base := s.vrBase(r)
	projection, stereo, lens, fov := hsShape(sc)
	out := map[string]any{
		"access": 1, "title": sc.Name, "description": sc.Details,
		"thumbnailImage": fmt.Sprintf("%s/scenes/%d/cover", base, sc.ID),
		"dateAdded":      strings.SplitN(sc.CreatedAt, "T", 2)[0],
		"duration":       sc.Duration * 1000, "rating": float64(sc.Mine.Rating) / 2, "isFavorite": sc.Mine.Favorite,
		"favorites": 0, "comments": 0,
		"projection": projection, "stereo": stereo, "isEyeSwapped": false, "fov": fov, "lens": lens, "cameraIPD": 6.5,
		"tags": s.hsTags(ctx, sc), "scripts": []any{}, "subtitles": []any{},
		"eventServer":   fmt.Sprintf("%s/heresphere/events/%d", base, sc.ID),
		"writeFavorite": true, "writeRating": true, "writeTags": false, "writeHSP": false,
	}
	if sc.HasPreview {
		out["thumbnailVideo"] = fmt.Sprintf("%s/scenes/%d/preview.mp4", base, sc.ID)
	}
	if validLooseDate(sc.Date) && sc.Date != "" {
		out["dateReleased"] = sc.Date
	}
	if in.NeedsMediaSource == nil || *in.NeedsMediaSource {
		out["media"] = []map[string]any{{"name": "Original", "sources": []map[string]any{{
			"resolution": max(sc.Height, 1), "height": sc.Height, "width": sc.Width, "size": sc.SizeBytes, "url": s.streamFor(r, sc)}}}}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleHereSphereScan(w http.ResponseWriter, r *http.Request) {
	hereSphereHeaders(w)
	ctx := r.Context()
	u := userFrom(ctx)
	list, _, err := s.store.ListScenes(ctx, store.SceneFilter{UserID: u.ID, Sort: "added", Desc: true, Limit: 1000})
	if err != nil {
		s.fail(w, err, "scan")
		return
	}
	base := s.vrBase(r)
	data := []map[string]any{}
	for i := range list {
		sc := &list[i]
		s.decorate(sc)
		item := map[string]any{"link": fmt.Sprintf("%s/heresphere/%d", base, sc.ID), "title": sc.Name,
			"dateAdded": strings.SplitN(sc.CreatedAt, "T", 2)[0], "duration": sc.Duration * 1000, "rating": float64(sc.Mine.Rating) / 2,
			"favorites": 0, "comments": 0, "isFavorite": sc.Mine.Favorite, "tags": s.hsTags(ctx, sc)}
		if sc.Date != "" {
			item["dateReleased"] = sc.Date
		}
		data = append(data, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"scanData": data})
}

// headset sittings in progress: person+scene → the play row and when it began
var hsPlays = struct {
	sync.Mutex
	m map[string]*hsPlay
}{m: map[string]*hsPlay{}}

type hsPlay struct {
	id      int64
	watched float64 // seconds while playing
	last    time.Time
	playing bool
	seen    time.Time
}

// handleHereSphereEvent takes HereSphere's open/play/pause/close reports
// (event 0..3, time in ms) and keeps the sitting and the place to resume.
func (s *Server) handleHereSphereEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Event int     `json:"event"`
		Time  float64 `json:"time"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	sc, err := s.store.GetScene(ctx, u.ID, idParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	key := fmt.Sprintf("%d:%d", u.ID, sc.ID)
	now := time.Now()
	hsPlays.Lock()
	p := hsPlays.m[key]
	if p == nil || now.Sub(p.seen) > 3*time.Hour {
		id, err := s.store.StartPlay(ctx, u.ID, sc.ID, "heresphere")
		if err != nil {
			hsPlays.Unlock()
			s.fail(w, err, "play")
			return
		}
		p = &hsPlay{id: id, last: now}
		hsPlays.m[key] = p
	}
	if p.playing {
		p.watched += now.Sub(p.last).Seconds()
	}
	p.last, p.seen = now, now
	p.playing = in.Event == 1
	watched := int64(p.watched)
	id := p.id
	if in.Event == 3 {
		delete(hsPlays.m, key)
	}
	for k, x := range hsPlays.m {
		if now.Sub(x.seen) > 6*time.Hour {
			delete(hsPlays.m, k)
		}
	}
	hsPlays.Unlock()
	st, _ := s.store.GetSettings(ctx, u.ID)
	_, _ = s.store.BeatPlay(ctx, u.ID, id, watched, in.Time/1000, sc.Duration, st.CountAfter)
	w.WriteHeader(http.StatusOK)
}
