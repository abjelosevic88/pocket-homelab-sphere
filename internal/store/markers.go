package store

import (
	"context"
	"database/sql"
	"strings"
)

const markerCols = `mk.id, mk.scene_id, mk.title, mk.seconds, mk.end_seconds, mk.primary_tag_id, mk.created_at,
	COALESCE(t.id,0), COALESCE(t.name,''), CASE WHEN s.title = '' THEN s.path ELSE s.title END`

func scanMarker(row interface{ Scan(...any) error }) (*Marker, error) {
	var m Marker
	var end sql.NullFloat64
	var tag sql.NullInt64
	var tid int64
	var tname string
	if err := row.Scan(&m.ID, &m.SceneID, &m.Title, &m.Seconds, &end, &tag, &m.CreatedAt, &tid, &tname, &m.SceneName); err != nil {
		return nil, notFound(err)
	}
	if end.Valid {
		m.EndSeconds = &end.Float64
	}
	if tag.Valid {
		m.PrimaryTagID = &tag.Int64
	}
	if tid > 0 {
		m.Tag = &Ref{ID: tid, Name: tname}
	}
	return &m, nil
}

// MarkerFilter narrows the marker wall.
type MarkerFilter struct {
	SceneID int64
	TagID   int64
	Query   string
	Sort    string // scene | added | random | seconds
	Limit   int
	Offset  int
}

// ListMarkers returns markers (of a scene, a tag, or all).
func (s *Store) ListMarkers(ctx context.Context, f MarkerFilter) ([]Marker, int, error) {
	w := []string{"s.trashed_at = ''"}
	var args []any
	if f.SceneID > 0 {
		w = append(w, "mk.scene_id = ?")
		args = append(args, f.SceneID)
	}
	if f.TagID > 0 {
		w = append(w, "(mk.primary_tag_id = ? OR mk.primary_tag_id IN (SELECT id FROM tags WHERE parent_id = ?))")
		args = append(args, f.TagID, f.TagID)
	}
	for _, tok := range strings.Fields(strings.ToLower(f.Query)) {
		w = append(w, "(lower(mk.title) LIKE ? OR lower(COALESCE(t.name,'')) LIKE ? OR lower(s.title) LIKE ? OR lower(s.path) LIKE ?)")
		like := "%" + tok + "%"
		args = append(args, like, like, like, like)
	}
	from := ` FROM markers mk JOIN scenes s ON s.id = mk.scene_id LEFT JOIN tags t ON t.id = mk.primary_tag_id WHERE ` + strings.Join(w, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "mk.scene_id, mk.seconds"
	switch f.Sort {
	case "added":
		order = "mk.created_at DESC"
	case "random":
		order = "((mk.id * 7919 + 13) % 1000003)"
	case "scene":
		order = "s.created_at DESC, mk.seconds"
	}
	limit := f.Limit
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+markerCols+from+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Marker{}
	for rows.Next() {
		m, err := scanMarker(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *m)
	}
	return out, total, rows.Err()
}

// GetMarker loads one marker.
func (s *Store) GetMarker(ctx context.Context, id int64) (*Marker, error) {
	return scanMarker(s.DB.QueryRowContext(ctx, `SELECT `+markerCols+` FROM markers mk JOIN scenes s ON s.id = mk.scene_id LEFT JOIN tags t ON t.id = mk.primary_tag_id WHERE mk.id = ?`, id))
}

// SaveMarker inserts (ID 0) or updates a marker.
func (s *Store) SaveMarker(ctx context.Context, m *Marker, by int64) error {
	var end any
	if m.EndSeconds != nil && *m.EndSeconds > m.Seconds {
		end = *m.EndSeconds
	}
	if m.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO markers (scene_id, title, seconds, end_seconds, primary_tag_id, created_by) VALUES (?,?,?,?,?,?)`,
			m.SceneID, strings.TrimSpace(m.Title), m.Seconds, end, nullI(m.PrimaryTagID), nullI(&by))
		if err != nil {
			return err
		}
		m.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE markers SET title = ?, seconds = ?, end_seconds = ?, primary_tag_id = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(m.Title), m.Seconds, end, nullI(m.PrimaryTagID), Now(), m.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMarker removes a marker.
func (s *Store) DeleteMarker(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM markers WHERE id = ?`, id)
	return err
}

// ---------- galleries ----------

