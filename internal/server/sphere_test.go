package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

type harness struct {
	t     *testing.T
	s     *Server
	st    *store.Store
	h     http.Handler
	alice *store.User
	bob   *store.User
	ac    string
	bc    string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg := testConfig(t.TempDir())
	st := openTestStore(t, cfg)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	ctx := context.Background()
	alice := &store.User{Email: "alice@example.com", Name: "Alice", Role: "admin", PasswordHash: "x"}
	bob := &store.User{Email: "bob@example.com", Name: "Bob", Role: "member", PasswordHash: "x"}
	if err := st.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}
	ac, _ := st.CreateSession(ctx, alice.ID, time3h)
	bc, _ := st.CreateSession(ctx, bob.ID, time3h)
	// a library with two videos and a gallery
	for _, f := range []string{"Studio One/First Scene.mp4", "VR/StudioVR.Some.Scene.Oculus.4K.mp4"} {
		p := filepath.Join(cfg.LibraryDir, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := sparseFile(p, 5<<20); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.scanAll(ctx, nil); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, s: s, st: st, h: s.Router(), alice: alice, bob: bob, ac: ac, bc: bc}
}

func (h *harness) do(who, method, path string, body any) (int, map[string]any, []byte) {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if who != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: who})
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m, rec.Body.Bytes()
}

