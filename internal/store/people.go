package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// ---------- personal marks (favourite, rating) ----------

// Mark is one person's favourite/rating of a performer, studio, tag, group or gallery.
type Mark struct {
	Favorite bool
	Rating   int
}

// SetMark stores a favourite and/or rating (nil = unchanged).
func (s *Store) SetMark(ctx context.Context, uid int64, kind string, id int64, favorite *bool, rating *int) error {
	if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO user_marks (user_id, kind, item_id) VALUES (?, ?, ?)`, uid, kind, id); err != nil {
		return err
	}
	if favorite != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE user_marks SET favorite = ? WHERE user_id = ? AND kind = ? AND item_id = ?`, boolInt(*favorite), uid, kind, id); err != nil {
			return err
		}
	}
	if rating != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE user_marks SET rating = ? WHERE user_id = ? AND kind = ? AND item_id = ?`, *rating, uid, kind, id); err != nil {
			return err
		}
	}
	return nil
}

// Marks returns one person's marks of one kind.
func (s *Store) Marks(ctx context.Context, uid int64, kind string) (map[int64]Mark, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT item_id, favorite, rating FROM user_marks WHERE user_id = ? AND kind = ?`, uid, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Mark{}
	for rows.Next() {
		var id int64
		var m Mark
		if err := rows.Scan(&id, &m.Favorite, &m.Rating); err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, rows.Err()
}

// visibleScenes counts scenes not in the trash; used in the counts below.
const visibleScene = `s.trashed_at = ''`

// ---------- performers ----------

const performerCols = `p.id, p.name, p.disambiguation, p.aliases, p.gender, p.birthdate, p.death_date, p.country, p.ethnicity,
	p.hair_color, p.eye_color, p.height_cm, p.weight_kg, p.measurements, p.fake_tits, p.tattoos, p.piercings, p.career,
	p.details, p.urls, p.image_path, p.stash_ids, p.created_at, p.updated_at`

