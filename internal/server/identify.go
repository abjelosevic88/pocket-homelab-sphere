package server

import (
	"context"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/lookup"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// Identify: what a scene is, found on StashDB (or another stash-box) by the
// file's fingerprints or by words, and on ThePornDB by its hash or name.
// Every person uses their own keys. Applying a match fills the scene and
// creates the studio, performers (with their pictures) and tags it needs.

func (s *Server) boxes(st store.Settings) (*lookup.StashBox, *lookup.TPDB) {
	var sb *lookup.StashBox
	var tp *lookup.TPDB
	if st.StashDBKey != "" {
		sb = &lookup.StashBox{Endpoint: firstNonEmpty(st.StashBoxURL, lookup.StashDB), Key: st.StashDBKey, UA: s.cfg.UserAgent}
	}
	if st.TPDBKey != "" {
		tp = &lookup.TPDB{Key: st.TPDBKey, UA: s.cfg.UserAgent}
	}
	return sb, tp
}

func realPHash(h string) string {
	if h == "-" {
		return ""
	}
	return h
}

// candidates looks a scene up everywhere the person has a key for.
func (s *Server) candidates(ctx context.Context, uid int64, sc *store.Scene, query string, fingerprintsOnly bool) ([]lookup.Scene, []string) {
	st, _ := s.store.GetSettings(ctx, uid)
	sb, tp := s.boxes(st)
	out := []lookup.Scene{}
	var problems []string
	if sb == nil && tp == nil {
		return out, []string{"Add a StashDB or ThePornDB key under Settings → Lookups to find scenes."}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if sb != nil {
		if res, err := sb.ByFingerprints(ctx, sc.OSHash, realPHash(sc.PHash)); err == nil {
			out = append(out, res...)
		} else {
			problems = append(problems, err.Error())
		}
		if !fingerprintsOnly && strings.TrimSpace(query) != "" {
			if res, err := sb.Search(ctx, query); err == nil {
				out = append(out, res...)
			} else {
				problems = append(problems, err.Error())
			}
		}
	}
	if tp != nil {
		parse := ""
		if !fingerprintsOnly {
			parse = firstNonEmpty(strings.TrimSpace(query), strings.TrimSuffix(path.Base(sc.Path), path.Ext(sc.Path)))
		}
		if res, err := tp.Search(ctx, parse, sc.OSHash); err == nil {
			out = append(out, res...)
		} else {
			problems = append(problems, err.Error())
		}
	}
	// the same scene found twice: keep the first (fingerprint matches come first)
	seen := map[string]bool{}
	uniq := out[:0]
	for _, c := range out {
		k := c.Source + ":" + c.ID
		if !seen[k] {
			seen[k] = true
			uniq = append(uniq, c)
		}
	}
	return uniq, problems
}

func (s *Server) handleIdentify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query().Get("q")
	list, problems := s.candidates(ctx, userFrom(ctx).ID, sc, q, false)
	writeJSON(w, http.StatusOK, map[string]any{"results": list, "problems": problems})
}

// applyFields says which parts of a match to take.
type applyFields struct {
	Title      bool `json:"title"`
	Date       bool `json:"date"`
	Details    bool `json:"details"`
	Code       bool `json:"code"`
	Director   bool `json:"director"`
	URLs       bool `json:"urls"`
	Studio     bool `json:"studio"`
	Performers bool `json:"performers"`
	Tags       bool `json:"tags"`
	Cover      bool `json:"cover"`
	Organized  bool `json:"organized"` // mark the scene as done
}

func allFields() applyFields {
	return applyFields{true, true, true, true, true, true, true, true, true, true, true}
}

func (s *Server) handleApplyIdentify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, ok := s.sceneFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Result lookup.Scene `json:"result"`
		Fields *applyFields `json:"fields"`
	}
	if err := decode(r, &in); err != nil || in.Result.ID == "" {
		writeErr(w, http.StatusBadRequest, "choose a match")
		return
	}
	f := allFields()
	if in.Fields != nil {
		f = *in.Fields
	}
	if err := s.applyMatch(ctx, userFrom(ctx), sc, &in.Result, f); err != nil {
		s.fail(w, err, "apply")
		return
	}
	s.handleGetScene(w, r)
}

