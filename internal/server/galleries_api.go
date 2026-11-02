package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// ---------- markers ----------

func (s *Server) decorateMarker(m *store.Marker) {
	m.Stream = fmt.Sprintf("/api/v1/scenes/%d/stream", m.SceneID)
	m.Preview = fmt.Sprintf("/api/v1/markers/%d/clip.mp4?v=%d", m.ID, int(m.Seconds*10))
	m.Screenshot = fmt.Sprintf("/api/v1/markers/%d/picture.jpg?v=%d", m.ID, int(m.Seconds*10))
	if m.SceneName != "" && strings.Contains(m.SceneName, "/") {
		m.SceneName = niceName(m.SceneName)
	}
}

func (s *Server) handleListMarkers(w http.ResponseWriter, r *http.Request) {
	f := store.MarkerFilter{SceneID: idParam(r, "id"), TagID: int64(queryInt(r, "tag")), Query: r.URL.Query().Get("q"), Sort: r.URL.Query().Get("sort"), Limit: queryInt(r, "per")}
	if p := queryInt(r, "page"); p > 1 && f.Limit > 0 {
		f.Offset = (p - 1) * f.Limit
	}
	list, total, err := s.store.ListMarkers(r.Context(), f)
	if err != nil {
		s.fail(w, err, "markers")
		return
	}
	for i := range list {
		s.decorateMarker(&list[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total})
}

func (s *Server) handleSaveMarker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		store.Marker
		Tag string `json:"tag_name"` // a new tag by name
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	m := in.Marker
	if id := idParam(r, "mid"); id > 0 {
		cur, err := s.store.GetMarker(ctx, id)
		if err != nil {
			s.fail(w, err, "marker")
			return
		}
		m.ID, m.SceneID = cur.ID, cur.SceneID
	} else {
		m.SceneID = idParam(r, "id")
		if _, err := s.store.GetScene(ctx, 0, m.SceneID); err != nil {
			s.fail(w, err, "scene")
			return
		}
	}
	if m.Seconds < 0 {
		writeErr(w, http.StatusBadRequest, "the time cannot be negative")
		return
	}
	if name := strings.TrimSpace(in.Tag); name != "" {
		id, _, err := s.store.EnsureTag(ctx, name)
		if err != nil {
			s.fail(w, err, "tag")
			return
		}
		m.PrimaryTagID = &id
	}
	if m.PrimaryTagID != nil && *m.PrimaryTagID == 0 {
		m.PrimaryTagID = nil
	}
	if strings.TrimSpace(m.Title) == "" && m.PrimaryTagID == nil {
		writeErr(w, http.StatusBadRequest, "give the marker a title or a tag")
		return
	}
	if err := s.store.SaveMarker(ctx, &m, userFrom(ctx).ID); err != nil {
		s.fail(w, err, "save marker")
		return
	}
	out, _ := s.store.GetMarker(ctx, m.ID)
	s.decorateMarker(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteMarker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := s.store.GetMarker(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "marker")
		return
	}
	if err := s.store.DeleteMarker(ctx, m.ID); err != nil {
		s.fail(w, err, "delete")
		return
	}
	matches, _ := filepath.Glob(filepath.Join(s.cfg.GeneratedDir, "markers", fmt.Sprintf("%d-%d-*", m.SceneID, m.ID)))
	for _, p := range matches {
		_ = os.Remove(p)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- galleries ----------

func (s *Server) decorateGallery(g *store.Gallery) {
	g.Name = g.Title
	if g.Name == "" {
		g.Name = niceName(g.Path)
	}
	if g.ImageCount > 0 {
		g.Cover = fmt.Sprintf("/api/v1/galleries/%d/cover?v=%d", g.ID, g.CoverImage)
	}
	if g.Studio != nil && g.Studio.Image != "" {
		g.Studio.Image = ""
	}
}

func (s *Server) handleListGalleries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	per := queryInt(r, "per")
	if per <= 0 || per > 500 {
		per = 60
	}
	f := store.GalleryFilter{UserID: userFrom(r.Context()).ID, Query: q.Get("q"), Performer: int64(queryInt(r, "performer")), Tag: int64(queryInt(r, "tag")),
		Studio: int64(queryInt(r, "studio")), Favorite: queryBool(r, "favorite"), Sort: q.Get("sort"), Desc: q.Get("dir") != "asc", Limit: per}
	if p := queryInt(r, "page"); p > 1 {
		f.Offset = (p - 1) * per
	}
	list, total, err := s.store.ListGalleries(r.Context(), f)
	if err != nil {
		s.fail(w, err, "galleries")
		return
	}
	for i := range list {
		s.decorateGallery(&list[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total})
}

func (s *Server) handleGetGallery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.store.GetGallery(ctx, userFrom(ctx).ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "gallery")
		return
	}
	s.decorateGallery(g)
	ims, err := s.store.GalleryImages(ctx, g.ID)
	if err != nil {
		s.fail(w, err, "pictures")
		return
	}
	for i := range ims {
		ims[i].URL = fmt.Sprintf("/api/v1/images/%d", ims[i].ID)
		ims[i].Thumb = fmt.Sprintf("/api/v1/images/%d?thumb=1", ims[i].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"gallery": g, "images": ims})
}

