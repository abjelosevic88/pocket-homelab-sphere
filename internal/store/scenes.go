package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

const sceneCols = `s.id, s.path, s.size_bytes, s.file_mtime, s.oshash, s.phash, s.duration, s.width, s.height, s.fps,
	s.video_codec, s.audio_codec, s.container, s.bitrate, s.probed_at, s.title, s.code, s.details, s.director, s.date,
	s.urls, s.studio_id, s.organized, s.stash_ids, s.source_url, s.projection, s.stereo, s.fov, s.vr_set,
	s.cover_path, s.cover_source, s.gen_state, s.gen_error, s.gen_version, s.has_preview, s.has_sprite,
	s.missing_since, s.trashed_at, s.trash_path, s.added_by, s.created_at, s.updated_at`

const mineCols = `COALESCE(us.rating,0), COALESCE(us.favorite,0), COALESCE(us.o_count,0), COALESCE(us.play_count,0),
	COALESCE(us.play_seconds,0), COALESCE(us.resume_seconds,0), COALESCE(us.last_played_at,''), COALESCE(us.hidden,0)`

func scanScene(row interface{ Scan(...any) error }, withMine bool) (*Scene, error) {
	var sc Scene
	var urls, stash string
	var studio, addedBy sql.NullInt64
	dst := []any{&sc.ID, &sc.Path, &sc.SizeBytes, &sc.FileMtime, &sc.OSHash, &sc.PHash, &sc.Duration, &sc.Width, &sc.Height, &sc.FPS,
		&sc.VideoCodec, &sc.AudioCodec, &sc.Container, &sc.Bitrate, &sc.ProbedAt, &sc.Title, &sc.Code, &sc.Details, &sc.Director, &sc.Date,
		&urls, &studio, &sc.Organized, &stash, &sc.SourceURL, &sc.Projection, &sc.Stereo, &sc.FOV, &sc.VRSet,
		&sc.CoverPath, &sc.CoverSource, &sc.GenState, &sc.GenError, &sc.GenVersion, &sc.HasPreview, &sc.HasSprite,
		&sc.MissingSince, &sc.TrashedAt, &sc.TrashPath, &addedBy, &sc.CreatedAt, &sc.UpdatedAt}
	if withMine {
		m := &sc.Mine
		dst = append(dst, &m.Rating, &m.Favorite, &m.OCount, &m.PlayCount, &m.PlaySeconds, &m.ResumeSeconds, &m.LastPlayedAt, &m.Hidden)
	}
	if err := row.Scan(dst...); err != nil {
		return nil, notFound(err)
	}
	sc.URLs = decodeList(urls)
	sc.StashIDs = decodeMap(stash)
	if studio.Valid {
		sc.StudioID = &studio.Int64
	}
	if addedBy.Valid {
		sc.AddedBy = &addedBy.Int64
	}
	sc.Performers, sc.Tags = []Ref{}, []Ref{}
	return &sc, nil
}

func decodeList(s string) []string {
	out := []string{}
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func decodeMap(s string) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = map[string]string{}
	}
	return out
}

// EncodeList is the stored form of a list of strings.
func EncodeList(v []string) string {
	clean := []string{}
	seen := map[string]bool{}
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			clean = append(clean, s)
		}
	}
	b, _ := json.Marshal(clean)
	return string(b)
}

