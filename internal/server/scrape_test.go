package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

func TestScrapeMediaFindsFiles(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head>
			<meta property="og:video" content="https://cdn.example.com/scene/og.mp4">
			</head><body>
			<a href="/dl/movie_2160p.mp4" download>Download 4K</a>
			<a href="/dl/movie_720.mp4">720p</a>
			<a href="/dl/preview_320.mp4">Preview</a>
			<video src="https://cdn.example.com/trailer_sample.mp4"></video>
			<script>var hls = "https://cdn.example.com/scene/master.m3u8?token=abc";</script>
		</body></html>`))
	}))
	t.Cleanup(srv.Close)
	// the harness admin can reach localhost (scrape guard allows admins)
	u := &store.User{ID: h.alice.ID, Role: "admin"}
	cands, err := h.s.scrapeMedia(context.Background(), u, srv.URL+"/model/x/movie/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	// the 4K download link wins; previews and trailers are gone
	if cands[0].Label != "4K" {
		t.Fatalf("best = %+v", cands[0])
	}
	for _, c := range cands {
		if c.URL == srv.URL+"/dl/preview_320.mp4" || c.URL == "https://cdn.example.com/trailer_sample.mp4" {
			t.Fatalf("preview/trailer kept: %s", c.URL)
		}
	}
	var hasHLS, has720 bool
	for _, c := range cands {
		if c.URL == "https://cdn.example.com/scene/master.m3u8?token=abc" {
			hasHLS = true
		}
		if c.Label == "720p" {
			has720 = true
		}
	}
	if !hasHLS || !has720 {
		t.Fatalf("missing candidates: %+v", cands)
	}
}