const galleryCols = `g.id, g.path, g.is_zip, g.title, g.date, g.details, g.photographer, g.urls, g.studio_id, g.scene_id,
	g.image_count, g.cover_image, g.file_mtime, g.missing_since, g.created_at, g.updated_at`

func scanGallery(row interface{ Scan(...any) error }, extra ...any) (*Gallery, error) {
	var g Gallery
	var urls string
	var studio, scene sql.NullInt64
	if err := row.Scan(append([]any{&g.ID, &g.Path, &g.IsZip, &g.Title, &g.Date, &g.Details, &g.Photographer, &urls, &studio, &scene,
		&g.ImageCount, &g.CoverImage, &g.FileMtime, &g.MissingSince, &g.CreatedAt, &g.UpdatedAt}, extra...)...); err != nil {
		return nil, notFound(err)
	}
	g.URLs = decodeList(urls)
	if studio.Valid {
		g.StudioID = &studio.Int64
	}
	if scene.Valid {
		g.SceneID = &scene.Int64
	}
	g.Performers, g.Tags = []Ref{}, []Ref{}
	return &g, nil
}

// GalleryFilter narrows the gallery list.
type GalleryFilter struct {
	UserID    int64
	Query     string
	Performer int64
	Tag       int64
	Studio    int64
	Favorite  bool
	Sort      string // added | title | date | images | rating | random
	Desc      bool
	Limit     int
	Offset    int
}