// applyMatch fills a scene from a lookup result.
func (s *Server) applyMatch(ctx context.Context, u *store.User, sc *store.Scene, m *lookup.Scene, f applyFields) error {
	p := store.ScenePatch{}
	set := func(on bool, dst **string, v string) {
		if on && strings.TrimSpace(v) != "" {
			*dst = strp(v)
		}
	}
	set(f.Title, &p.Title, m.Title)
	if f.Date && validLooseDate(m.Date) {
		set(true, &p.Date, m.Date)
	}
	set(f.Details, &p.Details, m.Details)
	set(f.Code, &p.Code, m.Code)
	set(f.Director, &p.Director, m.Director)
	if f.URLs && len(m.URLs) > 0 {
		urls := append(append([]string{}, sc.URLs...), m.URLs...)
		p.URLs = &urls
	}
	ids := map[string]string{}
	for k, v := range sc.StashIDs {
		ids[k] = v
	}
	ids[m.Endpoint] = m.ID
	p.StashIDs = &ids
	if f.Organized {
		p.Organized = boolp(true)
	}
	if f.Studio && m.Studio != nil && m.Studio.Name != "" {
		sid, err := s.ensureStudioFrom(ctx, u, m.Studio, m.Endpoint)
		if err == nil {
			ps := &sid
			p.StudioID = &ps
		}
	}
	if err := s.store.UpdateScene(ctx, sc.ID, p); err != nil {
		return err
	}
	var perfs, tags []int64
	if f.Performers {
		for i := range m.Performers {
			if id, err := s.ensurePerformerFrom(ctx, u, &m.Performers[i], m.Endpoint); err == nil {
				perfs = append(perfs, id)
			}
		}
	}
	if f.Tags {
		for _, t := range m.Tags {
			if id, _, err := s.store.EnsureTag(ctx, t); err == nil {
				tags = append(tags, id)
			}
		}
	}
	if err := s.store.AddSceneRelations(ctx, sc.ID, perfs, tags); err != nil {
		return err
	}
	if f.Cover && m.Image != "" && (sc.CoverSource == "" || sc.CoverSource == "frame" || sc.CoverSource == "stashdb") {
		if b, err := lookup.Fetch(ctx, m.Image, s.cfg.UserAgent); err == nil {
			_ = s.setSceneCover(ctx, sc, b, "stashdb")
		}
	}
	return nil
}

func (s *Server) ensureStudioFrom(ctx context.Context, u *store.User, ls *lookup.Studio, endpoint string) (int64, error) {
	id, created, err := s.store.EnsureStudio(ctx, ls.Name)
	if err != nil {
		return 0, err
	}
	st, err := s.store.GetStudio(ctx, 0, id)
	if err != nil {
		return id, nil
	}
	changed := false
	if st.StashIDs[endpoint] == "" && ls.ID != "" {
		st.StashIDs[endpoint] = ls.ID
		changed = true
	}
	if st.URL == "" && ls.URL != "" {
		st.URL = ls.URL
		changed = true
	}
	if st.ParentID == nil && ls.Parent != "" && !strings.EqualFold(ls.Parent, ls.Name) {
		if pid, _, err := s.store.EnsureStudio(ctx, ls.Parent); err == nil && pid != id {
			st.ParentID = &pid
			changed = true
		}
	}
	if changed {
		_ = s.store.SaveStudio(ctx, st)
	}
	if (created || st.ImagePath == "") && ls.Image != "" {
		if b, err := lookup.Fetch(ctx, ls.Image, s.cfg.UserAgent); err == nil {
			_ = s.setImage(ctx, "studios", id, b)
		}
	}
	return id, nil
}

func (s *Server) ensurePerformerFrom(ctx context.Context, u *store.User, lp *lookup.Performer, endpoint string) (int64, error) {
	// a performer already known by this stash id wins over name matches
	if lp.ID != "" {
		var id int64
		if s.store.DB.QueryRowContext(ctx, `SELECT id FROM performers WHERE json_extract(stash_ids, '$."' || ? || '"') = ?`, endpoint, lp.ID).Scan(&id) == nil {
			return id, nil
		}
	}
	id, created, err := s.store.EnsurePerformer(ctx, lp.Name)
	if err != nil {
		return 0, err
	}
	p, err := s.store.GetPerformer(ctx, 0, id)
	if err != nil {
		return id, nil
	}
	fillPerformer(p, lp, created)
	if lp.ID != "" && p.StashIDs[endpoint] == "" {
		p.StashIDs[endpoint] = lp.ID
	}
	_ = s.store.SavePerformer(ctx, p)
	if p.ImagePath == "" && lp.Image != "" {
		if b, err := lookup.Fetch(ctx, lp.Image, s.cfg.UserAgent); err == nil {
			_ = s.setImage(ctx, "performers", id, b)
		}
	}
	return id, nil
}

// fillPerformer copies facts from a lookup; all of them when overwrite,
// otherwise only the empty ones.
func fillPerformer(p *store.Performer, lp *lookup.Performer, overwrite bool) {
	str := func(dst *string, v string) {
		if v != "" && (overwrite || *dst == "") {
			*dst = v
		}
	}
	str(&p.Disambiguation, lp.Disambiguation)
	str(&p.Gender, lp.Gender)
	if validLooseDate(lp.Birthdate) {
		str(&p.Birthdate, lp.Birthdate)
	}
	if validLooseDate(lp.DeathDate) {
		str(&p.DeathDate, lp.DeathDate)
	}
	str(&p.Country, lp.Country)
	str(&p.Ethnicity, lp.Ethnicity)
	str(&p.HairColor, lp.HairColor)
	str(&p.EyeColor, lp.EyeColor)
	str(&p.Measurements, lp.Measurements)
	str(&p.FakeTits, lp.FakeTits)
	str(&p.Career, lp.Career)
	str(&p.Tattoos, lp.Tattoos)
	str(&p.Piercings, lp.Piercings)
	if lp.HeightCM > 0 && (overwrite || p.HeightCM == 0) {
		p.HeightCM = lp.HeightCM
	}
	if len(lp.Aliases) > 0 && (overwrite || p.Aliases == "") {
		p.Aliases = store.JoinList(p.Aliases + ", " + strings.Join(lp.Aliases, ", "))
	}
	if lp.As != "" && !strings.EqualFold(lp.As, p.Name) {
		p.Aliases = store.JoinList(p.Aliases + ", " + lp.As)
	}
	if len(lp.URLs) > 0 && (overwrite || len(p.URLs) == 0) {
		p.URLs = append(p.URLs, lp.URLs...)
	}
}

