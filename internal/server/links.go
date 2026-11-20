package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Sphere as
// the target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed in
// linkRoutes stays closed to it (403). Both start off; the library is
// opened to other apps only by the person, one by one.

var linkOffers = []core.Offer{
	{Scope: "sphere:read", Name: "See your video library", Description: "Read your scenes, performers, studios, tags and what you watched (for dashboards or an assistant you trust with it)."},
	{Scope: "sphere:downloads", Name: "Send links to download", Description: "Add a video link to your downloads (for example from Pocket Clips, Reader or a flow)."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
// The library itself stays on the box: this is the person's own marks on it.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "sphere.json", Kind: core.KindPocket, Title: "Your ratings, favourites, plays and where you left each video, your saved views and followed channels (the videos stay on the box)"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", Title: "Puts your ratings, favourites and plays on the same videos here (matched by fingerprint or path) and adds your saved views and channels; what is already here is skipped"},
	},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(scenes|dashboard|home|stats|performers|studios|tags|groups|markers|history)$`), "sphere:read"},
	{"GET", regexp.MustCompile(`^/api/v1/(scenes|performers|studios|tags|groups)/\d+$`), "sphere:read"},
	{"GET", regexp.MustCompile(`^/api/v1/downloads$`), "sphere:downloads"},
	{"POST", regexp.MustCompile(`^/api/v1/downloads$`), "sphere:downloads"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "sphere" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token,
// "" otherwise.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}