func scanPerformer(row interface{ Scan(...any) error }) (*Performer, error) {
	var p Performer
	var urls, stash string
	if err := row.Scan(&p.ID, &p.Name, &p.Disambiguation, &p.Aliases, &p.Gender, &p.Birthdate, &p.DeathDate, &p.Country, &p.Ethnicity,
		&p.HairColor, &p.EyeColor, &p.HeightCM, &p.WeightKG, &p.Measurements, &p.FakeTits, &p.Tattoos, &p.Piercings, &p.Career,
		&p.Details, &urls, &p.ImagePath, &stash, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	p.URLs, p.StashIDs = decodeList(urls), decodeMap(stash)
	if t, err := time.Parse("2006-01-02", p.Birthdate); err == nil {
		end := time.Now()
		if d, err := time.Parse("2006-01-02", p.DeathDate); err == nil {
			end = d
		}
		p.Age = end.Year() - t.Year()
		if end.YearDay() < t.YearDay() {
			p.Age--
		}
	}
	return &p, nil
}

// PerformerFilter narrows the performer list.
type PerformerFilter struct {
	UserID   int64
	Query    string
	Gender   string
	Favorite bool
	Sort     string // name | scenes | rating | age | added | o | random
	Desc     bool
	Limit    int
	Offset   int
}

// ListPerformers returns a page of performers with counts and the person's marks.
func (s *Store) ListPerformers(ctx context.Context, f PerformerFilter) ([]Performer, int, error) {
	w := []string{"1=1"}
	var args []any
	for _, tok := range strings.Fields(strings.ToLower(f.Query)) {
		w = append(w, "(lower(p.name) LIKE ? OR lower(p.aliases) LIKE ? OR lower(p.disambiguation) LIKE ?)")
		like := "%" + tok + "%"
		args = append(args, like, like, like)
	}
	if f.Gender != "" {
		w = append(w, "p.gender = ?")
		args = append(args, f.Gender)
	}
	if f.Favorite {
		w = append(w, "COALESCE(m.favorite,0) = 1")
	}
	from := ` FROM performers p LEFT JOIN user_marks m ON m.kind = 'performer' AND m.item_id = p.id AND m.user_id = ? WHERE ` + strings.Join(w, " AND ")
	all := append([]any{f.UserID}, args...)
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+from, all...).Scan(&total); err != nil {
		return nil, 0, err
	}
	count := `(SELECT COUNT(1) FROM scene_performers sp JOIN scenes s ON s.id = sp.scene_id WHERE sp.performer_id = p.id AND ` + visibleScene + `)`
	ocount := `(SELECT COALESCE(SUM(us.o_count),0) FROM scene_performers sp JOIN user_scenes us ON us.scene_id = sp.scene_id AND us.user_id = ` + "?" + ` WHERE sp.performer_id = p.id)`
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	order := "p.name COLLATE NOCASE " + dir
	switch f.Sort {
	case "scenes":
		order = "scenes " + dir + ", p.name COLLATE NOCASE"
	case "rating":
		order = "COALESCE(m.rating,0) " + dir + ", p.name COLLATE NOCASE"
	case "age":
		order = "p.birthdate = '', p.birthdate " + map[string]string{"ASC": "DESC", "DESC": "ASC"}[dir]
	case "added":
		order = "p.created_at " + dir
	case "o":
		order = "o " + dir + ", p.name COLLATE NOCASE"
	case "random":
		order = "((p.id * 7919 + " + time.Now().Format("20060102") + ") % 1000003)"
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 60
	}
	q := `SELECT ` + performerCols + `, ` + count + ` AS scenes, ` + ocount + ` AS o, COALESCE(m.favorite,0), COALESCE(m.rating,0)` + from + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	rows, err := s.DB.QueryContext(ctx, q, append(append([]any{f.UserID}, all...), limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Performer{}
	for rows.Next() {
		var p Performer
		var urls, stash string
		if err := rows.Scan(&p.ID, &p.Name, &p.Disambiguation, &p.Aliases, &p.Gender, &p.Birthdate, &p.DeathDate, &p.Country, &p.Ethnicity,
			&p.HairColor, &p.EyeColor, &p.HeightCM, &p.WeightKG, &p.Measurements, &p.FakeTits, &p.Tattoos, &p.Piercings, &p.Career,
			&p.Details, &urls, &p.ImagePath, &stash, &p.CreatedAt, &p.UpdatedAt, &p.Scenes, &p.OCount, &p.Favorite, &p.Rating); err != nil {
			return nil, 0, err
		}
		p.URLs, p.StashIDs = decodeList(urls), decodeMap(stash)
		if t, err := time.Parse("2006-01-02", p.Birthdate); err == nil {
			p.Age = int(time.Since(t).Hours() / 24 / 365.25)
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// GetPerformer loads one performer with counts and the person's marks.
func (s *Store) GetPerformer(ctx context.Context, uid, id int64) (*Performer, error) {
	p, err := scanPerformer(s.DB.QueryRowContext(ctx, `SELECT `+performerCols+` FROM performers p WHERE p.id = ?`, id))
	if err != nil {
		return nil, err
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM scene_performers sp JOIN scenes s ON s.id = sp.scene_id WHERE sp.performer_id = ? AND `+visibleScene, id).Scan(&p.Scenes)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM gallery_performers WHERE performer_id = ?`, id).Scan(&p.Galleries)
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(us.o_count),0) FROM scene_performers sp JOIN user_scenes us ON us.scene_id = sp.scene_id AND us.user_id = ? WHERE sp.performer_id = ?`, uid, id).Scan(&p.OCount)
	_ = s.DB.QueryRowContext(ctx, `SELECT favorite, rating FROM user_marks WHERE user_id = ? AND kind = 'performer' AND item_id = ?`, uid, id).Scan(&p.Favorite, &p.Rating)
	return p, nil
}

// SavePerformer inserts (ID 0) or updates a performer's facts.
func (s *Store) SavePerformer(ctx context.Context, p *Performer) error {
	p.Name = strings.TrimSpace(p.Name)
	args := []any{p.Name, strings.TrimSpace(p.Disambiguation), JoinList(p.Aliases), p.Gender, p.Birthdate, p.DeathDate, p.Country, p.Ethnicity,
		p.HairColor, p.EyeColor, p.HeightCM, p.WeightKG, p.Measurements, p.FakeTits, p.Tattoos, p.Piercings, p.Career,
		p.Details, EncodeList(p.URLs), EncodeMap(p.StashIDs)}
	if p.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO performers (name, disambiguation, aliases, gender, birthdate, death_date, country, ethnicity,
			hair_color, eye_color, height_cm, weight_kg, measurements, fake_tits, tattoos, piercings, career, details, urls, stash_ids)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
		if err != nil {
			return conflict(err)
		}
		p.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE performers SET name=?, disambiguation=?, aliases=?, gender=?, birthdate=?, death_date=?, country=?, ethnicity=?,
		hair_color=?, eye_color=?, height_cm=?, weight_kg=?, measurements=?, fake_tits=?, tattoos=?, piercings=?, career=?, details=?, urls=?, stash_ids=?,
		updated_at=? WHERE id=?`, append(args, Now(), p.ID)...)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPerformerImage stores the picture path (” removes it).
func (s *Store) SetPerformerImage(ctx context.Context, id int64, path string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE performers SET image_path = ?, updated_at = ? WHERE id = ?`, path, Now(), id)
	return err
}

// DeletePerformer removes a performer (scenes keep everything else).
func (s *Store) DeletePerformer(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM performers WHERE id = ?`, id)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM user_marks WHERE kind = 'performer' AND item_id = ?`, id)
	return err
}

// MergePerformers moves every scene and gallery of the sources onto the target and removes the sources.
func (s *Store) MergePerformers(ctx context.Context, target int64, sources []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, src := range sources {
		if src == target {
			continue
		}
		var name, aliases string
		if err := tx.QueryRowContext(ctx, `SELECT name, aliases FROM performers WHERE id = ?`, src).Scan(&name, &aliases); err != nil {
			return notFound(err)
		}
		stmts := []string{
			`INSERT OR IGNORE INTO scene_performers (scene_id, performer_id) SELECT scene_id, ? FROM scene_performers WHERE performer_id = ?`,
			`INSERT OR IGNORE INTO gallery_performers (gallery_id, performer_id) SELECT gallery_id, ? FROM gallery_performers WHERE performer_id = ?`,
		}
		for _, q := range stmts {
			if _, err := tx.ExecContext(ctx, q, target, src); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE performers SET aliases = trim(aliases || CASE WHEN aliases = '' THEN '' ELSE ', ' END || ?, ', ') WHERE id = ?`,
			strings.Trim(name+", "+aliases, ", "), target); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM performers WHERE id = ?`, src); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- names → ids (find or create) ----------

// NameIndex maps lower-case names and aliases to ids, for matching.
func (s *Store) NameIndex(ctx context.Context, table string) (map[string]int64, error) {
	var q string
	switch table {
	case "performers":
		q = `SELECT id, name, aliases FROM performers`
	case "studios":
		q = `SELECT id, name, aliases FROM studios`
	case "tags":
		q = `SELECT id, name, aliases FROM tags`
	default:
		return nil, ErrNotFound
	}
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var name, aliases string
		if err := rows.Scan(&id, &name, &aliases); err != nil {
			return nil, err
		}
		out[strings.ToLower(name)] = id
		for _, a := range strings.Split(aliases, ",") {
			if a = strings.ToLower(strings.TrimSpace(a)); a != "" {
				if _, taken := out[a]; !taken {
					out[a] = id
				}
			}
		}
	}
	return out, rows.Err()
}

// EnsurePerformer returns the performer with this name (or alias), creating it.
func (s *Store) EnsurePerformer(ctx context.Context, name string) (int64, bool, error) {
	name = strings.TrimSpace(name)
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM performers WHERE name = ? COLLATE NOCASE ORDER BY disambiguation = '' DESC LIMIT 1`, name).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if err != sql.ErrNoRows {
		return 0, false, err
	}
	idx, err := s.NameIndex(ctx, "performers")
	if err != nil {
		return 0, false, err
	}
	if id, ok := idx[strings.ToLower(name)]; ok {
		return id, false, nil
	}
	p := &Performer{Name: name}
	if err := s.SavePerformer(ctx, p); err != nil {
		return 0, false, err
	}
	return p.ID, true, nil
}

// EnsureStudio returns the studio with this name (or alias), creating it.
func (s *Store) EnsureStudio(ctx context.Context, name string) (int64, bool, error) {
	name = strings.TrimSpace(name)
	idx, err := s.NameIndex(ctx, "studios")
	if err != nil {
		return 0, false, err
	}
	if id, ok := idx[strings.ToLower(name)]; ok {
		return id, false, nil
	}
	st := &Studio{Name: name}
	if err := s.SaveStudio(ctx, st); err != nil {
		return 0, false, err
	}
	return st.ID, true, nil
}

// EnsureTag returns the tag with this name (or alias), creating it.
func (s *Store) EnsureTag(ctx context.Context, name string) (int64, bool, error) {
	name = strings.TrimSpace(name)
	idx, err := s.NameIndex(ctx, "tags")
	if err != nil {
		return 0, false, err
	}
	if id, ok := idx[strings.ToLower(name)]; ok {
		return id, false, nil
	}
	t := &Tag{Name: name}
	if err := s.SaveTag(ctx, t); err != nil {
		return 0, false, err
	}
	return t.ID, true, nil
}

// ---------- studios ----------

const studioCols = `x.id, x.name, x.parent_id, x.aliases, x.url, x.details, x.image_path, x.stash_ids, x.created_at, x.updated_at`

func scanStudio(row interface{ Scan(...any) error }, extra ...any) (*Studio, error) {
	var st Studio
	var parent sql.NullInt64
	var stash string
	if err := row.Scan(append([]any{&st.ID, &st.Name, &parent, &st.Aliases, &st.URL, &st.Details, &st.ImagePath, &stash, &st.CreatedAt, &st.UpdatedAt}, extra...)...); err != nil {
		return nil, notFound(err)
	}
	if parent.Valid {
		st.ParentID = &parent.Int64
	}
	st.StashIDs = decodeMap(stash)
	return &st, nil
}

// ListStudios returns every studio with counts and the person's marks.
func (s *Store) ListStudios(ctx context.Context, uid int64, query string) ([]Studio, error) {
	w := "1=1"
	var args []any
	if q := strings.TrimSpace(strings.ToLower(query)); q != "" {
		w = "(lower(x.name) LIKE ? OR lower(x.aliases) LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+studioCols+`,
		(SELECT COUNT(1) FROM scenes s WHERE (s.studio_id = x.id OR s.studio_id IN (SELECT id FROM studios c WHERE c.parent_id = x.id)) AND `+visibleScene+`),
		COALESCE(m.favorite,0), COALESCE(m.rating,0)
		FROM studios x LEFT JOIN user_marks m ON m.kind = 'studio' AND m.item_id = x.id AND m.user_id = ?
		WHERE `+w+` ORDER BY x.name COLLATE NOCASE`, append([]any{uid}, args...)...)
	if err != nil {
		return nil, err
	}
	out := []Studio{}
	for rows.Next() {
		var st *Studio
		var n int
		var fav bool
		var rating int
		if st, err = scanStudio(rows, &n, &fav, &rating); err != nil {
			rows.Close()
			return nil, err
		}
		st.Scenes, st.Favorite, st.Rating = n, fav, rating
		out = append(out, *st)
	}
	rows.Close()
	names := map[int64]string{}
	for _, st := range out {
		names[st.ID] = st.Name
	}
	for i := range out {
		if out[i].ParentID != nil {
			out[i].Parent = &Ref{ID: *out[i].ParentID, Name: names[*out[i].ParentID]}
		}
	}
	return out, nil
}

// GetStudio loads one studio with parent, children and counts.
func (s *Store) GetStudio(ctx context.Context, uid, id int64) (*Studio, error) {
	st, err := scanStudio(s.DB.QueryRowContext(ctx, `SELECT `+studioCols+` FROM studios x WHERE x.id = ?`, id))
	if err != nil {
		return nil, err
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM scenes s WHERE (s.studio_id = ? OR s.studio_id IN (SELECT id FROM studios WHERE parent_id = ?)) AND `+visibleScene, id, id).Scan(&st.Scenes)
	_ = s.DB.QueryRowContext(ctx, `SELECT favorite, rating FROM user_marks WHERE user_id = ? AND kind = 'studio' AND item_id = ?`, uid, id).Scan(&st.Favorite, &st.Rating)
	if st.ParentID != nil {
		var r Ref
		if s.DB.QueryRowContext(ctx, `SELECT id, name, image_path FROM studios WHERE id = ?`, *st.ParentID).Scan(&r.ID, &r.Name, &r.Image) == nil {
			st.Parent = &r
		}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, image_path FROM studios WHERE parent_id = ? ORDER BY name COLLATE NOCASE`, id)
	if err == nil {
		for rows.Next() {
			var r Ref
			if rows.Scan(&r.ID, &r.Name, &r.Image) == nil {
				st.Children = append(st.Children, r)
			}
		}
		rows.Close()
	}
	return st, nil
}

// SaveStudio inserts (ID 0) or updates a studio.
func (s *Store) SaveStudio(ctx context.Context, st *Studio) error {
	st.Name = strings.TrimSpace(st.Name)
	if st.ParentID != nil && *st.ParentID == st.ID {
		st.ParentID = nil
	}
	args := []any{st.Name, nullI(st.ParentID), JoinList(st.Aliases), strings.TrimSpace(st.URL), st.Details, EncodeMap(st.StashIDs)}
	if st.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO studios (name, parent_id, aliases, url, details, stash_ids) VALUES (?,?,?,?,?,?)`, args...)
		if err != nil {
			return conflict(err)
		}
		st.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE studios SET name=?, parent_id=?, aliases=?, url=?, details=?, stash_ids=?, updated_at=? WHERE id=?`, append(args, Now(), st.ID)...)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetStudioImage stores the logo path.
func (s *Store) SetStudioImage(ctx context.Context, id int64, path string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE studios SET image_path = ?, updated_at = ? WHERE id = ?`, path, Now(), id)
	return err
}