// EncodeMap is the stored form of a string map.
func EncodeMap(v map[string]string) string {
	if v == nil {
		v = map[string]string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func idList(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprint(id)
	}
	return strings.Join(parts, ",")
}

// GetScene loads one scene with its relations and the person's own marks.
func (s *Store) GetScene(ctx context.Context, uid, id int64) (*Scene, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+sceneCols+`, `+mineCols+` FROM scenes s
		LEFT JOIN user_scenes us ON us.scene_id = s.id AND us.user_id = ? WHERE s.id = ?`, uid, id)
	sc, err := scanScene(row, true)
	if err != nil {
		return nil, err
	}
	list := []Scene{*sc}
	if err := s.fillScenes(ctx, list, true); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// SceneByPath finds a scene by its library path.
func (s *Store) SceneByPath(ctx context.Context, path string) (*Scene, error) {
	return scanScene(s.DB.QueryRowContext(ctx, `SELECT `+sceneCols+` FROM scenes s WHERE s.path = ?`, path), false)
}

// fillScenes adds studio, performers, tags, groups and marker counts.
func (s *Store) fillScenes(ctx context.Context, list []Scene, groups bool) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]int64, len(list))
	at := map[int64]int{}
	studioIDs := map[int64]bool{}
	for i := range list {
		ids[i] = list[i].ID
		at[list[i].ID] = i
		if list[i].StudioID != nil {
			studioIDs[*list[i].StudioID] = true
		}
	}
	in := idList(ids)
	type pair struct {
		scene int64
		ref   Ref
	}
	collect := func(q string) ([]pair, error) {
		rows, err := s.DB.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []pair
		for rows.Next() {
			var p pair
			if err := rows.Scan(&p.scene, &p.ref.ID, &p.ref.Name, &p.ref.Image, &p.ref.Extra); err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, rows.Err()
	}
	perf, err := collect(`SELECT sp.scene_id, p.id, p.name, p.image_path, p.disambiguation FROM scene_performers sp
		JOIN performers p ON p.id = sp.performer_id WHERE sp.scene_id IN (` + in + `) ORDER BY p.name COLLATE NOCASE`)
	if err != nil {
		return err
	}
	for _, p := range perf {
		list[at[p.scene]].Performers = append(list[at[p.scene]].Performers, p.ref)
	}
	tags, err := collect(`SELECT st.scene_id, t.id, t.name, t.image_path, '' FROM scene_tags st
		JOIN tags t ON t.id = st.tag_id WHERE st.scene_id IN (` + in + `) ORDER BY t.name COLLATE NOCASE`)
	if err != nil {
		return err
	}
	for _, p := range tags {
		list[at[p.scene]].Tags = append(list[at[p.scene]].Tags, p.ref)
	}
	if groups {
		gs, err := collect(`SELECT gs.scene_id, g.id, g.name, g.front_path, CAST(gs.scene_index AS TEXT) FROM group_scenes gs
			JOIN groups_ g ON g.id = gs.group_id WHERE gs.scene_id IN (` + in + `) ORDER BY g.name`)
		if err != nil {
			return err
		}
		for _, p := range gs {
			list[at[p.scene]].Groups = append(list[at[p.scene]].Groups, p.ref)
		}
	}
	marks, err := collect(`SELECT scene_id, COUNT(1), '', '', '' FROM markers WHERE scene_id IN (` + in + `) GROUP BY scene_id`)
	if err != nil {
		return err
	}
	for _, p := range marks {
		list[at[p.scene]].Markers = int(p.ref.ID)
	}
	if len(studioIDs) > 0 {
		sids := make([]int64, 0, len(studioIDs))
		for id := range studioIDs {
			sids = append(sids, id)
		}
		rows, err := s.DB.QueryContext(ctx, `SELECT id, name, image_path FROM studios WHERE id IN (`+idList(sids)+`)`)
		if err != nil {
			return err
		}
		refs := map[int64]Ref{}
		for rows.Next() {
			var r Ref
			if err := rows.Scan(&r.ID, &r.Name, &r.Image); err != nil {
				rows.Close()
				return err
			}
			refs[r.ID] = r
		}
		rows.Close()
		for i := range list {
			if list[i].StudioID != nil {
				if r, ok := refs[*list[i].StudioID]; ok {
					list[i].Studio = &r
				}
			}
		}
	}
	return nil
}