// autoIdentify fills a new scene when its fingerprint matches exactly one
// scene (the person who added it — or the owner — has it switched on).
func (s *Server) autoIdentify(ctx context.Context, sceneID int64) {
	sc, err := s.store.GetScene(ctx, 0, sceneID)
	if err != nil || sc.Organized || len(sc.StashIDs) > 0 {
		return
	}
	uid := s.store.OwnerID(ctx)
	if sc.AddedBy != nil {
		uid = *sc.AddedBy
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if !st.AutoIdentify || (st.StashDBKey == "" && st.TPDBKey == "") {
		return
	}
	s.identifyOne(ctx, uid, sc)
}

func (s *Server) identifyOne(ctx context.Context, uid int64, sc *store.Scene) bool {
	u, err := s.store.GetUser(ctx, uid)
	if err != nil {
		return false
	}
	list, _ := s.candidates(ctx, uid, sc, "", true)
	var exact []lookup.Scene
	for _, c := range list {
		if c.Matched == "OSHASH" {
			exact = append(exact, c)
		}
	}
	if len(exact) == 0 {
		exact = list
	}
	if len(exact) != 1 {
		return false
	}
	f := allFields()
	f.Organized = false
	if err := s.applyMatch(ctx, u, sc, &exact[0], f); err != nil {
		s.log.Debug("identify failed", "scene", sc.ID, "err", err)
		return false
	}
	s.log.Info("scene identified", "scene", sc.ID, "source", exact[0].Source)
	return true
}

func (s *Server) identifyMany(ctx context.Context, uid int64, ids []int64) {
	for _, id := range ids {
		sc, err := s.store.GetScene(ctx, uid, id)
		if err != nil || sc.TrashedAt != "" {
			continue
		}
		s.identifyOne(ctx, uid, sc)
		select {
		case <-ctx.Done():
			return
		case <-time.After(1500 * time.Millisecond): // be polite to the lookup sites
		}
	}
}

// ---------- performers ----------

func (s *Server) handlePerformerLookup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, http.StatusBadRequest, "type a name")
		return
	}
	st, _ := s.store.GetSettings(ctx, userFrom(ctx).ID)
	sb, tp := s.boxes(st)
	out := []lookup.Performer{}
	var problems []string
	if sb == nil && tp == nil {
		problems = append(problems, "Add a StashDB or ThePornDB key under Settings → Lookups to find performers.")
	}
	if sb != nil {
		if res, err := sb.SearchPerformer(ctx, q); err == nil {
			out = append(out, res...)
		} else {
			problems = append(problems, err.Error())
		}
	}
	if tp != nil {
		if res, err := tp.SearchPerformer(ctx, q); err == nil {
			out = append(out, res...)
		} else {
			problems = append(problems, err.Error())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out, "problems": problems})
}

func (s *Server) handlePerformerApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.store.GetPerformer(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "performer")
		return
	}
	var in struct {
		Result    lookup.Performer `json:"result"`
		Overwrite bool             `json:"overwrite"`
		Image     bool             `json:"image"`
	}
	if err := decode(r, &in); err != nil || in.Result.Name == "" {
		writeErr(w, http.StatusBadRequest, "choose a match")
		return
	}
	fillPerformer(p, &in.Result, in.Overwrite)
	endpoint := lookup.StashDB
	if in.Result.Source == "tpdb" {
		endpoint = "https://theporndb.net"
	} else if st, _ := s.store.GetSettings(ctx, userFrom(ctx).ID); st.StashBoxURL != "" {
		endpoint = st.StashBoxURL
	}
	if in.Result.ID != "" {
		p.StashIDs[endpoint] = in.Result.ID
	}
	if err := s.store.SavePerformer(ctx, p); err != nil {
		s.fail(w, err, "save")
		return
	}
	if (in.Image || p.ImagePath == "") && in.Result.Image != "" {
		if b, err := lookup.Fetch(ctx, in.Result.Image, s.cfg.UserAgent); err == nil {
			_ = s.setImage(ctx, "performers", p.ID, b)
		}
	}
	s.handleGetPerformer(w, r)
}