// DeleteStudio removes a studio.
func (s *Store) DeleteStudio(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM studios WHERE id = ?`, id)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM user_marks WHERE kind = 'studio' AND item_id = ?`, id)
	return err
}

// ---------- tags ----------

const tagCols = `t.id, t.name, t.aliases, t.description, t.parent_id, t.image_path, t.created_at, t.updated_at`

func scanTag(row interface{ Scan(...any) error }, extra ...any) (*Tag, error) {
	var t Tag
	var parent sql.NullInt64
	if err := row.Scan(append([]any{&t.ID, &t.Name, &t.Aliases, &t.Description, &parent, &t.ImagePath, &t.CreatedAt, &t.UpdatedAt}, extra...)...); err != nil {
		return nil, notFound(err)
	}
	if parent.Valid {
		t.ParentID = &parent.Int64
	}
	return &t, nil
}

// ListTags returns every tag with counts and the person's favourites.
func (s *Store) ListTags(ctx context.Context, uid int64, query string) ([]Tag, error) {
	w := "1=1"
	var args []any
	if q := strings.TrimSpace(strings.ToLower(query)); q != "" {
		w = "(lower(t.name) LIKE ? OR lower(t.aliases) LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+tagCols+`,
		(SELECT COUNT(1) FROM scene_tags st JOIN scenes s ON s.id = st.scene_id WHERE st.tag_id = t.id AND `+visibleScene+`),
		(SELECT COUNT(1) FROM markers mk WHERE mk.primary_tag_id = t.id),
		COALESCE(m.favorite,0)
		FROM tags t LEFT JOIN user_marks m ON m.kind = 'tag' AND m.item_id = t.id AND m.user_id = ?
		WHERE `+w+` ORDER BY t.name COLLATE NOCASE`, append([]any{uid}, args...)...)
	if err != nil {
		return nil, err
	}
	out := []Tag{}
	for rows.Next() {
		var t *Tag
		var scenes, markers int
		var fav bool
		if t, err = scanTag(rows, &scenes, &markers, &fav); err != nil {
			rows.Close()
			return nil, err
		}
		t.Scenes, t.Markers, t.Favorite = scenes, markers, fav
		out = append(out, *t)
	}
	rows.Close()
	names := map[int64]string{}
	for _, t := range out {
		names[t.ID] = t.Name
	}
	for i := range out {
		if out[i].ParentID != nil {
			out[i].Parent = &Ref{ID: *out[i].ParentID, Name: names[*out[i].ParentID]}
		}
	}
	return out, nil
}

// GetTag loads one tag.
func (s *Store) GetTag(ctx context.Context, uid, id int64) (*Tag, error) {
	t, err := scanTag(s.DB.QueryRowContext(ctx, `SELECT `+tagCols+` FROM tags t WHERE t.id = ?`, id))
	if err != nil {
		return nil, err
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM scene_tags st JOIN scenes s ON s.id = st.scene_id WHERE (st.tag_id = ? OR st.tag_id IN (SELECT id FROM tags WHERE parent_id = ?)) AND `+visibleScene, id, id).Scan(&t.Scenes)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM markers WHERE primary_tag_id = ?`, id).Scan(&t.Markers)
	_ = s.DB.QueryRowContext(ctx, `SELECT favorite FROM user_marks WHERE user_id = ? AND kind = 'tag' AND item_id = ?`, uid, id).Scan(&t.Favorite)
	if t.ParentID != nil {
		var r Ref
		if s.DB.QueryRowContext(ctx, `SELECT id, name FROM tags WHERE id = ?`, *t.ParentID).Scan(&r.ID, &r.Name) == nil {
			t.Parent = &r
		}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, image_path FROM tags WHERE parent_id = ? ORDER BY name COLLATE NOCASE`, id)
	if err == nil {
		for rows.Next() {
			var r Ref
			if rows.Scan(&r.ID, &r.Name, &r.Image) == nil {
				t.Children = append(t.Children, r)
			}
		}
		rows.Close()
	}
	return t, nil
}

