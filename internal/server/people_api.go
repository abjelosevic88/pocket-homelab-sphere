package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/lookup"
	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// Performers, studios, tags and groups are shared by everyone on the box;
// favourites and ratings of them are each person's own.

func (s *Server) decoratePerformers(list []store.Performer) {
	for i := range list {
		s.decoratePerformer(&list[i])
	}
}

func (s *Server) decoratePerformer(p *store.Performer) {
	if p.ImagePath != "" {
		p.Image = fmt.Sprintf("/api/v1/performers/%d/image?v=%s", p.ID, coverVersion(p.ImagePath))
	}
}

func (s *Server) decorateStudio(st *store.Studio) {
	if st.ImagePath != "" {
		st.Image = fmt.Sprintf("/api/v1/studios/%d/image?v=%s", st.ID, coverVersion(st.ImagePath))
	}
}

func (s *Server) decorateTag(t *store.Tag) {
	if t.ImagePath != "" {
		t.Image = fmt.Sprintf("/api/v1/tags/%d/image?v=%s", t.ID, coverVersion(t.ImagePath))
	}
}

func (s *Server) decorateGroup(g *store.Group) {
	if g.FrontPath != "" {
		g.Front = fmt.Sprintf("/api/v1/groups/%d/front?v=%s", g.ID, coverVersion(g.FrontPath))
	}
	if g.BackPath != "" {
		g.Back = fmt.Sprintf("/api/v1/groups/%d/back?v=%s", g.ID, coverVersion(g.BackPath))
	}
}

// markInput is a favourite and/or rating sent with an edit.
type markInput struct {
	Favorite *bool `json:"favorite"`
	Rating   *int  `json:"rating"`
}

func (s *Server) handleMark(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in markInput
		if err := decode(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		if in.Rating != nil && (*in.Rating < 0 || *in.Rating > 10) {
			writeErr(w, http.StatusBadRequest, "rating is 0 to 10")
			return
		}
		if err := s.store.SetMark(r.Context(), userFrom(r.Context()).ID, kind, idParam(r, "id"), in.Favorite, in.Rating); err != nil {
			s.fail(w, err, "save")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// pictureFrom reads a picture from the request: raw bytes, or {"url": …}.
func (s *Server) pictureFrom(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var in struct {
			URL string `json:"url"`
		}
		if err := decode(r, &in); err != nil || in.URL == "" {
			return nil, fmt.Errorf("send a picture or a link to one")
		}
		return s.fetchPicture(r.Context(), userFrom(r.Context()), in.URL)
	}
	return io.ReadAll(http.MaxBytesReader(w, r.Body, 25<<20))
}

// fetchPicture downloads a picture from a link (local addresses for admins only).
func (s *Server) fetchPicture(ctx context.Context, u *store.User, link string) ([]byte, error) {
	if err := s.publicURL(ctx, u, link); err != nil {
		return nil, err
	}
	return lookup.Fetch(ctx, link, s.cfg.UserAgent)
}

// ---------- performers ----------

func (s *Server) handleListPerformers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	per := queryInt(r, "per")
	if per <= 0 || per > 500 {
		per = 60
	}
	f := store.PerformerFilter{UserID: userFrom(r.Context()).ID, Query: q.Get("q"), Gender: q.Get("gender"), Favorite: queryBool(r, "favorite"),
		Sort: q.Get("sort"), Desc: q.Get("dir") == "desc", Limit: per}
	if f.Sort == "" {
		f.Sort, f.Desc = "name", false
	}
	if p := queryInt(r, "page"); p > 1 {
		f.Offset = (p - 1) * per
	}
	list, total, err := s.store.ListPerformers(r.Context(), f)
	if err != nil {
		s.fail(w, err, "performers")
		return
	}
	s.decoratePerformers(list)
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total})
}

func (s *Server) handleGetPerformer(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPerformer(r.Context(), userFrom(r.Context()).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "performer")
		return
	}
	s.decoratePerformer(p)
	writeJSON(w, http.StatusOK, p)
}

type performerInput struct {
	store.Performer
	ImageURL  string `json:"image_url"`
	MFavorite *bool  `json:"favorite"`
	MRating   *int   `json:"rating"`
}