// sceneWhere builds the WHERE clause of a scene list.
func sceneWhere(f SceneFilter) (string, []any) {
	var w []string
	var args []any
	if f.Trashed {
		w = append(w, "s.trashed_at != ''")
	} else {
		w = append(w, "s.trashed_at = ''")
	}
	if f.Missing {
		w = append(w, "s.missing_since != ''")
	}
	if !f.Hidden {
		w = append(w, "COALESCE(us.hidden,0) = 0")
	}
	for _, tok := range strings.Fields(strings.ToLower(f.Query)) {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(tok) + "%"
		w = append(w, `(lower(s.title) LIKE ? ESCAPE '\' OR lower(s.path) LIKE ? ESCAPE '\' OR lower(s.details) LIKE ? ESCAPE '\' OR lower(s.code) LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM scene_performers sp JOIN performers p ON p.id = sp.performer_id WHERE sp.scene_id = s.id AND (lower(p.name) LIKE ? ESCAPE '\' OR lower(p.aliases) LIKE ? ESCAPE '\'))
			OR EXISTS (SELECT 1 FROM scene_tags st JOIN tags t ON t.id = st.tag_id WHERE st.scene_id = s.id AND lower(t.name) LIKE ? ESCAPE '\')
			OR EXISTS (SELECT 1 FROM studios x WHERE x.id = s.studio_id AND lower(x.name) LIKE ? ESCAPE '\'))`)
		for i := 0; i < 8; i++ {
			args = append(args, like)
		}
	}
	if len(f.IDs) > 0 {
		w = append(w, "s.id IN ("+idList(f.IDs)+")")
	}
	for _, p := range f.Performers {
		w = append(w, "EXISTS (SELECT 1 FROM scene_performers sp WHERE sp.scene_id = s.id AND sp.performer_id = ?)")
		args = append(args, p)
	}
	if len(f.Tags) > 0 {
		// a tag also finds the scenes of its child tags (one level)
		sub := "SELECT 1 FROM scene_tags st JOIN tags t ON t.id = st.tag_id WHERE st.scene_id = s.id AND (t.id = ? OR t.parent_id = ?)"
		var parts []string
		for _, t := range f.Tags {
			parts = append(parts, "EXISTS ("+sub+")")
			args = append(args, t, t)
		}
		if f.TagsAll {
			w = append(w, "("+strings.Join(parts, " AND ")+")")
		} else {
			w = append(w, "("+strings.Join(parts, " OR ")+")")
		}
	}
	if len(f.Studios) > 0 {
		in := idList(f.Studios)
		w = append(w, "(s.studio_id IN ("+in+") OR s.studio_id IN (SELECT id FROM studios WHERE parent_id IN ("+in+")))")
	}
	if len(f.Groups) > 0 {
		w = append(w, "EXISTS (SELECT 1 FROM group_scenes gs WHERE gs.scene_id = s.id AND gs.group_id IN ("+idList(f.Groups)+"))")
	}
	if f.RatingMin > 0 {
		w = append(w, "COALESCE(us.rating,0) >= ?")
		args = append(args, f.RatingMin)
	}
	if f.Favorite {
		w = append(w, "COALESCE(us.favorite,0) = 1")
	}
	if f.Unwatched {
		w = append(w, "COALESCE(us.play_count,0) = 0")
	}
	if f.Watched {
		w = append(w, "COALESCE(us.play_count,0) > 0")
	}
	if f.InProgress {
		w = append(w, "COALESCE(us.resume_seconds,0) > 30 AND (s.duration = 0 OR us.resume_seconds < s.duration * 0.95)")
	}
	switch f.VR {
	case "yes":
		w = append(w, "s.projection NOT IN ('', 'flat')")
	case "no":
		w = append(w, "s.projection IN ('', 'flat')")
	}
	if f.MinHeight > 0 {
		// a VR frame's quality is its width (two eyes side by side)
		w = append(w, "MAX(s.height, CASE WHEN s.projection NOT IN ('', 'flat') THEN s.width / 2 ELSE 0 END) >= ?")
		args = append(args, f.MinHeight)
	}
	if f.MaxHeight > 0 {
		w = append(w, "s.height > 0 AND s.height <= ?")
		args = append(args, f.MaxHeight)
	}
	if f.MinDuration > 0 {
		w = append(w, "s.duration >= ?")
		args = append(args, f.MinDuration)
	}
	if f.MaxDuration > 0 {
		w = append(w, "s.duration > 0 AND s.duration <= ?")
		args = append(args, f.MaxDuration)
	}
	switch f.Organized {
	case "yes":
		w = append(w, "s.organized = 1")
	case "no":
		w = append(w, "s.organized = 0")
	}
	if f.HasMarkers {
		w = append(w, "EXISTS (SELECT 1 FROM markers m WHERE m.scene_id = s.id)")
	}
	if f.Folder != "" {
		w = append(w, "(s.path LIKE ? ESCAPE '\\')")
		args = append(args, strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.Trim(f.Folder, "/"))+"/%")
	}
	return strings.Join(w, " AND "), args
}

