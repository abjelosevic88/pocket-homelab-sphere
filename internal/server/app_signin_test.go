package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// The iPhone app signs in through /app-signin: sign in, Allow, a one-time
// code to pocketsphere://, traded with the PKCE verifier for a token.
func TestAppSignin(t *testing.T) {
	h := newHarness(t)
	ts := httptest.NewServer(h.h)
	defer ts.Close()
	h.s.cfg.BaseURL = ts.URL
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(cookie bool) *http.Response {
		req, _ := http.NewRequest("GET", ts.URL+"/app-signin?"+url.Values{"challenge": {challenge}, "device": {"Alice's iPhone"}}.Encode(), nil)
		if cookie {
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: h.ac})
		}
		res, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	allow := func(origin string) string {
		req, _ := http.NewRequest("POST", ts.URL+"/app-signin", strings.NewReader(url.Values{"challenge": {challenge}, "device": {"Alice's iPhone"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: h.ac})
		res, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusSeeOther {
			return res.Status
		}
		return res.Header.Get("Location")
	}

	// signed out: to the sign-in page, coming back here after it
	res := get(false)
	if loc := res.Header.Get("Location"); res.StatusCode != 302 || !strings.HasPrefix(loc, "/login?next=%2Fapp-signin%3F") {
		t.Fatalf("signed out: %d %s", res.StatusCode, loc)
	}
	// signed in: the question, naming the phone and the person
	res = get(true)
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), "Allow") || !strings.Contains(string(body), "Alice&#39;s iPhone") {
		t.Fatalf("question: %d %s", res.StatusCode, body)
	}
	// another site can't press Allow
	if got := allow("https://evil.example"); !strings.Contains(got, "403") {
		t.Fatalf("cross-site allow: %s", got)
	}
	code := func(loc string) string {
		u, err := url.Parse(loc)
		if err != nil || u.Scheme != "pocketsphere" || u.Query().Get("code") == "" {
			t.Fatalf("callback: %s", loc)
		}
		return u.Query().Get("code")
	}
	exchange := func(c, v string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/auth/app/exchange", strings.NewReader(`{"code":"`+c+`","verifier":"`+v+`"}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	// the wrong verifier gets nothing, and the code is spent
	c1 := code(allow(ts.URL))
	if st, _ := exchange(c1, strings.Repeat("x", 50)); st != 401 {
		t.Fatalf("wrong verifier: %d", st)
	}
	if st, _ := exchange(c1, verifier); st != 401 {
		t.Fatalf("spent code: %d", st)
	}
	// the right one: a token for Alice, named after the phone, that reads her account
	st, out := exchange(code(allow(ts.URL)), verifier)
	tok, _ := out["token"].(string)
	if st != 200 || tok == "" || !strings.Contains(out["info"].(map[string]any)["name"].(string), "Alice's iPhone") {
		t.Fatalf("exchange: %d %v", st, out)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(b), "alice@example.com") {
		t.Fatalf("token: %d %s", res.StatusCode, b)
	}
}

// The iPhone app plays converted videos as HLS: a VOD playlist of 6 s segments.
func TestHLSPlaylist(t *testing.T) {
	h := newHarness(t)
	ids := h.sceneIDs()
	if _, err := h.st.DB.Exec(`UPDATE scenes SET duration = 20 WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	code, _, body := h.do(h.ac, "GET", "/api/v1/scenes/"+strconv.FormatInt(ids[0], 10)+"/stream.m3u8?q=720", nil)
	pl := string(body)
	if code != 200 || !strings.Contains(pl, "#EXT-X-ENDLIST") || strings.Count(pl, "#EXTINF") != 4 ||
		!strings.Contains(pl, "hls/3.ts?q=720") || !strings.Contains(pl, "#EXTINF:2.000,") {
		t.Fatalf("playlist: %d\n%s", code, pl)
	}
	if code, _, _ := h.do(h.ac, "GET", "/api/v1/scenes/"+strconv.FormatInt(ids[0], 10)+"/hls/9.ts", nil); code != 404 {
		t.Fatalf("segment past the end: %d", code)
	}
	if code, _, _ := h.do("", "GET", "/api/v1/scenes/"+strconv.FormatInt(ids[0], 10)+"/stream.m3u8", nil); code != 401 {
		t.Fatalf("signed out: %d", code)
	}
	a := hlsArgs([]string{"-i", "x", "-c:a", "copy", "-movflags", "frag", "-f", "mp4", "pipe:1"}, 12)
	if got := strings.Join(a, " "); !strings.Contains(got, "-c:a aac") || !strings.Contains(got, "-output_ts_offset 12.000") || strings.Contains(got, "movflags") {
		t.Fatalf("args: %s", got)
	}
}