func (s *Server) handleUpdateGallery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Title         string   `json:"title"`
		Date          string   `json:"date"`
		Details       string   `json:"details"`
		Photographer  string   `json:"photographer"`
		URLs          []string `json:"urls"`
		StudioID      int64    `json:"studio_id"`
		SceneID       int64    `json:"scene_id"`
		CoverImage    int64    `json:"cover_image"`
		Performers    []int64  `json:"performers"`
		Tags          []int64  `json:"tags"`
		NewTags       []string `json:"new_tags"`
		NewPerformers []string `json:"new_performers"`
		MFavorite     *bool    `json:"favorite"`
		MRating       *int     `json:"rating"`
	}
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	id := idParam(r, "id")
	p := store.GalleryPatch{}
	if keys["title"] {
		p.Title = &in.Title
	}
	if keys["date"] {
		if !validLooseDate(in.Date) {
			writeErr(w, http.StatusBadRequest, "the date should look like 2024-05-31")
			return
		}
		p.Date = &in.Date
	}
	if keys["details"] {
		p.Details = &in.Details
	}
	if keys["photographer"] {
		p.Photographer = &in.Photographer
	}
	if keys["urls"] {
		p.URLs = &in.URLs
	}
	if keys["studio_id"] {
		var sid *int64
		if in.StudioID > 0 {
			sid = &in.StudioID
		}
		p.StudioID = &sid
	}
	if keys["scene_id"] {
		var sid *int64
		if in.SceneID > 0 {
			sid = &in.SceneID
		}
		p.SceneID = &sid
	}
	if keys["cover_image"] {
		p.CoverImage = &in.CoverImage
	}
	if keys["performers"] || keys["new_performers"] {
		ids := append([]int64{}, in.Performers...)
		for _, n := range in.NewPerformers {
			if id, _, err := s.store.EnsurePerformer(ctx, n); err == nil && strings.TrimSpace(n) != "" {
				ids = append(ids, id)
			}
		}
		p.Performers = &ids
	}
	if keys["tags"] || keys["new_tags"] {
		ids := append([]int64{}, in.Tags...)
		for _, n := range in.NewTags {
			if id, _, err := s.store.EnsureTag(ctx, n); err == nil && strings.TrimSpace(n) != "" {
				ids = append(ids, id)
			}
		}
		p.Tags = &ids
	}
	if err := s.store.UpdateGallery(ctx, id, p); err != nil {
		s.fail(w, err, "save gallery")
		return
	}
	if in.MFavorite != nil || in.MRating != nil {
		_ = s.store.SetMark(ctx, userFrom(ctx).ID, "gallery", id, in.MFavorite, in.MRating)
	}
	s.handleGetGallery(w, r)
}

func (s *Server) handleGalleryCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.store.GetGallery(ctx, 0, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "gallery")
		return
	}
	imgID := s.store.GalleryCoverImage(ctx, g)
	if imgID == 0 {
		writeErr(w, http.StatusNotFound, "no pictures")
		return
	}
	s.serveImage(w, r, imgID, true)
}

func (s *Server) handleImageFile(w http.ResponseWriter, r *http.Request) {
	s.serveImage(w, r, idParam(r, "id"), queryBool(r, "thumb"))
}

// serveImage sends a gallery picture (from its folder or zip); thumb = a
// small JPEG made once and kept with the other made files.
func (s *Server) serveImage(w http.ResponseWriter, r *http.Request, id int64, thumb bool) {
	ctx := r.Context()
	im, err := s.store.GetImage(ctx, id)
	if err != nil {
		s.fail(w, err, "picture")
		return
	}
	g, err := s.store.GetGallery(ctx, 0, im.GalleryID)
	if err != nil {
		s.fail(w, err, "gallery")
		return
	}
	read := func() ([]byte, error) {
		full, err := s.libPath(g.Path)
		if err != nil {
			return nil, err
		}
		if !g.IsZip {
			return os.ReadFile(filepath.Join(full, filepath.Base(im.Name)))
		}
		zr, err := zip.OpenReader(full)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name == im.Name {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, 100<<20))
			}
		}
		return nil, os.ErrNotExist
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if thumb {
		dst := filepath.Join(s.cfg.GeneratedDir, "thumbs", fmt.Sprintf("%d.jpg", im.ID))
		err := s.makeOnce(dst, dst, func() error {
			b, err := read()
			if err != nil {
				return err
			}
			img, _, err := image.Decode(bytes.NewReader(b))
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, flatten(fit(img, 480)), &jpeg.Options{Quality: 80}); err != nil {
				return err
			}
			return writeFileAtomic(dst, buf.Bytes())
		})
		if err != nil {
			writeErr(w, http.StatusNotFound, "the picture could not be read")
			return
		}
		http.ServeFile(w, r, dst)
		return
	}
	if !g.IsZip {
		full, _ := s.libPath(g.Path)
		http.ServeFile(w, r, filepath.Join(full, filepath.Base(im.Name)))
		return
	}
	b, err := read()
	if err != nil {
		writeErr(w, http.StatusNotFound, "the picture could not be read")
		return
	}
	ct := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp", ".gif": "image/gif"}[strings.ToLower(path.Ext(im.Name))]
	w.Header().Set("Content-Type", ct)
	_, _ = w.Write(b)
}
