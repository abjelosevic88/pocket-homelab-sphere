package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/lookup"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// Discover: the scenes of a performer or studio you do not have yet, from
// StashDB or TPDB (each person's own keys); a wishlist that ticks itself off
// when the file arrives and is identified; following performers and studios
// for their new scenes. The lookup sites only describe scenes — the videos
// are wherever the studio sells them; a link yt-dlp can read can be sent to
// Downloads.

const discoverPer = 40

// discoverItem is one remote scene with what this person already has of it.
type discoverItem struct {
	Scene    lookup.Scene `json:"scene"`
	HaveID   int64        `json:"have_id"`
	WantedID int64        `json:"wanted_id"`
	Release  int64        `json:"release_id,omitempty"`
}

// sources lists what a person can look things up on, preferred first.
func sources(st store.Settings) []string {
	var out []string
	if st.StashDBKey != "" {
		out = append(out, "stashdb")
	}
	if st.TPDBKey != "" {
		out = append(out, "tpdb")
	}
	return out
}

func pickSource(st store.Settings, want string) (string, error) {
	avail := sources(st)
	if len(avail) == 0 {
		return "", errors.New("add a StashDB or TPDB key under Settings → Lookups first")
	}
	for _, a := range avail {
		if a == want {
			return a, nil
		}
	}
	return avail[0], nil
}

func endpointFor(st store.Settings, source string) string {
	if source == "tpdb" {
		return "tpdb"
	}
	return firstNonEmpty(st.StashBoxURL, lookup.StashDB)
}

func sourceName(source string) string {
	if source == "tpdb" {
		return "TPDB"
	}
	return "StashDB"
}

// remoteFor finds the performer's or studio's id on the source: the one
// remembered from a lookup, else the single exact match by name (then
// remembered).
func (s *Server) remoteFor(ctx context.Context, st store.Settings, source, kind string, itemID int64) (string, string, error) {
	sb, tp := s.boxes(st)
	ep := endpointFor(st, source)
	switch kind {
	case "performer":
		p, err := s.store.GetPerformer(ctx, 0, itemID)
		if err != nil {
			return "", "", err
		}
		if id := p.StashIDs[ep]; id != "" {
			return id, p.Name, nil
		}
		var found []lookup.Performer
		if source == "tpdb" {
			found, err = tp.SearchPerformer(ctx, p.Name)
		} else {
			found, err = sb.SearchPerformer(ctx, p.Name)
		}
		if err != nil {
			return "", "", err
		}
		var exact []lookup.Performer
		for _, f := range found {
			same := strings.EqualFold(f.Name, p.Name)
			for _, a := range f.Aliases {
				same = same || strings.EqualFold(a, p.Name)
			}
			if same && (p.Disambiguation == "" || f.Disambiguation == "" || strings.EqualFold(f.Disambiguation, p.Disambiguation)) {
				exact = append(exact, f)
			}
		}
		switch len(exact) {
		case 0:
			return "", "", fmt.Errorf("%s has nobody called %q — check the name, or use Look up on this page", sourceName(source), p.Name)
		case 1:
		default:
			return "", "", fmt.Errorf("%s has %d performers called %q — choose the right one with Look up on this page", sourceName(source), len(exact), p.Name)
		}
		p.StashIDs[ep] = exact[0].ID
		_ = s.store.SavePerformer(ctx, p)
		return exact[0].ID, p.Name, nil
	case "studio":
		x, err := s.store.GetStudio(ctx, 0, itemID)
		if err != nil {
			return "", "", err
		}
		if id := x.StashIDs[ep]; id != "" {
			return id, x.Name, nil
		}
		var id string
		if source == "tpdb" {
			list, err := tp.SearchSite(ctx, x.Name)
			if err != nil {
				return "", "", err
			}
			for _, f := range list {
				if strings.EqualFold(f.Name, x.Name) {
					if id != "" {
						return "", "", fmt.Errorf("TPDB has more than one studio called %q", x.Name)
					}
					id = f.ID
				}
			}
		} else {
			f, err := sb.FindStudio(ctx, x.Name)
			if err != nil {
				return "", "", err
			}
			if f != nil {
				id = f.ID
			}
		}
		if id == "" {
			return "", "", fmt.Errorf("%s has no studio called %q — check the name (or other names)", sourceName(source), x.Name)
		}
		x.StashIDs[ep] = id
		_ = s.store.SaveStudio(ctx, x)
		return id, x.Name, nil
	}
	return "", "", errors.New("unknown kind")
}