// SaveTag inserts (ID 0) or updates a tag.
func (s *Store) SaveTag(ctx context.Context, t *Tag) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.ParentID != nil && *t.ParentID == t.ID {
		t.ParentID = nil
	}
	args := []any{t.Name, JoinList(t.Aliases), t.Description, nullI(t.ParentID)}
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO tags (name, aliases, description, parent_id) VALUES (?,?,?,?)`, args...)
		if err != nil {
			return conflict(err)
		}
		t.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE tags SET name=?, aliases=?, description=?, parent_id=?, updated_at=? WHERE id=?`, append(args, Now(), t.ID)...)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTagImage stores the picture path.
func (s *Store) SetTagImage(ctx context.Context, id int64, path string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE tags SET image_path = ?, updated_at = ? WHERE id = ?`, path, Now(), id)
	return err
}

// DeleteTag removes a tag.
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM user_marks WHERE kind = 'tag' AND item_id = ?`, id)
	return err
}

// MergeTags moves scenes, markers and galleries of the sources onto the target.
func (s *Store) MergeTags(ctx context.Context, target int64, sources []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, src := range sources {
		if src == target {
			continue
		}
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM tags WHERE id = ?`, src).Scan(&name); err != nil {
			return notFound(err)
		}
		for _, q := range []string{
			`INSERT OR IGNORE INTO scene_tags (scene_id, tag_id) SELECT scene_id, ? FROM scene_tags WHERE tag_id = ?`,
			`INSERT OR IGNORE INTO gallery_tags (gallery_id, tag_id) SELECT gallery_id, ? FROM gallery_tags WHERE tag_id = ?`,
			`UPDATE markers SET primary_tag_id = ? WHERE primary_tag_id = ?`,
			`UPDATE tags SET parent_id = ? WHERE parent_id = ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, target, src); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tags SET aliases = trim(aliases || CASE WHEN aliases = '' THEN '' ELSE ', ' END || ?, ', ') WHERE id = ?`, name, target); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, src); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- groups ----------

const groupCols = `g.id, g.name, g.aliases, g.date, g.director, g.synopsis, g.studio_id, g.urls, g.front_path, g.back_path, g.created_at, g.updated_at`

func scanGroup(row interface{ Scan(...any) error }, extra ...any) (*Group, error) {
	var g Group
	var studio sql.NullInt64
	var urls string
	if err := row.Scan(append([]any{&g.ID, &g.Name, &g.Aliases, &g.Date, &g.Director, &g.Synopsis, &studio, &urls, &g.FrontPath, &g.BackPath, &g.CreatedAt, &g.UpdatedAt}, extra...)...); err != nil {
		return nil, notFound(err)
	}
	if studio.Valid {
		g.StudioID = &studio.Int64
	}
	g.URLs = decodeList(urls)
	return &g, nil
}

// ListGroups returns every group with counts and the person's marks.
func (s *Store) ListGroups(ctx context.Context, uid int64, query string) ([]Group, error) {
	w := "1=1"
	var args []any
	if q := strings.TrimSpace(strings.ToLower(query)); q != "" {
		w = "(lower(g.name) LIKE ? OR lower(g.aliases) LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+groupCols+`,
		(SELECT COUNT(1) FROM group_scenes gs JOIN scenes s ON s.id = gs.scene_id WHERE gs.group_id = g.id AND `+visibleScene+`),
		(SELECT COALESCE(SUM(s.duration),0) FROM group_scenes gs JOIN scenes s ON s.id = gs.scene_id WHERE gs.group_id = g.id AND `+visibleScene+`),
		COALESCE(m.favorite,0), COALESCE(m.rating,0), COALESCE(x.id,0), COALESCE(x.name,'')
		FROM groups_ g LEFT JOIN user_marks m ON m.kind = 'group' AND m.item_id = g.id AND m.user_id = ?
		LEFT JOIN studios x ON x.id = g.studio_id
		WHERE `+w+` ORDER BY g.name COLLATE NOCASE`, append([]any{uid}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g *Group
		var sid int64
		var sname string
		var n int
		var dur float64
		var fav bool
		var rating int
		if g, err = scanGroup(rows, &n, &dur, &fav, &rating, &sid, &sname); err != nil {
			return nil, err
		}
		g.Scenes, g.Duration, g.Favorite, g.Rating = n, dur, fav, rating
		if sid > 0 {
			g.Studio = &Ref{ID: sid, Name: sname}
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// GetGroup loads one group.
func (s *Store) GetGroup(ctx context.Context, uid, id int64) (*Group, error) {
	list, err := s.ListGroups(ctx, uid, "")
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, ErrNotFound
}

// SaveGroup inserts (ID 0) or updates a group.
func (s *Store) SaveGroup(ctx context.Context, g *Group) error {
	g.Name = strings.TrimSpace(g.Name)
	args := []any{g.Name, JoinList(g.Aliases), g.Date, g.Director, g.Synopsis, nullI(g.StudioID), EncodeList(g.URLs)}
	if g.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO groups_ (name, aliases, date, director, synopsis, studio_id, urls) VALUES (?,?,?,?,?,?,?)`, args...)
		if err != nil {
			return conflict(err)
		}
		g.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE groups_ SET name=?, aliases=?, date=?, director=?, synopsis=?, studio_id=?, urls=?, updated_at=? WHERE id=?`, append(args, Now(), g.ID)...)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetGroupImage stores the front or back cover.
func (s *Store) SetGroupImage(ctx context.Context, id int64, back bool, path string) error {
	col := "front_path"
	if back {
		col = "back_path"
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE groups_ SET `+col+` = ?, updated_at = ? WHERE id = ?`, path, Now(), id)
	return err
}

// DeleteGroup removes a group (the scenes stay).
func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM groups_ WHERE id = ?`, id)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM user_marks WHERE kind = 'group' AND item_id = ?`, id)
	return err
}

// SetSceneGroups replaces the groups a scene is in ({group id: scene number}).
func (s *Store) SetSceneGroups(ctx context.Context, sceneID int64, groups map[int64]int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_scenes WHERE scene_id = ?`, sceneID); err != nil {
		return err
	}
	for gid, idx := range groups {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO group_scenes (group_id, scene_id, scene_index) SELECT id, ?, ? FROM groups_ WHERE id = ?`, sceneID, idx, gid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GroupSceneIDs lists a group's scenes in order.
func (s *Store) GroupSceneIDs(ctx context.Context, id int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT gs.scene_id FROM group_scenes gs JOIN scenes s ON s.id = gs.scene_id WHERE gs.group_id = ? AND `+visibleScene+` ORDER BY gs.scene_index, s.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// JoinList cleans a comma-separated list, keeping case.
func JoinList(s string) string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			out = append(out, t)
		}
	}
	return strings.Join(out, ", ")
}