// ListGalleries returns a page of galleries.
func (s *Store) ListGalleries(ctx context.Context, f GalleryFilter) ([]Gallery, int, error) {
	w := []string{"g.missing_since = ''"}
	var args []any
	for _, tok := range strings.Fields(strings.ToLower(f.Query)) {
		w = append(w, "(lower(g.title) LIKE ? OR lower(g.path) LIKE ? OR EXISTS (SELECT 1 FROM gallery_performers gp JOIN performers p ON p.id = gp.performer_id WHERE gp.gallery_id = g.id AND lower(p.name) LIKE ?))")
		like := "%" + tok + "%"
		args = append(args, like, like, like)
	}
	if f.Performer > 0 {
		w = append(w, "EXISTS (SELECT 1 FROM gallery_performers gp WHERE gp.gallery_id = g.id AND gp.performer_id = ?)")
		args = append(args, f.Performer)
	}
	if f.Tag > 0 {
		w = append(w, "EXISTS (SELECT 1 FROM gallery_tags gt WHERE gt.gallery_id = g.id AND gt.tag_id = ?)")
		args = append(args, f.Tag)
	}
	if f.Studio > 0 {
		w = append(w, "g.studio_id = ?")
		args = append(args, f.Studio)
	}
	if f.Favorite {
		w = append(w, "COALESCE(m.favorite,0) = 1")
	}
	from := ` FROM galleries g LEFT JOIN user_marks m ON m.kind = 'gallery' AND m.item_id = g.id AND m.user_id = ? WHERE ` + strings.Join(w, " AND ")
	all := append([]any{f.UserID}, args...)
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+from, all...).Scan(&total); err != nil {
		return nil, 0, err
	}
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	order := "g.created_at " + dir
	switch f.Sort {
	case "title":
		order = "lower(CASE WHEN g.title = '' THEN g.path ELSE g.title END) " + dir
	case "date":
		order = "COALESCE(NULLIF(g.date,''), substr(g.created_at,1,10)) " + dir
	case "images":
		order = "g.image_count " + dir
	case "rating":
		order = "COALESCE(m.rating,0) " + dir
	case "random":
		order = "((g.id * 7919 + 13) % 1000003)"
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 60
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+galleryCols+`, COALESCE(m.favorite,0), COALESCE(m.rating,0)`+from+` ORDER BY `+order+`, g.id LIMIT ? OFFSET ?`, append(all, limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	out := []Gallery{}
	for rows.Next() {
		var fav bool
		var rating int
		g, err := scanGallery(rows, &fav, &rating)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		g.Favorite, g.Rating = fav, rating
		out = append(out, *g)
	}
	rows.Close()
	return out, total, s.fillGalleries(ctx, out)
}

func (s *Store) fillGalleries(ctx context.Context, list []Gallery) error {
	if len(list) == 0 {
		return nil
	}
	at := map[int64]int{}
	ids := make([]int64, len(list))
	for i := range list {
		at[list[i].ID] = i
		ids[i] = list[i].ID
	}
	in := idList(ids)
	for _, q := range []struct {
		sql   string
		perfs bool
	}{
		{`SELECT gp.gallery_id, p.id, p.name, p.image_path FROM gallery_performers gp JOIN performers p ON p.id = gp.performer_id WHERE gp.gallery_id IN (` + in + `) ORDER BY p.name`, true},
		{`SELECT gt.gallery_id, t.id, t.name, t.image_path FROM gallery_tags gt JOIN tags t ON t.id = gt.tag_id WHERE gt.gallery_id IN (` + in + `) ORDER BY t.name`, false},
	} {
		rows, err := s.DB.QueryContext(ctx, q.sql)
		if err != nil {
			return err
		}
		for rows.Next() {
			var gid int64
			var r Ref
			if err := rows.Scan(&gid, &r.ID, &r.Name, &r.Image); err != nil {
				rows.Close()
				return err
			}
			if q.perfs {
				list[at[gid]].Performers = append(list[at[gid]].Performers, r)
			} else {
				list[at[gid]].Tags = append(list[at[gid]].Tags, r)
			}
		}
		rows.Close()
	}
	sids := []int64{}
	for _, g := range list {
		if g.StudioID != nil {
			sids = append(sids, *g.StudioID)
		}
	}
	if len(sids) > 0 {
		rows, err := s.DB.QueryContext(ctx, `SELECT id, name FROM studios WHERE id IN (`+idList(sids)+`)`)
		if err != nil {
			return err
		}
		names := map[int64]string{}
		for rows.Next() {
			var id int64
			var n string
			if rows.Scan(&id, &n) == nil {
				names[id] = n
			}
		}
		rows.Close()
		for i := range list {
			if list[i].StudioID != nil {
				list[i].Studio = &Ref{ID: *list[i].StudioID, Name: names[*list[i].StudioID]}
			}
		}
	}
	return nil
}

// GetGallery loads one gallery.
func (s *Store) GetGallery(ctx context.Context, uid, id int64) (*Gallery, error) {
	var fav bool
	var rating int
	g, err := scanGallery(s.DB.QueryRowContext(ctx, `SELECT `+galleryCols+`, COALESCE(m.favorite,0), COALESCE(m.rating,0) FROM galleries g
		LEFT JOIN user_marks m ON m.kind = 'gallery' AND m.item_id = g.id AND m.user_id = ? WHERE g.id = ?`, uid, id), &fav, &rating)
	if err != nil {
		return nil, err
	}
	g.Favorite, g.Rating = fav, rating
	list := []Gallery{*g}
	return &list[0], s.fillGalleries(ctx, list)
}

// GalleryPatch changes a gallery's facts.
type GalleryPatch struct {
	Title, Date, Details, Photographer *string
	URLs                               *[]string
	StudioID                           **int64
	SceneID                            **int64
	CoverImage                         *int64
	Performers, Tags                   *[]int64
}

// UpdateGallery applies a patch.
func (s *Store) UpdateGallery(ctx context.Context, id int64, p GalleryPatch) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	set := []string{"updated_at = ?"}
	args := []any{Now()}
	for col, v := range map[string]*string{"title": p.Title, "date": p.Date, "details": p.Details, "photographer": p.Photographer} {
		if v != nil {
			set = append(set, col+" = ?")
			args = append(args, strings.TrimSpace(*v))
		}
	}
	if p.URLs != nil {
		set = append(set, "urls = ?")
		args = append(args, EncodeList(*p.URLs))
	}
	if p.StudioID != nil {
		set = append(set, "studio_id = ?")
		args = append(args, nullI(*p.StudioID))
	}
	if p.SceneID != nil {
		set = append(set, "scene_id = ?")
		args = append(args, nullI(*p.SceneID))
	}
	if p.CoverImage != nil {
		set = append(set, "cover_image = ?")
		args = append(args, *p.CoverImage)
	}
	res, err := tx.ExecContext(ctx, `UPDATE galleries SET `+strings.Join(set, ", ")+` WHERE id = ?`, append(args, id)...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if p.Performers != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM gallery_performers WHERE gallery_id = ?`, id); err != nil {
			return err
		}
		for _, pid := range *p.Performers {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gallery_performers (gallery_id, performer_id) SELECT ?, id FROM performers WHERE id = ?`, id, pid); err != nil {
				return err
			}
		}
	}
	if p.Tags != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM gallery_tags WHERE gallery_id = ?`, id); err != nil {
			return err
		}
		for _, tid := range *p.Tags {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gallery_tags (gallery_id, tag_id) SELECT ?, id FROM tags WHERE id = ?`, id, tid); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// GalleryIndex is the scanner's view of a gallery row.