func sceneOrder(f SceneFilter) string {
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	var e string
	switch f.Sort {
	case "date":
		e = "COALESCE(NULLIF(s.date,''), substr(s.created_at,1,10))"
	case "title":
		e = "lower(CASE WHEN s.title = '' THEN s.path ELSE s.title END)"
	case "rating":
		e = "COALESCE(us.rating,0)"
	case "duration":
		e = "s.duration"
	case "plays":
		e = "COALESCE(us.play_count,0)"
	case "played":
		e = "COALESCE(us.last_played_at,'')"
	case "o":
		e = "COALESCE(us.o_count,0)"
	case "size":
		e = "s.size_bytes"
	case "resolution":
		e = "MAX(s.height, CASE WHEN s.projection NOT IN ('', 'flat') THEN s.width / 2 ELSE 0 END)"
	case "random":
		seed := f.Seed
		if seed <= 0 {
			seed = 1
		}
		return fmt.Sprintf("((s.id * %d + 7919) %% 1000003) ASC, s.id", seed%999983+1)
	case "path":
		e = "s.path"
	case "updated":
		e = "s.updated_at"
	default: // added
		e = "s.created_at"
	}
	return e + " " + dir + ", s.id " + dir
}

// ListScenes returns one page of scenes and the total.
func (s *Store) ListScenes(ctx context.Context, f SceneFilter) ([]Scene, int, error) {
	where, args := sceneWhere(f)
	from := ` FROM scenes s LEFT JOIN user_scenes us ON us.scene_id = s.id AND us.user_id = ? WHERE ` + where
	all := append([]any{f.UserID}, args...)
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+from, all...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 60
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sceneCols+`, `+mineCols+from+` ORDER BY `+sceneOrder(f)+` LIMIT ? OFFSET ?`, append(all, limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	list := []Scene{}
	for rows.Next() {
		sc, err := scanScene(rows, true)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		list = append(list, *sc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, s.fillScenes(ctx, list, false)
}

// SceneIDs returns the ids of a whole filtered list (the player's queue).
func (s *Store) SceneIDs(ctx context.Context, f SceneFilter, limit int) ([]int64, error) {
	where, args := sceneWhere(f)
	rows, err := s.DB.QueryContext(ctx, `SELECT s.id FROM scenes s LEFT JOIN user_scenes us ON us.scene_id = s.id AND us.user_id = ? WHERE `+where+` ORDER BY `+sceneOrder(f)+` LIMIT ?`,
		append(append([]any{f.UserID}, args...), limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ScenePatch changes a scene's facts; nil fields stay as they are.
type ScenePatch struct {
	Title, Code, Details, Director, Date *string
	URLs                                 *[]string
	StudioID                             **int64
	Organized                            *bool
	StashIDs                             *map[string]string
	SourceURL                            *string
	Projection, Stereo                   *string
	FOV                                  *int
	VRSet                                *bool
	Performers, Tags                     *[]int64
}

// UpdateScene applies a patch (relations replaced when given).
func (s *Store) UpdateScene(ctx context.Context, id int64, p ScenePatch) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	set := []string{"updated_at = ?"}
	args := []any{Now()}
	str := func(col string, v *string) {
		if v != nil {
			set = append(set, col+" = ?")
			args = append(args, strings.TrimSpace(*v))
		}
	}
	str("title", p.Title)
	str("code", p.Code)
	str("details", p.Details)
	str("director", p.Director)
	str("date", p.Date)
	str("source_url", p.SourceURL)
	str("projection", p.Projection)
	str("stereo", p.Stereo)
	if p.URLs != nil {
		set = append(set, "urls = ?")
		args = append(args, EncodeList(*p.URLs))
	}
	if p.StudioID != nil {
		set = append(set, "studio_id = ?")
		args = append(args, nullI(*p.StudioID))
	}
	if p.Organized != nil {
		set = append(set, "organized = ?")
		args = append(args, boolInt(*p.Organized))
	}
	if p.StashIDs != nil {
		set = append(set, "stash_ids = ?")
		args = append(args, EncodeMap(*p.StashIDs))
	}
	if p.FOV != nil {
		set = append(set, "fov = ?")
		args = append(args, *p.FOV)
	}
	if p.VRSet != nil {
		set = append(set, "vr_set = ?")
		args = append(args, boolInt(*p.VRSet))
	}
	res, err := tx.ExecContext(ctx, `UPDATE scenes SET `+strings.Join(set, ", ")+` WHERE id = ?`, append(args, id)...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if p.Performers != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM scene_performers WHERE scene_id = ?`, id); err != nil {
			return err
		}
		for _, pid := range *p.Performers {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO scene_performers (scene_id, performer_id) SELECT ?, id FROM performers WHERE id = ?`, id, pid); err != nil {
				return err
			}
		}
	}
	if p.Tags != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM scene_tags WHERE scene_id = ?`, id); err != nil {
			return err
		}
		for _, tid := range *p.Tags {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO scene_tags (scene_id, tag_id) SELECT ?, id FROM tags WHERE id = ?`, id, tid); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// AddSceneRelations adds performers and tags without removing any.
func (s *Store) AddSceneRelations(ctx context.Context, id int64, performers, tags []int64) error {
	for _, pid := range performers {
		if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO scene_performers (scene_id, performer_id) SELECT ?, id FROM performers WHERE id = ?`, id, pid); err != nil {
			return err
		}
	}
	for _, tid := range tags {
		if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO scene_tags (scene_id, tag_id) SELECT ?, id FROM tags WHERE id = ?`, id, tid); err != nil {
			return err
		}
	}
	return nil
}

// SceneFile is what the scanner knows about a file.
type SceneFile struct {
	Path      string
	SizeBytes int64
	Mtime     string
}

// AddScene inserts a new file; it returns the id.
func (s *Store) AddScene(ctx context.Context, f SceneFile, oshash string, addedBy int64) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO scenes (path, size_bytes, file_mtime, oshash, added_by) VALUES (?, ?, ?, ?, ?)`,
		f.Path, f.SizeBytes, f.Mtime, oshash, nullI(&addedBy))
	if err != nil {
		return 0, conflict(err)
	}
	return res.LastInsertId()
}

// SceneIndex is the scanner's view of every scene row.
type SceneIndex struct {
	ID           int64
	Path         string
	SizeBytes    int64
	Mtime        string
	OSHash       string
	MissingSince string
	Trashed      bool
}

// AllSceneFiles lists every scene's file facts.
func (s *Store) AllSceneFiles(ctx context.Context) ([]SceneIndex, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, path, size_bytes, file_mtime, oshash, missing_since, trashed_at != '' FROM scenes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SceneIndex
	for rows.Next() {
		var x SceneIndex
		if err := rows.Scan(&x.ID, &x.Path, &x.SizeBytes, &x.Mtime, &x.OSHash, &x.MissingSince, &x.Trashed); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// MoveScene points a scene at a file's new place (renamed or moved).
func (s *Store) MoveScene(ctx context.Context, id int64, f SceneFile) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET path = ?, size_bytes = ?, file_mtime = ?, missing_since = '', updated_at = ? WHERE id = ?`, f.Path, f.SizeBytes, f.Mtime, Now(), id)
	return conflict(err)
}

