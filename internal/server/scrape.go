package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// When yt-dlp does not support a page, Sphere reads the page itself (with the
// person's cookies) and looks for the real video files on it — the download
// links member sites put on a scene page. The best one is downloaded; several
// are offered to choose from. Clips, trailers and previews are left out.

var errBlocked = errors.New("addresses on the local network can only be fetched by an admin")

var (
	reSource   = regexp.MustCompile(`(?is)<(?:source|video)\b[^>]*\bsrc=["']([^"']+)["']`)
	reHref     = regexp.MustCompile(`(?is)<a\b([^>]*)\bhref=["']([^"']+\.(?:mp4|m4v|mkv|webm|mov|wmv|avi)(?:\?[^"']*)?)["']`)
	reMeta     = regexp.MustCompile(`(?is)<meta\b[^>]*(?:property|name)=["'](?:og:video(?::secure_url|:url)?|twitter:player:stream)["'][^>]*content=["']([^"']+)["']`)
	reContent  = regexp.MustCompile(`(?is)"contentUrl"\s*:\s*"([^"]+)"`)
	reBare     = regexp.MustCompile(`(?i)https?:\\?/\\?/[^\s"'<>\\]+\.(?:mp4|m4v|m3u8|mpd)(?:\?[^\s"'<>\\]*)?`)
	reBaseHref = regexp.MustCompile(`(?is)<base\b[^>]*\bhref=["']([^"']+)["']`)
	rePreview  = regexp.MustCompile(`(?i)(preview|trailer|sample|teaser|thumb|poster|sprite|watermark|promo|/ads?/|scrubber|timeline|sprite)`)
	reQual4k   = regexp.MustCompile(`(?i)(2160|4k|uhd|2880|3840)`)
	reQual1440 = regexp.MustCompile(`(?i)(1440|2k)`)
	reQual1080 = regexp.MustCompile(`(?i)(1080|fullhd|fhd|fullhigh)`)
	reQual720  = regexp.MustCompile(`(?i)(720|hd720|\bhd\b)`)
	reQual480  = regexp.MustCompile(`(?i)(480|sd)`)
)

// scoreMedia ranks a candidate: higher = more likely the full, best file.
func scoreMedia(u, label string) int {
	hay := u + " " + label
	s := 0
	switch {
	case reQual4k.MatchString(hay):
		s += 400
	case reQual1440.MatchString(hay):
		s += 300
	case reQual1080.MatchString(hay):
		s += 200
	case reQual720.MatchString(hay):
		s += 100
	case reQual480.MatchString(hay):
		s += 40
	}
	ext := strings.ToLower(path.Ext(strings.SplitN(u, "?", 2)[0]))
	switch ext {
	case ".mp4", ".m4v":
		s += 30
	case ".mkv", ".mov":
		s += 20
	case ".webm":
		s += 10
	case ".m3u8":
		s += 5 // a playlist yt-dlp can still fetch, but a direct file is nicer
	case ".mpd":
		s += 3
	}
	if strings.Contains(strings.ToLower(label), "download") {
		s += 25
	}
	return s
}

// cookieJar builds a cookie jar from the person's uploaded cookies.txt.
func (s *Server) cookieJar(uid int64) http.CookieJar {
	jar, _ := cookiejar.New(nil)
	b, err := os.ReadFile(s.cookiesFile(uid))
	if err != nil {
		return jar
	}
	byHost := map[string][]*http.Cookie{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 7 {
			continue
		}
		domain, cpath, secure, name, value := f[0], f[2], f[3], f[5], f[6]
		host := strings.TrimPrefix(domain, ".")
		c := &http.Cookie{Name: name, Value: value, Path: cpath, Secure: strings.EqualFold(secure, "true")}
		if strings.HasPrefix(domain, ".") {
			c.Domain = domain
		}
		byHost[host] = append(byHost[host], c)
	}
	for host, cks := range byHost {
		if u, err := url.Parse("https://" + host + "/"); err == nil {
			jar.SetCookies(u, cks)
		}
	}
	return jar
}

