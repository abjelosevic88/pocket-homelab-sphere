package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// a stash-box that knows one performer with two (later three) scenes
func fakeStashBox(t *testing.T, scenes *atomic.Int32) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		q := string(b)
		switch {
		case strings.Contains(q, "searchPerformer"):
			w.Write([]byte(`{"data":{"searchPerformer":[{"id":"perf-1","name":"Jane Doe","aliases":[],"images":[]},{"id":"perf-2","name":"Jane Doerr","aliases":[]}]}}`))
		case strings.Contains(q, "queryScenes"):
			list := []string{`{"id":"sc-new","title":"A Brand New Day Together","date":"2026-10-01","urls":[{"url":"https://example.com/a"}],"images":[],"tags":[],"performers":[],"fingerprints":[]}`,
				`{"id":"sc-have","title":"Already Here Scene","date":"2025-01-01","urls":[],"images":[],"tags":[],"performers":[],"fingerprints":[]}`}
			if scenes.Load() > 2 {
				list = append([]string{`{"id":"sc-later","title":"Released Later On","date":"2026-11-01","urls":[],"images":[],"tags":[],"performers":[],"fingerprints":[]}`}, list...)
			}
			w.Write([]byte(`{"data":{"queryScenes":{"count":` + string(rune('0'+len(list))) + `,"scenes":[` + strings.Join(list, ",") + `]}}}`))
		default:
			w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverMissingFollowAndWishlist(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var n atomic.Int32
	n.Store(2)
	sb := fakeStashBox(t, &n)
	h.do(h.ac, "PUT", "/api/v1/settings", map[string]any{"stashdb_key": "key-123456789", "stashbox_url": sb.URL})
	_, p, _ := h.do(h.ac, "POST", "/api/v1/performers", map[string]any{"name": "Jane Doe"})
	pid := int64(p["id"].(float64))
	// one scene of hers is already in the library (identified earlier)
	sid := h.sceneIDs()[0]
	ids := map[string]string{sb.URL: "sc-have"}
	if err := h.st.UpdateScene(ctx, sid, store.ScenePatch{StashIDs: &ids}); err != nil {
		t.Fatal(err)
	}

	_, d, body := h.do(h.ac, "GET", "/api/v1/performers/"+itoa(pid)+"/discover", nil)
	items, _ := d["items"].([]any)
	if len(items) != 2 || d["remote_id"] != "perf-1" {
		t.Fatalf("discover: %s", body)
	}
	byID := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		byID[m["scene"].(map[string]any)["id"].(string)] = m
	}
	if byID["sc-have"]["have_id"].(float64) != float64(sid) || byID["sc-new"]["have_id"].(float64) != 0 {
		t.Fatalf("have: %v", byID)
	}
	// the match by name was remembered
	perf, _ := h.st.GetPerformer(ctx, 0, pid)
	if perf.StashIDs[sb.URL] != "perf-1" {
		t.Fatalf("stash id not kept: %v", perf.StashIDs)
	}

	// wishlist: Alice wants the new one; Bob sees none of it
	_, wres, _ := h.do(h.ac, "POST", "/api/v1/wanted", map[string]any{"scene": byID["sc-new"]["scene"]})
	wid := int64(wres["id"].(float64))
	if code, _, _ := h.do(h.bc, "DELETE", "/api/v1/wanted/"+itoa(wid), nil); code != 404 {
		t.Fatalf("bob removed alice's wish: %d", code)
	}
	_, _, bw := h.do(h.bc, "GET", "/api/v1/wanted", nil)
	if strings.Contains(string(bw), "sc-new") {
		t.Fatal("bob sees alice's wishlist")
	}
	// identifying a file as that scene ticks the wish off
	n2, _ := h.st.MarkWantedFound(ctx, sb.URL, "sc-new", sid)
	if n2 != 1 {
		t.Fatalf("wish not found: %d", n2)
	}

	// following: the first look only remembers, the next finds the new one
	_, f, _ := h.do(h.ac, "POST", "/api/v1/follows", map[string]any{"kind": "performer", "item_id": pid})
	fid := int64(f["id"].(float64))
	fol, _ := h.st.GetFollow(ctx, h.alice.ID, fid)
	if got, err := h.s.checkFollow(ctx, fol); err != nil || got != 0 {
		t.Fatalf("first check: %d %v", got, err)
	}
	n.Store(3)
	fol, _ = h.st.GetFollow(ctx, h.alice.ID, fid)
	if got, err := h.s.checkFollow(ctx, fol); err != nil || got != 1 {
		t.Fatalf("second check: %d %v", got, err)
	}
	_, _, rel := h.do(h.ac, "GET", "/api/v1/releases", nil)
	var rl []map[string]any
	_ = json.Unmarshal(rel, &rl)
	if len(rl) != 1 || rl[0]["scene"].(map[string]any)["id"] != "sc-later" {
		t.Fatalf("releases: %s", rel)
	}
	if code, _, _ := h.do(h.bc, "DELETE", "/api/v1/follows/"+itoa(fid), nil); code != 404 {
		t.Fatalf("bob removed alice's follow: %d", code)
	}
	_, _, br := h.do(h.bc, "GET", "/api/v1/releases", nil)
	if strings.TrimSpace(string(br)) != "[]" {
		t.Fatalf("bob sees alice's new scenes: %s", br)
	}
}