// remoteScenes is one page of the remote list, kept for 6 hours.
func (s *Server) remoteScenes(ctx context.Context, st store.Settings, source, kind, remoteID string, page int, fresh bool) (*lookup.ScenePage, error) {
	key := fmt.Sprintf("disc:%s:%s:%s:%d", endpointFor(st, source), kind, remoteID, page)
	if !fresh {
		if v, at, ok := s.store.CacheGet(ctx, key); ok {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 6*time.Hour {
				var p lookup.ScenePage
				if json.Unmarshal([]byte(v), &p) == nil {
					return &p, nil
				}
			}
		}
	}
	sb, tp := s.boxes(st)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var p *lookup.ScenePage
	var err error
	if source == "tpdb" {
		p, err = tp.ScenesOf(ctx, kind, remoteID, page, discoverPer)
	} else {
		p, err = sb.ScenesOf(ctx, kind, remoteID, page, discoverPer)
	}
	if err != nil {
		return nil, err
	}
	if b, err := json.Marshal(p); err == nil {
		_ = s.store.CachePut(ctx, key, string(b))
	}
	return p, nil
}

// libraryMatcher tells whether the library already has a remote scene.
type libraryMatcher struct {
	ids, titles map[string]int64
}

func (s *Server) newMatcher(ctx context.Context) libraryMatcher {
	ids, titles, _ := s.store.LibraryKeys(ctx)
	return libraryMatcher{ids, titles}
}

func (m libraryMatcher) have(sc *lookup.Scene) int64 {
	if id := m.ids[sc.Endpoint+"|"+sc.ID]; id > 0 {
		return id
	}
	if t := store.NormTitle(sc.Title); len(t) >= 10 {
		return m.titles[t]
	}
	return 0
}

func (s *Server) annotate(ctx context.Context, uid int64, list []lookup.Scene) []discoverItem {
	m := s.newMatcher(ctx)
	wanted, _ := s.store.WantedKeys(ctx, uid)
	out := make([]discoverItem, 0, len(list))
	for _, sc := range list {
		out = append(out, discoverItem{Scene: sc, HaveID: m.have(&sc), WantedID: wanted[sc.Endpoint+"|"+sc.ID]})
	}
	return out
}

// handleDiscover lists a performer's or studio's scenes with what you have.
func (s *Server) handleDiscover(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		u := userFrom(ctx)
		st, _ := s.store.GetSettings(ctx, u.ID)
		source, err := pickSource(st, r.URL.Query().Get("source"))
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"problem": err.Error(), "sources": []string{}, "items": []any{}})
			return
		}
		remoteID, _, err := s.remoteFor(ctx, st, source, kind, idParam(r, "id"))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.fail(w, err, "discover")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"problem": err.Error(), "source": source, "sources": sources(st), "items": []any{}})
			return
		}
		page := max(queryInt(r, "page"), 1)
		p, err := s.remoteScenes(ctx, st, source, kind, remoteID, page, queryBool(r, "fresh"))
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"problem": err.Error(), "source": source, "sources": sources(st), "items": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"source": source, "sources": sources(st), "remote_id": remoteID, "total": p.Total,
			"page": page, "per": discoverPer, "items": s.annotate(ctx, u.ID, p.Scenes)})
	}
}

// ---------- wishlist ----------

func (s *Server) handleListWanted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	list, err := s.store.ListWanted(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "wanted")
		return
	}
	// files that arrived by title (not identified yet) count as found too
	m := s.newMatcher(ctx)
	type item struct {
		store.Wanted
		HaveID int64 `json:"have_id"`
	}
	out := []item{}
	for _, x := range list {
		it := item{Wanted: x}
		var sc lookup.Scene
		if json.Unmarshal(x.Scene, &sc) == nil {
			it.HaveID = m.have(&sc)
		}
		if x.FoundSceneID != nil {
			it.HaveID = *x.FoundSceneID
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddWanted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Scene lookup.Scene `json:"scene"`
	}
	if err := decode(r, &in); err != nil || in.Scene.ID == "" || in.Scene.Endpoint == "" {
		writeErr(w, http.StatusBadRequest, "choose a scene")
		return
	}
	b, _ := json.Marshal(in.Scene)
	x := &store.Wanted{Endpoint: in.Scene.Endpoint, RemoteID: in.Scene.ID, Title: oneLine(in.Scene.Title, 300), ReleaseDate: in.Scene.Date, Scene: b}
	if err := s.store.AddWanted(ctx, userFrom(ctx).ID, x); err != nil {
		s.fail(w, err, "wanted")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": x.ID})
}

func (s *Server) handleDeleteWanted(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteWanted(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "wanted")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- following ----------

func (s *Server) decorateFollows(list []store.Follow) {
	for i := range list {
		if list[i].Image != "" {
			list[i].Image = fmt.Sprintf("/api/v1/%ss/%d/image?v=%s", list[i].Kind, list[i].ItemID, coverVersion(list[i].Image))
		}
	}
}

func (s *Server) handleListFollows(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListFollows(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err, "follows")
		return
	}
	s.decorateFollows(list)
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddFollow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Kind   string `json:"kind"`
		ItemID int64  `json:"item_id"`
	}
	if err := decode(r, &in); err != nil || (in.Kind != "performer" && in.Kind != "studio") || in.ItemID <= 0 {
		writeErr(w, http.StatusBadRequest, "follow a performer or a studio")
		return
	}
	table := map[string]string{"performer": "performers", "studio": "studios"}[in.Kind]
	var n int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+table+` WHERE id = ?`, in.ItemID).Scan(&n)
	if n == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	id, err := s.store.AddFollow(ctx, u.ID, in.Kind, in.ItemID)
	if err != nil {
		s.fail(w, err, "follow")
		return
	}
	// the first look only remembers what is out there already
	go func() {
		if f, err := s.store.GetFollow(context.WithoutCancel(ctx), u.ID, id); err == nil {
			s.checkFollow(context.WithoutCancel(ctx), f)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) handleDeleteFollow(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFollow(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "follow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckFollow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, err := s.store.GetFollow(ctx, userFrom(ctx).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "follow")
		return
	}
	n, err := s.checkFollow(ctx, f)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"new": 0, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"new": n})
}