// SceneFileChanged notes a changed file: probe and make the pictures again.
func (s *Store) SceneFileChanged(ctx context.Context, id int64, f SceneFile, oshash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET size_bytes = ?, file_mtime = ?, oshash = ?, phash = '', probed_at = '', gen_state = '', missing_since = '', updated_at = ? WHERE id = ?`,
		f.SizeBytes, f.Mtime, oshash, Now(), id)
	return err
}

// SetSceneMissing marks a scene's file as gone (or back).
func (s *Store) SetSceneMissing(ctx context.Context, id int64, missing bool) error {
	v := ""
	if missing {
		v = Now()
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET missing_since = ? WHERE id = ?`, v, id)
	return err
}

// ProbeResult is what ffprobe found.
type ProbeResult struct {
	Duration   float64
	Width      int
	Height     int
	FPS        float64
	VideoCodec string
	AudioCodec string
	Container  string
	Bitrate    int64
}

// SetProbe stores a file's technical facts.
func (s *Store) SetProbe(ctx context.Context, id int64, p ProbeResult) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET duration = ?, width = ?, height = ?, fps = ?, video_codec = ?, audio_codec = ?, container = ?, bitrate = ?, probed_at = ? WHERE id = ?`,
		p.Duration, p.Width, p.Height, p.FPS, p.VideoCodec, p.AudioCodec, p.Container, p.Bitrate, Now(), id)
	return err
}

// SetVR stores the VR shape found from the file (unless chosen by hand).
func (s *Store) SetVR(ctx context.Context, id int64, projection, stereo string, fov int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET projection = ?, stereo = ?, fov = ? WHERE id = ? AND vr_set = 0`, projection, stereo, fov, id)
	return err
}