// scrapeClient fetches a page; addresses on the local network only for admins.
func (s *Server) scrapeClient(u *store.User) *http.Client {
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	if u == nil || u.Role != "admin" {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, _ := net.SplitHostPort(address)
			if ip := net.ParseIP(host); ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || isCGNAT(ip) {
				return errBlocked
			}
			return nil
		}
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: dialer.DialContext, TLSHandshakeTimeout: 20 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	return &http.Client{Transport: tr, Jar: s.cookieJar(u.ID), Timeout: 45 * time.Second, CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 8 {
			return errBlocked
		}
		return nil
	}}
}

// mediaChoice is one video file found on a page.
type mediaChoice = store.DownloadChoice

// scrapeMedia fetches a page with the person's cookies and returns the video
// files on it, best first. An empty list means it found none.
func (s *Server) scrapeMedia(ctx context.Context, u *store.User, pageURL string) ([]mediaChoice, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := s.scrapeClient(u).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "html") && !strings.Contains(ct, "xml") && ct != "" {
		return nil, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	html := string(body)

	base, _ := url.Parse(resp.Request.URL.String())
	if m := reBaseHref.FindStringSubmatch(html); m != nil {
		if b, err := url.Parse(strings.TrimSpace(m[1])); err == nil {
			base = resp.Request.URL.ResolveReference(b)
		}
	}
	resolve := func(raw string) string {
		raw = strings.ReplaceAll(strings.TrimSpace(raw), "\\/", "/")
		raw = strings.ReplaceAll(raw, "&amp;", "&")
		r, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		abs := base.ResolveReference(r)
		if abs.Scheme != "http" && abs.Scheme != "https" {
			return ""
		}
		abs.Fragment = ""
		return abs.String()
	}

	type cand struct {
		url, label string
	}
	var found []cand
	add := func(raw, label string) {
		if u := resolve(raw); u != "" {
			found = append(found, cand{u, label})
		}
	}
	for _, m := range reSource.FindAllStringSubmatch(html, -1) {
		add(m[1], "")
	}
	for _, m := range reHref.FindAllStringSubmatch(html, -1) {
		label := m[1]
		if strings.Contains(strings.ToLower(m[1]), "download") {
			label = "download " + label
		}
		add(m[2], label)
	}
	for _, m := range reMeta.FindAllStringSubmatch(html, -1) {
		add(m[1], "")
	}
	for _, m := range reContent.FindAllStringSubmatch(html, -1) {
		add(m[1], "")
	}
	for _, u := range reBare.FindAllString(html, -1) {
		add(u, "")
	}

	// keep the best per unique URL, drop previews
	best := map[string]mediaChoice{}
	for _, c := range found {
		low := strings.ToLower(c.url)
		if rePreview.MatchString(low) && !strings.Contains(strings.ToLower(c.label), "download") {
			continue
		}
		score := scoreMedia(c.url, c.label)
		if ex, ok := best[c.url]; ok && ex.Score >= score {
			continue
		}
		best[c.url] = mediaChoice{URL: c.url, Label: strings.TrimSpace(mediaLabel(c.url, c.label)), Score: score}
	}
	out := make([]mediaChoice, 0, len(best))
	for _, c := range best {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].URL < out[j].URL
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out, nil
}

// mediaLabel is a short name for a found file (a quality, or its file name).
func mediaLabel(u, label string) string {
	hay := u + " " + label
	switch {
	case reQual4k.MatchString(hay):
		return "4K"
	case reQual1440.MatchString(hay):
		return "1440p"
	case reQual1080.MatchString(hay):
		return "1080p"
	case reQual720.MatchString(hay):
		return "720p"
	case reQual480.MatchString(hay):
		return "480p"
	}
	name := path.Base(strings.SplitN(u, "?", 2)[0])
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}