func (s *Server) handleSavePerformer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in performerInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := in.Performer
	p.ID = idParam(r, "id")
	if strings.TrimSpace(p.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a performer needs a name")
		return
	}
	if !validLooseDate(p.Birthdate) || !validLooseDate(p.DeathDate) {
		writeErr(w, http.StatusBadRequest, "dates look like 1995-04-30")
		return
	}
	if p.ID > 0 {
		cur, err := s.store.GetPerformer(ctx, 0, p.ID)
		if err != nil {
			s.fail(w, err, "performer")
			return
		}
		if p.StashIDs == nil {
			p.StashIDs = cur.StashIDs
		}
	}
	if err := s.store.SavePerformer(ctx, &p); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "there is already a performer with that name — add a disambiguation to tell them apart")
			return
		}
		s.fail(w, err, "save performer")
		return
	}
	if in.ImageURL != "" {
		if b, err := s.fetchPicture(ctx, userFrom(ctx), in.ImageURL); err == nil {
			_ = s.setImage(ctx, "performers", p.ID, b)
		}
	}
	if in.MFavorite != nil || in.MRating != nil {
		_ = s.store.SetMark(ctx, userFrom(ctx).ID, "performer", p.ID, in.MFavorite, in.MRating)
	}
	out, _ := s.store.GetPerformer(ctx, userFrom(ctx).ID, p.ID)
	s.decoratePerformer(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeletePerformer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.store.GetPerformer(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "performer")
		return
	}
	if err := s.store.DeletePerformer(ctx, p.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	s.removeFile(p.ImagePath)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMergePerformers(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Sources []int64 `json:"sources"`
	}
	if err := decode(r, &in); err != nil || len(in.Sources) == 0 {
		writeErr(w, http.StatusBadRequest, "choose who to merge")
		return
	}
	if err := s.store.MergePerformers(r.Context(), idParam(r, "id"), in.Sources); err != nil {
		s.fail(w, err, "merge")
		return
	}
	s.handleGetPerformer(w, r)
}

// ---------- pictures of performers, studios, tags, groups ----------

// setImage stores a picture for a performer, studio or tag.
func (s *Server) setImage(ctx context.Context, table string, id int64, b []byte) error {
	rel, err := s.savePicture(table, b, 1200)
	if err != nil {
		return err
	}
	var old string
	_ = s.store.DB.QueryRowContext(ctx, `SELECT image_path FROM `+table+` WHERE id = ?`, id).Scan(&old)
	switch table {
	case "performers":
		err = s.store.SetPerformerImage(ctx, id, rel)
	case "studios":
		err = s.store.SetStudioImage(ctx, id, rel)
	case "tags":
		err = s.store.SetTagImage(ctx, id, rel)
	}
	if err != nil {
		s.removeFile(rel)
		return err
	}
	if old != "" && old != rel {
		s.removeFile(old)
	}
	return nil
}

func (s *Server) handleImage(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var rel string
		_ = s.store.DB.QueryRowContext(r.Context(), `SELECT image_path FROM `+table+` WHERE id = ?`, idParam(r, "id")).Scan(&rel)
		s.servePicture(w, r, rel)
	}
}