type GalleryIndex struct {
	ID      int64
	Path    string
	Mtime   string
	Missing bool
}

// AllGalleries lists every gallery's path.
func (s *Store) AllGalleries(ctx context.Context) ([]GalleryIndex, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, path, file_mtime, missing_since != '' FROM galleries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GalleryIndex
	for rows.Next() {
		var g GalleryIndex
		if err := rows.Scan(&g.ID, &g.Path, &g.Mtime, &g.Missing); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ImageFile is one picture the scanner found.
type ImageFile struct {
	Name      string
	SizeBytes int64
	Width     int
	Height    int
}

// SyncGallery creates or refreshes a gallery and its pictures.
func (s *Store) SyncGallery(ctx context.Context, path string, isZip bool, mtime string, images []ImageFile) (int64, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var id int64
	created := false
	err = tx.QueryRowContext(ctx, `SELECT id FROM galleries WHERE path = ?`, path).Scan(&id)
	if err == sql.ErrNoRows {
		res, err := tx.ExecContext(ctx, `INSERT INTO galleries (path, is_zip, file_mtime, image_count) VALUES (?, ?, ?, ?)`, path, boolInt(isZip), mtime, len(images))
		if err != nil {
			return 0, false, err
		}
		id, _ = res.LastInsertId()
		created = true
	} else if err != nil {
		return 0, false, err
	} else if _, err := tx.ExecContext(ctx, `UPDATE galleries SET file_mtime = ?, image_count = ?, missing_since = '' WHERE id = ?`, mtime, len(images), id); err != nil {
		return 0, false, err
	}
	keep := map[string]bool{}
	for i, im := range images {
		keep[im.Name] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO images (gallery_id, name, position, size_bytes, width, height) VALUES (?,?,?,?,?,?)
			ON CONFLICT(gallery_id, name) DO UPDATE SET position = excluded.position, size_bytes = excluded.size_bytes,
			width = CASE WHEN excluded.width > 0 THEN excluded.width ELSE images.width END,
			height = CASE WHEN excluded.height > 0 THEN excluded.height ELSE images.height END`, id, im.Name, i, im.SizeBytes, im.Width, im.Height); err != nil {
			return 0, false, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM images WHERE gallery_id = ?`, id)
	if err != nil {
		return 0, false, err
	}
	var drop []int64
	for rows.Next() {
		var iid int64
		var n string
		if rows.Scan(&iid, &n) == nil && !keep[n] {
			drop = append(drop, iid)
		}
	}
	rows.Close()
	if len(drop) > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM images WHERE id IN (`+idList(drop)+`)`); err != nil {
			return 0, false, err
		}
	}
	return id, created, tx.Commit()
}

// SetGalleryMissing marks a gallery folder as gone (or back).
func (s *Store) SetGalleryMissing(ctx context.Context, id int64, missing bool) error {
	v := ""
	if missing {
		v = Now()
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE galleries SET missing_since = ? WHERE id = ?`, v, id)
	return err
}

// GalleryImages lists a gallery's pictures in order.
func (s *Store) GalleryImages(ctx context.Context, id int64) ([]Image, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, gallery_id, name, position, size_bytes, width, height FROM images WHERE gallery_id = ? ORDER BY position, name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Image{}
	for rows.Next() {
		var im Image
		if err := rows.Scan(&im.ID, &im.GalleryID, &im.Name, &im.Position, &im.SizeBytes, &im.Width, &im.Height); err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, rows.Err()
}

// GetImage loads one picture.
func (s *Store) GetImage(ctx context.Context, id int64) (*Image, error) {
	var im Image
	err := s.DB.QueryRowContext(ctx, `SELECT id, gallery_id, name, position, size_bytes, width, height FROM images WHERE id = ?`, id).
		Scan(&im.ID, &im.GalleryID, &im.Name, &im.Position, &im.SizeBytes, &im.Width, &im.Height)
	return &im, notFound(err)
}

// GalleryCoverImage returns the picture a gallery shows on its card.
func (s *Store) GalleryCoverImage(ctx context.Context, g *Gallery) int64 {
	if g.CoverImage > 0 {
		return g.CoverImage
	}
	var id int64
	_ = s.DB.QueryRowContext(ctx, `SELECT id FROM images WHERE gallery_id = ? ORDER BY position, name LIMIT 1`, g.ID).Scan(&id)
	return id
}