func (h *harness) sceneIDs() []int64 {
	var ids []int64
	rows, _ := h.st.DB.Query(`SELECT id FROM scenes ORDER BY id`)
	defer rows.Close()
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

func TestScanFindsScenesAndVR(t *testing.T) {
	h := newHarness(t)
	ids := h.sceneIDs()
	if len(ids) != 2 {
		t.Fatalf("scenes = %d, want 2", len(ids))
	}
	sc, err := h.st.SceneByPath(context.Background(), "Studio One/First Scene.mp4")
	if err != nil || sc.OSHash == "" {
		t.Fatalf("scene by path: %v %+v", err, sc)
	}
	// renaming a file keeps the scene (same fingerprint)
	lib := h.s.cfg.LibraryDir
	if err := os.Rename(filepath.Join(lib, "Studio One/First Scene.mp4"), filepath.Join(lib, "Studio One/Renamed.mp4")); err != nil {
		t.Fatal(err)
	}
	if err := h.s.scanAll(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	moved, err := h.st.SceneByPath(context.Background(), "Studio One/Renamed.mp4")
	if err != nil || moved.ID != sc.ID {
		t.Fatalf("rename lost the scene: %v (id %d vs %d)", err, moved.ID, sc.ID)
	}
}

func TestPerPersonIsolation(t *testing.T) {
	h := newHarness(t)
	id := h.sceneIDs()[0]
	sid := func(p string) string { return strings.ReplaceAll(p, "{id}", itoa(id)) }

	// the catalogue is shared
	if code, _, _ := h.do(h.bc, "GET", sid("/api/v1/scenes/{id}"), nil); code != 200 {
		t.Fatalf("bob sees the scene: %d", code)
	}
	// Alice rates, favourites, adds an O and watches
	if code, _, b := h.do(h.ac, "PUT", sid("/api/v1/scenes/{id}/mine"), map[string]any{"rating": 9, "favorite": true}); code != 200 {
		t.Fatalf("mine: %d %s", code, b)
	}
	h.do(h.ac, "POST", sid("/api/v1/scenes/{id}/o"), nil)
	_, play, _ := h.do(h.ac, "POST", sid("/api/v1/scenes/{id}/plays"), map[string]any{})
	pid := int64(play["id"].(float64))
	if code, _, _ := h.do(h.ac, "PUT", "/api/v1/plays/"+itoa(pid), map[string]any{"watched": 120, "position": 120, "duration": 600}); code != 200 {
		t.Fatalf("beat: %d", code)
	}
	// Bob sees none of it
	_, m, _ := h.do(h.bc, "GET", sid("/api/v1/scenes/{id}"), nil)
	mine := m["scene"].(map[string]any)["mine"].(map[string]any)
	if mine["rating"].(float64) != 0 || mine["favorite"].(bool) || mine["o_count"].(float64) != 0 || mine["play_count"].(float64) != 0 {
		t.Fatalf("bob sees alice's marks: %v", mine)
	}
	// and cannot touch Alice's sitting
	if code, _, _ := h.do(h.bc, "PUT", "/api/v1/plays/"+itoa(pid), map[string]any{"watched": 999}); code != 404 {
		t.Fatalf("bob beat alice's play: %d", code)
	}
	if code, _, _ := h.do(h.bc, "DELETE", "/api/v1/plays/"+itoa(pid), nil); code != 404 {
		t.Fatalf("bob deleted alice's play: %d", code)
	}
	// Alice's history counted the play
	_, m, _ = h.do(h.ac, "GET", sid("/api/v1/scenes/{id}"), nil)
	if got := m["scene"].(map[string]any)["mine"].(map[string]any)["play_count"].(float64); got != 1 {
		t.Fatalf("alice play_count = %v", got)
	}
	// saved filters, downloads and subscriptions are personal
	_, f, _ := h.do(h.ac, "POST", "/api/v1/filters", map[string]any{"name": "Mine", "query": "vr=yes"})
	fid := itoa(int64(f["id"].(float64)))
	if code, _, _ := h.do(h.bc, "DELETE", "/api/v1/filters/"+fid, nil); code != 404 {
		t.Fatalf("bob deleted alice's filter: %d", code)
	}
	d := &store.Download{ID: "dl1", UserID: h.alice.ID, URL: "https://example.com/v", Kind: "video", Status: "failed"}
	if err := h.st.CreateDownload(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.do(h.bc, "DELETE", "/api/v1/downloads/dl1", nil); code != 404 {
		t.Fatalf("bob deleted alice's download: %d", code)
	}
	_, list, body := h.do(h.bc, "GET", "/api/v1/downloads", nil)
	_ = list
	if strings.Contains(string(body), "dl1") {
		t.Fatal("bob lists alice's downloads")
	}
	x := &store.Subscription{UserID: h.alice.ID, URL: "https://example.com/c", Enabled: true, CheckMinutes: 60}
	if err := h.st.SaveSubscription(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.do(h.bc, "DELETE", "/api/v1/subscriptions/"+itoa(x.ID), nil); code != 404 {
		t.Fatalf("bob deleted alice's subscription: %d", code)
	}
	// performer favourites are personal too
	_, p, _ := h.do(h.ac, "POST", "/api/v1/performers", map[string]any{"name": "Test Person", "favorite": true})
	pidp := itoa(int64(p["id"].(float64)))
	_, bp, _ := h.do(h.bc, "GET", "/api/v1/performers/"+pidp, nil)
	if bp["favorite"].(bool) {
		t.Fatal("bob sees alice's favourite performer")
	}
	// members cannot delete what they did not add, or empty the trash
	if code, _, _ := h.do(h.bc, "DELETE", sid("/api/v1/scenes/{id}"), nil); code != 403 {
		t.Fatalf("bob deleted a scene: %d", code)
	}
	if code, _, _ := h.do(h.bc, "DELETE", sid("/api/v1/scenes/{id}/purge"), nil); code != 403 {
		t.Fatalf("bob purged a scene: %d", code)
	}
}

func TestSettingsArePersonal(t *testing.T) {
	h := newHarness(t)
	h.do(h.ac, "PUT", "/api/v1/settings", map[string]any{"discreet": true, "stashdb_key": "secret-key-123456", "default_sort": "random"})
	_, b, _ := h.do(h.bc, "GET", "/api/v1/settings", nil)
	if b["discreet"].(bool) || b["stashdb_key"].(string) != "" || b["default_sort"] == "random" {
		t.Fatalf("bob got alice's settings: %v", b)
	}
	_, a, _ := h.do(h.ac, "GET", "/api/v1/settings", nil)
	if !strings.Contains(a["stashdb_key"].(string), "••") {
		t.Fatalf("key not masked: %v", a["stashdb_key"])
	}
}

func TestHeadsetFeedsArePersonal(t *testing.T) {
	h := newHarness(t)
	id := h.sceneIDs()[1] // the VR one
	h.do(h.ac, "PUT", "/api/v1/scenes/"+itoa(id)+"/mine", map[string]any{"favorite": true})
	_, hs, _ := h.do(h.bc, "POST", "/api/v1/settings/headset", map[string]any{"action": "new"})
	base := hs["base"].(string)
	path := strings.TrimPrefix(base, "http://example.com")
	_, m, _ := h.do("", "POST", path+"/heresphere/"+itoa(id), map[string]any{"needsMediaSource": true})
	if m["isFavorite"].(bool) {
		t.Fatal("bob's headset shows alice's favourite")
	}
	if m["stereo"] != "sbs" || m["projection"] != "equirectangular" {
		t.Fatalf("VR shape = %v %v", m["projection"], m["stereo"])
	}
	// a wrong key finds nothing
	if code, _, _ := h.do("", "GET", "/vr/not-a-real-key-at-all/deovr", nil); code != 404 {
		t.Fatalf("wrong key: %d", code)
	}
	// rating written on the headset lands on bob, not alice
	h.do("", "POST", path+"/heresphere/"+itoa(id), map[string]any{"rating": 4.5, "isFavorite": true})
	_, am, _ := h.do(h.ac, "GET", "/api/v1/scenes/"+itoa(id), nil)
	if am["scene"].(map[string]any)["mine"].(map[string]any)["rating"].(float64) != 0 {
		t.Fatal("bob's headset rating changed alice's")
	}
	_, bm, _ := h.do(h.bc, "GET", "/api/v1/scenes/"+itoa(id), nil)
	if bm["scene"].(map[string]any)["mine"].(map[string]any)["rating"].(float64) != 9 {
		t.Fatalf("headset rating not stored: %v", bm["scene"].(map[string]any)["mine"])
	}
	// DeoVR library lists it
	_, _, body := h.do("", "GET", path+"/deovr", nil)
	if !strings.Contains(string(body), "/deovr/"+itoa(id)) {
		t.Fatalf("deovr library: %s", body)
	}
}

func TestExportImportIsIdempotent(t *testing.T) {
	h := newHarness(t)
	id := h.sceneIDs()[0]
	h.do(h.ac, "PUT", "/api/v1/scenes/"+itoa(id)+"/mine", map[string]any{"rating": 7})
	h.do(h.ac, "POST", "/api/v1/scenes/"+itoa(id)+"/o", nil)
	_, _, body := h.do(h.ac, "GET", "/api/v1/export.json", nil)
	// into Bob's account, twice
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/v1/import", bytes.NewReader(body))
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: h.bc})
		rec := httptest.NewRecorder()
		h.h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("import %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	_, m, _ := h.do(h.bc, "GET", "/api/v1/scenes/"+itoa(id), nil)
	mine := m["scene"].(map[string]any)["mine"].(map[string]any)
	if mine["rating"].(float64) != 7 || mine["o_count"].(float64) != 1 {
		t.Fatalf("after two imports: %v", mine)
	}
}

func TestLinkScopes(t *testing.T) {
	get := func(p string) *http.Request { return httptest.NewRequest("GET", p, nil) }
	post := func(p string) *http.Request { return httptest.NewRequest("POST", p, nil) }
	if !linkAllows([]string{"sphere:read"}, get("/api/v1/scenes")) {
		t.Fatal("read should list scenes")
	}
	if linkAllows([]string{"sphere:read"}, post("/api/v1/downloads")) {
		t.Fatal("read must not download")
	}
	if !linkAllows([]string{"sphere:downloads"}, post("/api/v1/downloads")) {
		t.Fatal("downloads scope should queue")
	}
	if linkAllows([]string{"sphere:read", "sphere:downloads"}, httptest.NewRequest("DELETE", "/api/v1/scenes/1", nil)) {
		t.Fatal("no link may delete")
	}
	if linkAllows([]string{"sphere:read"}, get("/api/v1/scenes/1/stream")) {
		t.Fatal("no link may stream videos")
	}
}

func TestNiceName(t *testing.T) {
	cases := map[string]string{
		"x/StudioVR.Jane.Doe.Morning.Light.Oculus,.Quest.2..4K..mp4":          "StudioVR Jane Doe Morning Light",
		"x/Studio.20.10.15.Jane.Doe.Movie.Night.XXX.1080p.MP4-GRP.mp4":        "Studio 20 10 15 Jane Doe Movie Night",
		"Studio - Jane Doe - Sunday.mp4":                                      "Studio - Jane Doe - Sunday",
		"[studio.com] - 2020.08.07 - Jane Doe - A day at the lake(1080p).mp4": "2020.08.07 - Jane Doe - A day at the lake(1080p)",
	}
	for in, want := range cases {
		if got := niceName(in); got != want {
			t.Errorf("niceName(%q) = %q, want %q", in, got, want)
		}
	}
}

const time3h = 3 * 60 * 60 * 1e9

func itoa(n int64) string {
	return strings.TrimSpace(strings.Replace(string(rune(0)), "\x00", "", 1)) + jsonNum(n)
}

func jsonNum(n int64) string { b, _ := json.Marshal(n); return string(b) }