// SetPHash stores the perceptual hash.
func (s *Store) SetPHash(ctx context.Context, id int64, phash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET phash = ? WHERE id = ?`, phash, id)
	return err
}

// SetSceneCover stores a cover picture path (” removes it).
func (s *Store) SetSceneCover(ctx context.Context, id int64, path, source string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET cover_path = ?, cover_source = ?, updated_at = ? WHERE id = ?`, path, source, Now(), id)
	return err
}

// SetGenerated records the made files of a scene.
func (s *Store) SetGenerated(ctx context.Context, id int64, state, errMsg string, preview, sprite bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET gen_state = ?, gen_error = ?, has_preview = ?, has_sprite = ?, gen_version = gen_version + 1 WHERE id = ?`,
		state, errMsg, boolInt(preview), boolInt(sprite), id)
	return err
}

// ResetGenerated asks for the files to be made again ([] = every scene).
func (s *Store) ResetGenerated(ctx context.Context, ids []int64) error {
	q := `UPDATE scenes SET gen_state = '' WHERE trashed_at = ''`
	if len(ids) > 0 {
		q += ` AND id IN (` + idList(ids) + `)`
	}
	_, err := s.DB.ExecContext(ctx, q)
	return err
}

// WorkItem is a scene that needs background work.
type WorkItem struct {
	ID    int64
	Path  string
	Probe bool // not probed yet
	Gen   bool // pictures/previews to make
	Hash  bool // no perceptual hash yet
}

// NextWork returns scenes that still need probing, pictures or hashing.
func (s *Store) NextWork(ctx context.Context, limit int, skip []int64) ([]WorkItem, error) {
	q := `SELECT id, path, probed_at = '', gen_state = '', phash = '' FROM scenes
		WHERE trashed_at = '' AND missing_since = '' AND (probed_at = '' OR gen_state = '' OR (phash = '' AND gen_state = 'done'))`
	if len(skip) > 0 {
		q += ` AND id NOT IN (` + idList(skip) + `)`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY probed_at != '', id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkItem
	for rows.Next() {
		var w WorkItem
		if err := rows.Scan(&w.ID, &w.Path, &w.Probe, &w.Gen, &w.Hash); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WorkLeft counts scenes still waiting for background work.
func (s *Store) WorkLeft(ctx context.Context) (probe, gen, hash int) {
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(probed_at = ''),0), COALESCE(SUM(gen_state = ''),0), COALESCE(SUM(phash = '' AND gen_state != 'failed'),0)
		FROM scenes WHERE trashed_at = '' AND missing_since = ''`).Scan(&probe, &gen, &hash)
	return
}

// TrashScene marks a scene deleted (its file is in the trash folder).
func (s *Store) TrashScene(ctx context.Context, id int64, trashPath string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET trashed_at = ?, trash_path = ? WHERE id = ?`, Now(), trashPath, id)
	return err
}

// RestoreScene brings a scene back from the trash.
func (s *Store) RestoreScene(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE scenes SET trashed_at = '', trash_path = '', missing_since = '' WHERE id = ?`, id)
	return err
}

// DeleteScene removes a scene row for good.
func (s *Store) DeleteScene(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM scenes WHERE id = ?`, id)
	return err
}

// TrashedBefore lists scenes deleted before the cut-off.
func (s *Store) TrashedBefore(ctx context.Context, before string) ([]Scene, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sceneCols+` FROM scenes s WHERE s.trashed_at != '' AND s.trashed_at < ?`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scene
	for rows.Next() {
		sc, err := scanScene(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *sc)
	}
	return out, rows.Err()
}

// Folders lists the library folders that hold scenes, with counts.
func (s *Store) Folders(ctx context.Context) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT path FROM scenes WHERE trashed_at = ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if i := strings.LastIndex(p, "/"); i > 0 {
			out[p[:i]]++
		}
	}
	return out, rows.Err()
}

// PHashes lists the perceptual hashes (for finding duplicates).
func (s *Store) PHashes(ctx context.Context) (map[int64]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, phash FROM scenes WHERE phash != '' AND trashed_at = ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var h string
		if err := rows.Scan(&id, &h); err != nil {
			return nil, err
		}
		out[id] = h
	}
	return out, rows.Err()
}