// handleFollowState says whether the person follows a performer/studio.
func (s *Server) handleFollowState(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]int64{"follow_id": s.store.FollowOf(r.Context(), userFrom(r.Context()).ID, kind, idParam(r, "id"))})
	}
}

// followChecker looks at every follow about once a day.
func (s *Server) followChecker(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		list, _ := s.store.ListFollows(ctx, 0)
		for i := range list {
			f := &list[i]
			if last, err := time.Parse(time.RFC3339Nano, f.LastCheckedAt); err == nil && time.Since(last) < 23*time.Hour {
				continue
			}
			_, _ = s.checkFollow(ctx, f)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second): // be polite to the lookup sites
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// checkFollow looks for a follow's new scenes. The first look only
// remembers what exists; later ones keep (and tell about) what is new.
func (s *Server) checkFollow(ctx context.Context, f *store.Follow) (int, error) {
	st, _ := s.store.GetSettings(ctx, f.UserID)
	source, err := pickSource(st, "")
	if err == nil {
		var remoteID string
		if remoteID, _, err = s.remoteFor(ctx, st, source, f.Kind, f.ItemID); err == nil {
			var p *lookup.ScenePage
			if p, err = s.remoteScenes(ctx, st, source, f.Kind, remoteID, 1, true); err == nil {
				n, err := s.takeNew(ctx, f, p.Scenes)
				msg := ""
				if err != nil {
					msg = err.Error()
				}
				_ = s.store.FollowChecked(ctx, f.ID, msg)
				return n, err
			}
		}
	}
	_ = s.store.FollowChecked(ctx, f.ID, err.Error())
	return 0, err
}

func (s *Server) takeNew(ctx context.Context, f *store.Follow, list []lookup.Scene) (int, error) {
	seen, err := s.store.FollowSeen(ctx, f.ID)
	if err != nil {
		return 0, err
	}
	first := f.LastCheckedAt == ""
	m := s.newMatcher(ctx)
	var mark []string
	var fresh []string
	for i := range list {
		sc := &list[i]
		key := sc.Endpoint + "|" + sc.ID
		if seen[key] {
			continue
		}
		mark = append(mark, key)
		if first || m.have(sc) > 0 {
			continue
		}
		b, _ := json.Marshal(sc)
		if added, err := s.store.AddRelease(ctx, f.UserID, f.ID, &store.Release{Endpoint: sc.Endpoint, RemoteID: sc.ID, ReleaseDate: sc.Date, Scene: b}); err == nil && added {
			fresh = append(fresh, sc.Title)
		}
	}
	if err := s.store.MarkFollowSeen(ctx, f.ID, mark); err != nil {
		return 0, err
	}
	if len(fresh) > 0 {
		s.notifyReleases(ctx, f, fresh)
	}
	return len(fresh), nil
}

func (s *Server) notifyReleases(ctx context.Context, f *store.Follow, titles []string) {
	st, err := s.store.GetSettings(ctx, f.UserID)
	if err != nil || st.NotifyURL == "" {
		return
	}
	n := len(titles)
	title := fmt.Sprintf("%d new %s", n, map[bool]string{true: "scene", false: "scenes"}[n == 1])
	body := "Someone you follow has new scenes."
	if !st.NotifyQuiet {
		body = fmt.Sprintf("%s: %s", f.Name, strings.Join(titles[:min(n, 3)], " · "))
	}
	link := ""
	if s.cfg.BaseURL != "" {
		link = s.cfg.BaseURL + "/discover"
	}
	if err := notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"sparkles"}, Link: link}); err != nil {
		s.log.Debug("notify failed", "err", err)
	}
}

// ---------- new scenes ----------

func (s *Server) handleListReleases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	list, err := s.store.ListReleases(ctx, u.ID, queryBool(r, "all"))
	if err != nil {
		s.fail(w, err, "releases")
		return
	}
	m := s.newMatcher(ctx)
	wanted, _ := s.store.WantedKeys(ctx, u.ID)
	out := []discoverItem{}
	for _, x := range list {
		var sc lookup.Scene
		if json.Unmarshal(x.Scene, &sc) != nil {
			continue
		}
		out = append(out, discoverItem{Scene: sc, HaveID: m.have(&sc), WantedID: wanted[sc.Endpoint+"|"+sc.ID], Release: x.ID})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDismissRelease(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DismissReleases(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "dismiss")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