func (s *Server) handleSetImage(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := s.pictureFrom(w, r)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		var exists int
		_ = s.store.DB.QueryRowContext(r.Context(), `SELECT COUNT(1) FROM `+table+` WHERE id = ?`, idParam(r, "id")).Scan(&exists)
		if exists == 0 {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if err := s.setImage(r.Context(), table, idParam(r, "id"), b); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleDeleteImage(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := idParam(r, "id")
		var old string
		_ = s.store.DB.QueryRowContext(ctx, `SELECT image_path FROM `+table+` WHERE id = ?`, id).Scan(&old)
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE `+table+` SET image_path = '' WHERE id = ?`, id)
		s.removeFile(old)
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------- studios ----------

func (s *Server) handleListStudios(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListStudios(r.Context(), userFrom(r.Context()).ID, r.URL.Query().Get("q"))
	if err != nil {
		s.fail(w, err, "studios")
		return
	}
	for i := range list {
		s.decorateStudio(&list[i])
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetStudio(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetStudio(r.Context(), userFrom(r.Context()).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "studio")
		return
	}
	s.decorateStudio(st)
	for i := range st.Children {
		if st.Children[i].Image != "" {
			st.Children[i].Image = fmt.Sprintf("/api/v1/studios/%d/image?v=%s", st.Children[i].ID, coverVersion(st.Children[i].Image))
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleSaveStudio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		store.Studio
		ImageURL  string `json:"image_url"`
		MFavorite *bool  `json:"favorite"`
		MRating   *int   `json:"rating"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st := in.Studio
	st.ID = idParam(r, "id")
	if strings.TrimSpace(st.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a studio needs a name")
		return
	}
	if st.ParentID != nil && *st.ParentID == 0 {
		st.ParentID = nil
	}
	if st.ID > 0 && st.StashIDs == nil {
		if cur, err := s.store.GetStudio(ctx, 0, st.ID); err == nil {
			st.StashIDs = cur.StashIDs
		}
	}
	if err := s.store.SaveStudio(ctx, &st); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "there is already a studio with that name")
			return
		}
		s.fail(w, err, "save studio")
		return
	}
	if in.ImageURL != "" {
		if b, err := s.fetchPicture(ctx, userFrom(ctx), in.ImageURL); err == nil {
			_ = s.setImage(ctx, "studios", st.ID, b)
		}
	}
	if in.MFavorite != nil || in.MRating != nil {
		_ = s.store.SetMark(ctx, userFrom(ctx).ID, "studio", st.ID, in.MFavorite, in.MRating)
	}
	out, _ := s.store.GetStudio(ctx, userFrom(ctx).ID, st.ID)
	s.decorateStudio(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteStudio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, err := s.store.GetStudio(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "studio")
		return
	}
	if err := s.store.DeleteStudio(ctx, st.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	s.removeFile(st.ImagePath)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- tags ----------

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTags(r.Context(), userFrom(r.Context()).ID, r.URL.Query().Get("q"))
	if err != nil {
		s.fail(w, err, "tags")
		return
	}
	for i := range list {
		s.decorateTag(&list[i])
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetTag(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTag(r.Context(), userFrom(r.Context()).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "tag")
		return
	}
	s.decorateTag(t)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleSaveTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		store.Tag
		MFavorite *bool `json:"favorite"`
		MRating   *int  `json:"rating"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t := in.Tag
	t.ID = idParam(r, "id")
	if strings.TrimSpace(t.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a tag needs a name")
		return
	}
	if t.ParentID != nil && *t.ParentID == 0 {
		t.ParentID = nil
	}
	if err := s.store.SaveTag(ctx, &t); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "there is already a tag with that name")
			return
		}
		s.fail(w, err, "save tag")
		return
	}
	if in.MFavorite != nil {
		_ = s.store.SetMark(ctx, userFrom(ctx).ID, "tag", t.ID, in.MFavorite, nil)
	}
	out, _ := s.store.GetTag(ctx, userFrom(ctx).ID, t.ID)
	s.decorateTag(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.store.GetTag(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "tag")
		return
	}
	if err := s.store.DeleteTag(ctx, t.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	s.removeFile(t.ImagePath)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMergeTags(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Sources []int64 `json:"sources"`
	}
	if err := decode(r, &in); err != nil || len(in.Sources) == 0 {
		writeErr(w, http.StatusBadRequest, "choose the tags to merge")
		return
	}
	if err := s.store.MergeTags(r.Context(), idParam(r, "id"), in.Sources); err != nil {
		s.fail(w, err, "merge")
		return
	}
	s.handleGetTag(w, r)
}

// ---------- groups ----------

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListGroups(r.Context(), userFrom(r.Context()).ID, r.URL.Query().Get("q"))
	if err != nil {
		s.fail(w, err, "groups")
		return
	}
	for i := range list {
		s.decorateGroup(&list[i])
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	g, err := s.store.GetGroup(r.Context(), userFrom(r.Context()).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "group")
		return
	}
	s.decorateGroup(g)
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleSaveGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		store.Group
		Scenes    []int64 `json:"scene_ids"` // in order; nil = unchanged
		MFavorite *bool   `json:"favorite"`
		MRating   *int    `json:"rating"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	g := in.Group
	g.ID = idParam(r, "id")
	if strings.TrimSpace(g.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a group needs a name")
		return
	}
	if g.StudioID != nil && *g.StudioID == 0 {
		g.StudioID = nil
	}
	if !validLooseDate(g.Date) {
		writeErr(w, http.StatusBadRequest, "the date should look like 2024-05-31")
		return
	}
	if err := s.store.SaveGroup(ctx, &g); err != nil {
		s.fail(w, err, "save group")
		return
	}
	if in.Scenes != nil {
		tx, err := s.store.DB.BeginTx(ctx, nil)
		if err == nil {
			_, _ = tx.ExecContext(ctx, `DELETE FROM group_scenes WHERE group_id = ?`, g.ID)
			for i, sid := range in.Scenes {
				_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO group_scenes (group_id, scene_id, scene_index) SELECT ?, id, ? FROM scenes WHERE id = ?`, g.ID, i+1, sid)
			}
			_ = tx.Commit()
		}
	}
	if in.MFavorite != nil || in.MRating != nil {
		_ = s.store.SetMark(ctx, userFrom(ctx).ID, "group", g.ID, in.MFavorite, in.MRating)
	}
	out, _ := s.store.GetGroup(ctx, userFrom(ctx).ID, g.ID)
	s.decorateGroup(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.store.GetGroup(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "group")
		return
	}
	if err := s.store.DeleteGroup(ctx, g.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	s.removeFile(g.FrontPath)
	s.removeFile(g.BackPath)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGroupImage(back bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		col := "front_path"
		if back {
			col = "back_path"
		}
		var rel string
		_ = s.store.DB.QueryRowContext(r.Context(), `SELECT `+col+` FROM groups_ WHERE id = ?`, idParam(r, "id")).Scan(&rel)
		s.servePicture(w, r, rel)
	}
}

func (s *Server) handleSetGroupImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.store.GetGroup(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "group")
		return
	}
	back := r.URL.Query().Get("side") == "back"
	if r.Method == http.MethodDelete {
		old := g.FrontPath
		if back {
			old = g.BackPath
		}
		_ = s.store.SetGroupImage(ctx, g.ID, back, "")
		s.removeFile(old)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	b, err := s.pictureFrom(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rel, err := s.savePicture("groups", b, 1600)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	old := g.FrontPath
	if back {
		old = g.BackPath
	}
	if err := s.store.SetGroupImage(ctx, g.ID, back, rel); err != nil {
		s.fail(w, err, "save")
		return
	}
	s.removeFile(old)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- auto-tag from file names ----------

// handleAutoTag puts performers, studios and tags on scenes whose file path
// mentions them by name or alias ("Jane.Doe" finds Jane Doe).
func (s *Server) handleAutoTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Performers bool    `json:"performers"`
		Studios    bool    `json:"studios"`
		Tags       bool    `json:"tags"`
		IDs        []int64 `json:"ids"` // only these scenes ([] = all)
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n, err := s.autoTag(ctx, in.Performers, in.Studios, in.Tags, in.IDs)
	if err != nil {
		s.fail(w, err, "auto tag")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"changed": n})
}

// squash makes a name or path comparable: lower-case words joined by single spaces.
func squash(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return " " + strings.TrimSpace(b.String()) + " "
}

func (s *Server) autoTag(ctx context.Context, perfs, studios, tags bool, only []int64) (int, error) {
	type name struct {
		id   int64
		text string
	}
	load := func(table string) ([]name, error) {
		idx, err := s.store.NameIndex(ctx, table)
		if err != nil {
			return nil, err
		}
		var out []name
		for n, id := range idx {
			if len([]rune(n)) >= 4 { // too short names match everything
				out = append(out, name{id, squash(n)})
			}
		}
		return out, nil
	}
	var pn, sn, tn []name
	var err error
	if perfs {
		if pn, err = load("performers"); err != nil {
			return 0, err
		}
	}
	if studios {
		if sn, err = load("studios"); err != nil {
			return 0, err
		}
	}
	if tags {
		if tn, err = load("tags"); err != nil {
			return 0, err
		}
	}
	idx, err := s.store.AllSceneFiles(ctx)
	if err != nil {
		return 0, err
	}
	want := map[int64]bool{}
	for _, id := range only {
		want[id] = true
	}
	changed := 0
	for _, x := range idx {
		if x.Trashed || (len(want) > 0 && !want[x.ID]) {
			continue
		}
		hay := squash(x.Path)
		var addP, addT []int64
		for _, n := range pn {
			if strings.Contains(hay, n.text) {
				addP = append(addP, n.id)
			}
		}
		for _, n := range tn {
			if strings.Contains(hay, n.text) {
				addT = append(addT, n.id)
			}
		}
		if len(addP) > 0 || len(addT) > 0 {
			_ = s.store.AddSceneRelations(ctx, x.ID, addP, addT)
			changed++
		}
		if len(sn) > 0 {
			var best int64
			bestLen := 0
			for _, n := range sn {
				if strings.Contains(hay, n.text) && len(n.text) > bestLen {
					best, bestLen = n.id, len(n.text)
				}
			}
			if best > 0 {
				res, _ := s.store.DB.ExecContext(ctx, `UPDATE scenes SET studio_id = ? WHERE id = ? AND studio_id IS NULL`, best, x.ID)
				if n, _ := res.RowsAffected(); n > 0 && len(addP) == 0 && len(addT) == 0 {
					changed++
				}
			}
		}
	}
	return changed, nil
}
