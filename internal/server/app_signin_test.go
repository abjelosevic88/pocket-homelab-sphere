package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

// HEVC tagged hev1 is served as hvc1 (four bytes), whatever range is asked for.
func TestRelabelHev1(t *testing.T) {
	// ftyp, then moov/trak/mdia/minf/stbl/stsd with one hev1 entry holding an hvcC with VPS, SPS, PPS
	box := func(typ string, body ...[]byte) []byte {
		var b []byte
		for _, x := range body {
			b = append(b, x...)
		}
		h := make([]byte, 8)
		binary.BigEndian.PutUint32(h, uint32(8+len(b)))
		copy(h[4:], typ)
		return append(h, b...)
	}
	hvcc := make([]byte, 22)
	hvcc = append(hvcc, 3)
	for _, typ := range []byte{32, 33, 34} {
		hvcc = append(hvcc, typ, 0, 1, 0, 2, 0xAA, 0xBB)
	}
	entry := box("hev1", make([]byte, 78), box("hvcC", hvcc))
	stsd := box("stsd", []byte{0, 0, 0, 0, 0, 0, 0, 1}, entry)
	file := append(box("ftyp", []byte("isom")), box("moov", box("trak", box("mdia", box("minf", box("stbl", stsd)))))...)
	file = append(file, box("mdat", make([]byte, 100))...)
	p := filepath.Join(t.TempDir(), "v.mp4")
	if err := os.WriteFile(p, file, 0o644); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(p)
	defer f.Close()
	fi, _ := f.Stat()
	offs := hev1Offsets(f, fi)
	if len(offs) != 1 || string(file[offs[0]:offs[0]+4]) != "hev1" {
		t.Fatalf("offsets: %v", offs)
	}
	r := &relabelled{f: f, patches: offs}
	// a range that cuts through the label
	if _, err := r.Seek(offs[0]+2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil || string(b[:2]) != "c1" {
		t.Fatalf("partial read: %q %v", b, err)
	}
	_, _ = r.Seek(0, io.SeekStart)
	all, _ := io.ReadAll(r)
	if !bytes.Contains(all, []byte("hvc1")) || bytes.Contains(all, []byte("hev1")) || len(all) != len(file) {
		t.Fatalf("whole file not relabelled")
	}
	// without the parameter sets in the header it stays as it is
	noSets := bytes.Replace(file, []byte{3, 32, 0, 1}, []byte{0, 32, 0, 1}, 1)
	p2 := filepath.Join(t.TempDir(), "w.mp4")
	_ = os.WriteFile(p2, noSets, 0o644)
	f2, _ := os.Open(p2)
	defer f2.Close()
	fi2, _ := f2.Stat()
	if o := hev1Offsets(f2, fi2); len(o) != 0 {
		t.Fatalf("relabelled without parameter sets: %v", o)
	}
}
