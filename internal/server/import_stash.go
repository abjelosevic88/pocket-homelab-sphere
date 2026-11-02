package server

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// Bring a Stash library over: the admin uploads a copy of Stash's database
// (stash-go.sqlite). Scenes are matched to the files here by Stash's own
// fingerprint (oshash), then by path, then by name and size. Everything
// carries over: titles, details, dates, links, studios, performers, tags,
// groups, markers and StashDB ids — and, for the person importing, ratings,
// favourites, O's, play history and where each video was left. Pictures come
// along when Stash keeps them in its database, or when its blobs folder is
// readable here (?blobs=/path). Running it again changes nothing new.

type stashImport struct {
	db       *sql.DB
	blobs    string
	cols     map[string]map[string]bool
	stats    map[string]int
	tagMap   map[int64]int64
	perfMap  map[int64]int64
	studMap  map[int64]int64
	groupMap map[int64]int64
}

func (x *stashImport) has(table, col string) bool {
	if x.cols[table] == nil {
		x.cols[table] = map[string]bool{}
		rows, err := x.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err == nil {
			for rows.Next() {
				var n string
				if rows.Scan(&n) == nil {
					x.cols[table][n] = true
				}
			}
			rows.Close()
		}
	}
	return x.cols[table][col]
}

// col is a column when Stash has it, otherwise a constant.
func (x *stashImport) col(table, col, alias, def string) string {
	if x.has(table, col) {
		return alias + "." + col
	}
	return def
}

func (x *stashImport) blob(checksum sql.NullString) []byte {
	if !checksum.Valid || checksum.String == "" {
		return nil
	}
	var b []byte
	if err := x.db.QueryRow(`SELECT blob FROM blobs WHERE checksum = ?`, checksum.String).Scan(&b); err == nil && len(b) > 0 {
		return b
	}
	if x.blobs != "" && len(checksum.String) > 4 {
		c := checksum.String
		for _, p := range []string{filepath.Join(x.blobs, c[:2], c[2:4], c), filepath.Join(x.blobs, c)} {
			if b, err := os.ReadFile(p); err == nil {
				return b
			}
		}
	}
	return nil
}

func (x *stashImport) strings(q string, args ...any) []string {
	rows, err := x.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v sql.NullString
		if rows.Scan(&v) == nil && v.String != "" {
			out = append(out, v.String)
		}
	}
	return out
}

func (s *Server) handleImportStash(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	ctx := r.Context()
	tmp, err := os.CreateTemp(s.cfg.DataDir, ".stash-import-*.sqlite")
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, http.MaxBytesReader(w, r.Body, 4<<30)); err != nil {
		tmp.Close()
		writeErr(w, http.StatusBadRequest, "the upload did not finish")
		return
	}
	tmp.Close()
	db, err := sql.Open("sqlite", "file:"+tmp.Name()+"?mode=ro&immutable=1")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "not a database")
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM scenes`).Scan(&n); err != nil {
		writeErr(w, http.StatusBadRequest, "this is not a Stash database (stash-go.sqlite)")
		return
	}
	blobs := strings.TrimSpace(r.URL.Query().Get("blobs"))
	// the library has to be read first so the files can be matched
	ictx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	defer cancel()
	_ = s.scanAll(ictx, nil)
	x := &stashImport{db: db, blobs: blobs, cols: map[string]map[string]bool{}, stats: map[string]int{},
		tagMap: map[int64]int64{}, perfMap: map[int64]int64{}, studMap: map[int64]int64{}, groupMap: map[int64]int64{}}
	if err := s.importStash(ictx, userFrom(ctx), x, r.URL.Query().Get("root")); err != nil {
		s.fail(w, err, "import")
		return
	}
	s.wakeWork()
	writeJSON(w, http.StatusOK, x.stats)
}

func (s *Server) importStash(ctx context.Context, u *store.User, x *stashImport, root string) error {
	// ---- tags ----
	rows, err := x.db.Query(`SELECT t.id, t.name, ` + x.col("tags", "description", "t", "''") + `, ` + x.col("tags", "image_blob", "t", "NULL") + `, ` + x.col("tags", "favorite", "t", "0") + ` FROM tags t`)
	if err != nil {
		return err
	}
	type tagRow struct {
		id       int64
		name     string
		desc     sql.NullString
		blob     sql.NullString
		favorite bool
	}
	var tags []tagRow
	for rows.Next() {
		var t tagRow
		if rows.Scan(&t.id, &t.name, &t.desc, &t.blob, &t.favorite) == nil {
			tags = append(tags, t)
		}
	}
	rows.Close()
	for _, t := range tags {
		id, created, err := s.store.EnsureTag(ctx, t.name)
		if err != nil {
			continue
		}
		x.tagMap[t.id] = id
		if created {
			x.stats["tags"]++
			tg, _ := s.store.GetTag(ctx, 0, id)
			if tg != nil {
				tg.Description = t.desc.String
				tg.Aliases = strings.Join(x.strings(`SELECT alias FROM tag_aliases WHERE tag_id = ?`, t.id), ", ")
				_ = s.store.SaveTag(ctx, tg)
			}
			if b := x.blob(t.blob); b != nil {
				_ = s.setImage(ctx, "tags", id, b)
			}
		}
		if t.favorite {
			_ = s.store.SetMark(ctx, u.ID, "tag", id, boolp(true), nil)
		}
	}
	if x.has("tags_relations", "parent_id") {
		rows, err := x.db.Query(`SELECT parent_id, child_id FROM tags_relations`)
		if err == nil {
			var pairs [][2]int64
			for rows.Next() {
				var p, c int64
				if rows.Scan(&p, &c) == nil {
					pairs = append(pairs, [2]int64{p, c})
				}
			}
			rows.Close()
			for _, pc := range pairs {
				if p, c := x.tagMap[pc[0]], x.tagMap[pc[1]]; p > 0 && c > 0 && p != c {
					_, _ = s.store.DB.ExecContext(ctx, `UPDATE tags SET parent_id = ? WHERE id = ? AND parent_id IS NULL`, p, c)
				}
			}
		}
	}

	// ---- studios (parents first: two passes) ----
	rows, err = x.db.Query(`SELECT x.id, x.name, x.parent_id, ` + x.col("studios", "details", "x", "''") + `, ` + x.col("studios", "image_blob", "x", "NULL") + `,
		` + x.col("studios", "favorite", "x", "0") + `, ` + x.col("studios", "rating", "x", "NULL") + `, ` + x.col("studios", "url", "x", "''") + ` FROM studios x`)
	if err != nil {
		return err
	}
	type studRow struct {
		id       int64
		name     string
		parent   sql.NullInt64
		details  sql.NullString
		blob     sql.NullString
		favorite bool
		rating   sql.NullInt64
		url      sql.NullString
	}
	var studs []studRow
	for rows.Next() {
		var st studRow
		if rows.Scan(&st.id, &st.name, &st.parent, &st.details, &st.blob, &st.favorite, &st.rating, &st.url) == nil {
			studs = append(studs, st)
		}
	}
	rows.Close()
	for _, st := range studs {
		id, created, err := s.store.EnsureStudio(ctx, st.name)
		if err != nil {
			continue
		}
		x.studMap[st.id] = id
		if created {
			x.stats["studios"]++
			if b := x.blob(st.blob); b != nil {
				_ = s.setImage(ctx, "studios", id, b)
			}
		}
		if cur, err := s.store.GetStudio(ctx, 0, id); err == nil {
			if cur.Details == "" {
				cur.Details = st.details.String
			}
			if cur.URL == "" {
				cur.URL = st.url.String
			}
			if cur.Aliases == "" && x.has("studio_aliases", "alias") {
				cur.Aliases = strings.Join(x.strings(`SELECT alias FROM studio_aliases WHERE studio_id = ?`, st.id), ", ")
			}
			if x.has("studio_stash_ids", "stash_id") {
				rows, err := x.db.Query(`SELECT endpoint, stash_id FROM studio_stash_ids WHERE studio_id = ?`, st.id)
				if err == nil {
					for rows.Next() {
						var ep, sid string
						if rows.Scan(&ep, &sid) == nil {
							cur.StashIDs[ep] = sid
						}
					}
					rows.Close()
				}
			}
			_ = s.store.SaveStudio(ctx, cur)
		}
		var rating *int
		if st.rating.Valid {
			v := int((st.rating.Int64 + 5) / 10)
			rating = &v
		}
		if st.favorite || rating != nil {
			_ = s.store.SetMark(ctx, u.ID, "studio", id, boolp(st.favorite), rating)
		}
	}
	for _, st := range studs {
		if st.parent.Valid {
			if p, c := x.studMap[st.parent.Int64], x.studMap[st.id]; p > 0 && c > 0 && p != c {
				_, _ = s.store.DB.ExecContext(ctx, `UPDATE studios SET parent_id = ? WHERE id = ? AND parent_id IS NULL`, p, c)
			}
		}
	}

	// ---- performers ----
	pc := func(c string) string { return x.col("performers", c, "p", "NULL") }
	rows, err = x.db.Query(`SELECT p.id, p.name, ` + strings.Join([]string{pc("disambiguation"), pc("gender"), pc("birthdate"), pc("death_date"), pc("country"),
		pc("ethnicity"), pc("hair_color"), pc("eye_color"), pc("height"), pc("weight"), pc("measurements"), pc("fake_tits"), pc("tattoos"), pc("piercings"),
		pc("career_length"), pc("career_start"), pc("career_end"), pc("details"), pc("image_blob"), pc("favorite"), pc("rating")}, ", ") + ` FROM performers p`)
	if err != nil {
		return err
	}
	type perfRow struct {
		id                                                                  int64
		name                                                                string
		dis, gender, birth, death, country, eth, hair, eye                  sql.NullString
		height, weight                                                      sql.NullInt64
		meas, fake, tattoos, piercings, career, cstart, cend, details, blob sql.NullString
		favorite                                                            sql.NullBool
		rating                                                              sql.NullInt64
	}
	var perfs []perfRow
	for rows.Next() {
		var p perfRow
		if rows.Scan(&p.id, &p.name, &p.dis, &p.gender, &p.birth, &p.death, &p.country, &p.eth, &p.hair, &p.eye, &p.height, &p.weight,
			&p.meas, &p.fake, &p.tattoos, &p.piercings, &p.career, &p.cstart, &p.cend, &p.details, &p.blob, &p.favorite, &p.rating) == nil {
			perfs = append(perfs, p)
		}
	}
	rows.Close()
	for _, p := range perfs {
		var id int64
		// same name and disambiguation, or create
		err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM performers WHERE name = ? COLLATE NOCASE AND disambiguation = ?`, p.name, p.dis.String).Scan(&id)
		created := false
		if err != nil {
			np := &store.Performer{Name: p.name, Disambiguation: p.dis.String}
			if err := s.store.SavePerformer(ctx, np); err != nil {
				continue
			}
			id, created = np.ID, true
			x.stats["performers"]++
		}
		x.perfMap[p.id] = id
		if created {
			cur, err := s.store.GetPerformer(ctx, 0, id)
			if err == nil {
				cur.Gender = strings.ToLower(p.gender.String)
				cur.Birthdate, cur.DeathDate = p.birth.String, p.death.String
				cur.Country, cur.Ethnicity = p.country.String, strings.ToLower(p.eth.String)
				cur.HairColor, cur.EyeColor = strings.ToLower(p.hair.String), strings.ToLower(p.eye.String)
				cur.HeightCM, cur.WeightKG = int(p.height.Int64), int(p.weight.Int64)
				cur.Measurements, cur.FakeTits, cur.Tattoos, cur.Piercings = p.meas.String, p.fake.String, p.tattoos.String, p.piercings.String
				cur.Career = p.career.String
				if cur.Career == "" && p.cstart.String != "" {
					cur.Career = strings.TrimSpace(firstN(p.cstart.String, 4) + " - " + firstN(p.cend.String, 4))
				}
				cur.Details = p.details.String
				cur.Aliases = strings.Join(x.strings(`SELECT alias FROM performer_aliases WHERE performer_id = ?`, p.id), ", ")
				if x.has("performer_urls", "url") {
					cur.URLs = x.strings(`SELECT url FROM performer_urls WHERE performer_id = ? ORDER BY position`, p.id)
				}
				if x.has("performer_stash_ids", "stash_id") {
					rows, err := x.db.Query(`SELECT endpoint, stash_id FROM performer_stash_ids WHERE performer_id = ?`, p.id)
					if err == nil {
						for rows.Next() {
							var ep, sid string
							if rows.Scan(&ep, &sid) == nil {
								cur.StashIDs[ep] = sid
							}
						}
						rows.Close()
					}
				}
				_ = s.store.SavePerformer(ctx, cur)
			}
			if b := x.blob(p.blob); b != nil {
				_ = s.setImage(ctx, "performers", id, b)
			}
		}
		var rating *int
		if p.rating.Valid {
			v := int((p.rating.Int64 + 5) / 10)
			rating = &v
		}
		if p.favorite.Bool || rating != nil {
			_ = s.store.SetMark(ctx, u.ID, "performer", id, boolp(p.favorite.Bool), rating)
		}
	}

	// ---- groups ----
	groupTable := "groups"
	if !x.has("groups", "name") && x.has("movies", "name") {
		groupTable = "movies"
	}
	if x.has(groupTable, "name") {
		gc := func(c string) string { return x.col(groupTable, c, "g", "NULL") }
		rows, err := x.db.Query(`SELECT g.id, g.name, ` + strings.Join([]string{gc("aliases"), gc("date"), gc("director"), gc("description"), gc("synopsis"), gc("studio_id"), gc("front_image_blob"), gc("back_image_blob"), gc("rating")}, ", ") + ` FROM ` + groupTable + ` g`)
		if err == nil {
			type grpRow struct {
				id                                 int64
				name                               string
				aliases, date, director, desc, syn sql.NullString
				studio                             sql.NullInt64
				front, back                        sql.NullString
				rating                             sql.NullInt64
			}
			var groups []grpRow
			for rows.Next() {
				var g grpRow
				if rows.Scan(&g.id, &g.name, &g.aliases, &g.date, &g.director, &g.desc, &g.syn, &g.studio, &g.front, &g.back, &g.rating) == nil {
					groups = append(groups, g)
				}
			}
			rows.Close()
			for _, g := range groups {
				var id int64
				if s.store.DB.QueryRowContext(ctx, `SELECT id FROM groups_ WHERE name = ?`, g.name).Scan(&id) != nil {
					ng := &store.Group{Name: g.name, Aliases: g.aliases.String, Date: g.date.String, Director: g.director.String, Synopsis: firstNonEmpty(g.desc.String, g.syn.String)}
					if sid := x.studMap[g.studio.Int64]; sid > 0 {
						ng.StudioID = &sid
					}
					if err := s.store.SaveGroup(ctx, ng); err != nil {
						continue
					}
					id = ng.ID
					x.stats["groups"]++
					for back, blob := range map[bool]sql.NullString{false: g.front, true: g.back} {
						if b := x.blob(blob); b != nil {
							if rel, err := s.savePicture("groups", b, 1600); err == nil {
								_ = s.store.SetGroupImage(ctx, id, back, rel)
							}
						}
					}
				}
				x.groupMap[g.id] = id
				if g.rating.Valid {
					v := int((g.rating.Int64 + 5) / 10)
					_ = s.store.SetMark(ctx, u.ID, "group", id, nil, &v)
				}
			}
		}
	}

	// ---- scenes ----
	return s.importStashScenes(ctx, u, x, root)
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *Server) importStashScenes(ctx context.Context, u *store.User, x *stashImport, root string) error {
	sc := func(c string) string { return x.col("scenes", c, "s", "NULL") }
	primary := ""
	if x.has("scenes_files", "primary") {
		primary = ` AND sf."primary" = 1`
	}
	oshash := "NULL"
	if x.has("files_fingerprints", "fingerprint") {
		oshash = `(SELECT fingerprint FROM files_fingerprints fp WHERE fp.file_id = f.id AND fp.type = 'oshash')`
	}
	rows, err := x.db.Query(`SELECT s.id, ` + strings.Join([]string{sc("title"), sc("details"), sc("date"), sc("code"), sc("director"), sc("rating"),
		sc("studio_id"), sc("organized"), sc("resume_time"), sc("play_duration"), sc("cover_blob"), sc("o_counter")}, ", ") + `,
		f.basename, fo.path, f.size, ` + oshash + `
		FROM scenes s JOIN scenes_files sf ON sf.scene_id = s.id` + primary + ` JOIN files f ON f.id = sf.file_id JOIN folders fo ON fo.id = f.parent_folder_id`)
	if err != nil {
		return err
	}
	type row struct {
		id                                   int64
		title, details, date, code, director sql.NullString
		rating, studio                       sql.NullInt64
		organized                            sql.NullBool
		resume, playDur                      sql.NullFloat64
		cover                                sql.NullString
		oCounter                             sql.NullInt64
		base, folder                         string
		size                                 int64
		oshash                               sql.NullString
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title, &r.details, &r.date, &r.code, &r.director, &r.rating, &r.studio, &r.organized, &r.resume, &r.playDur,
			&r.cover, &r.oCounter, &r.base, &r.folder, &r.size, &r.oshash); err == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	// Stash's library root: the longest folder all its scenes share
	if root == "" && len(list) > 0 {
		root = list[0].folder
		for _, r := range list[1:] {
			for root != "" && root != "/" && !strings.HasPrefix(r.folder+"/", strings.TrimRight(root, "/")+"/") {
				root = path.Dir(root)
			}
		}
	}
	root = strings.TrimRight(root, "/")
	idx, err := s.store.AllSceneFiles(ctx)
	if err != nil {
		return err
	}
	byPath, byHash, byNameSize := map[string]int64{}, map[string]int64{}, map[string]int64{}
	for _, f := range idx {
		if f.Trashed {
			continue
		}
		byPath[f.Path] = f.ID
		if f.OSHash != "" {
			byHash[f.OSHash] = f.ID
		}
		byNameSize[fmt.Sprintf("%s|%d", path.Base(f.Path), f.SizeBytes)] = f.ID
	}
	for _, r := range list {
		rel := strings.TrimPrefix(strings.TrimPrefix(r.folder, root), "/")
		rel = strings.TrimPrefix(path.Join(rel, r.base), "/")
		id := byHash[r.oshash.String]
		if id == 0 {
			id = byPath[rel]
		}
		if id == 0 {
			id = byNameSize[fmt.Sprintf("%s|%d", r.base, r.size)]
		}
		if id == 0 {
			x.stats["not_found"]++
			continue
		}
		x.stats["scenes"]++
		cur, err := s.store.GetScene(ctx, u.ID, id)
		if err != nil {
			continue
		}
		p := store.ScenePatch{}
		keep := func(dst **string, have, v string) {
			if have == "" && strings.TrimSpace(v) != "" {
				*dst = strp(v)
			}
		}
		keep(&p.Title, cur.Title, r.title.String)
		keep(&p.Details, cur.Details, r.details.String)
		if validLooseDate(r.date.String) {
			keep(&p.Date, cur.Date, r.date.String)
		}
		keep(&p.Code, cur.Code, r.code.String)
		keep(&p.Director, cur.Director, r.director.String)
		if r.organized.Bool && !cur.Organized {
			p.Organized = boolp(true)
		}
		if cur.StudioID == nil {
			if sid := x.studMap[r.studio.Int64]; sid > 0 {
				ps := &sid
				p.StudioID = &ps
			}
		}
		if x.has("scene_urls", "url") {
			if urls := x.strings(`SELECT url FROM scene_urls WHERE scene_id = ? ORDER BY position`, r.id); len(urls) > 0 {
				all := append(append([]string{}, cur.URLs...), urls...)
				p.URLs = &all
			}
		}
		if x.has("scene_stash_ids", "stash_id") {
			ids := map[string]string{}
			for k, v := range cur.StashIDs {
				ids[k] = v
			}
			rows, err := x.db.Query(`SELECT endpoint, stash_id FROM scene_stash_ids WHERE scene_id = ?`, r.id)
			if err == nil {
				for rows.Next() {
					var ep, sid string
					if rows.Scan(&ep, &sid) == nil {
						ids[ep] = sid
					}
				}
				rows.Close()
			}
			p.StashIDs = &ids
		}
		_ = s.store.UpdateScene(ctx, id, p)
		var perfs, tags []int64
		for _, v := range x.strings(`SELECT performer_id FROM performers_scenes WHERE scene_id = ?`, r.id) {
			var pid int64
			fmt.Sscan(v, &pid)
			if m := x.perfMap[pid]; m > 0 {
				perfs = append(perfs, m)
			}
		}
		for _, v := range x.strings(`SELECT tag_id FROM scenes_tags WHERE scene_id = ?`, r.id) {
			var tid int64
			fmt.Sscan(v, &tid)
			if m := x.tagMap[tid]; m > 0 {
				tags = append(tags, m)
			}
		}
		_ = s.store.AddSceneRelations(ctx, id, perfs, tags)
		// groups
		gtable, gcol := "groups_scenes", "group_id"
		if !x.has(gtable, gcol) && x.has("movies_scenes", "movie_id") {
			gtable, gcol = "movies_scenes", "movie_id"
		}
		if x.has(gtable, gcol) {
			grs, err := x.db.Query(`SELECT `+gcol+`, COALESCE(scene_index, 0) FROM `+gtable+` WHERE scene_id = ?`, r.id)
			if err == nil {
				var pairs [][2]int64
				for grs.Next() {
					var g, i int64
					if grs.Scan(&g, &i) == nil {
						pairs = append(pairs, [2]int64{g, i})
					}
				}
				grs.Close()
				for _, gi := range pairs {
					if gid := x.groupMap[gi[0]]; gid > 0 {
						_, _ = s.store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO group_scenes (group_id, scene_id, scene_index) VALUES (?, ?, ?)`, gid, id, gi[1])
					}
				}
			}
		}
		// markers
		if x.has("scene_markers", "seconds") {
			end := x.col("scene_markers", "end_seconds", "m", "NULL")
			mrs, err := x.db.Query(`SELECT m.title, m.seconds, `+end+`, m.primary_tag_id FROM scene_markers m WHERE m.scene_id = ?`, r.id)
			if err == nil {
				type mk struct {
					title string
					secs  float64
					end   sql.NullFloat64
					tag   sql.NullInt64
				}
				var ms []mk
				for mrs.Next() {
					var m mk
					if mrs.Scan(&m.title, &m.secs, &m.end, &m.tag) == nil {
						ms = append(ms, m)
					}
				}
				mrs.Close()
				for _, m := range ms {
					var exists int
					_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM markers WHERE scene_id = ? AND abs(seconds - ?) < 0.5 AND title = ?`, id, m.secs, m.title).Scan(&exists)
					if exists > 0 {
						continue
					}
					nm := &store.Marker{SceneID: id, Title: m.title, Seconds: m.secs}
					if m.end.Valid {
						nm.EndSeconds = &m.end.Float64
					}
					if t := x.tagMap[m.tag.Int64]; t > 0 {
						nm.PrimaryTagID = &t
					}
					if s.store.SaveMarker(ctx, nm, u.ID) == nil {
						x.stats["markers"]++
					}
				}
			}
		}
		// the cover, when ours is only a frame of the video
		if cur.CoverSource == "" || cur.CoverSource == "frame" {
			if b := x.blob(r.cover); b != nil {
				if s.setSceneCover(ctx, cur, b, "stash") == nil {
					x.stats["covers"]++
				}
			}
		}
		// ---- the importing person's own ----
		mine := store.MinePatch{}
		if r.rating.Valid && r.rating.Int64 > 0 {
			v := int((r.rating.Int64 + 5) / 10)
			mine.Rating = &v
		}
		if r.resume.Valid && r.resume.Float64 > 0 && cur.Mine.ResumeSeconds == 0 {
			mine.ResumeSeconds = &r.resume.Float64
		}
		_ = s.store.PutMine(ctx, u.ID, id, mine)
		if x.has("scenes_o_dates", "o_date") {
			for _, at := range x.strings(`SELECT o_date FROM scenes_o_dates WHERE scene_id = ?`, r.id) {
				var exists int
				_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM o_events WHERE user_id = ? AND scene_id = ? AND at = ?`, u.ID, id, normTime(at)).Scan(&exists)
				if exists == 0 {
					if _, err := s.store.AddO(ctx, u.ID, id, normTime(at)); err == nil {
						x.stats["os"]++
					}
				}
			}
		}
		if x.has("scenes_view_dates", "view_date") {
			views := x.strings(`SELECT view_date FROM scenes_view_dates WHERE scene_id = ? ORDER BY view_date`, r.id)
			for i, at := range views {
				at = normTime(at)
				var exists int
				_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM plays WHERE user_id = ? AND scene_id = ? AND started_at = ?`, u.ID, id, at).Scan(&exists)
				if exists > 0 {
					continue
				}
				secs := int64(0)
				if i == len(views)-1 && r.playDur.Valid {
					secs = int64(r.playDur.Float64) // Stash keeps one total; it goes on the last sitting
				}
				day := at
				if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
					day = t.Local().Format("2006-01-02")
				}
				if len(day) > 10 {
					day = day[:10]
				}
				_, _ = s.store.DB.ExecContext(ctx, `INSERT INTO plays (user_id, scene_id, started_at, seconds, counted, device, day) VALUES (?, ?, ?, ?, 1, 'stash', ?)`, u.ID, id, at, secs, day)
				_, _ = s.store.DB.ExecContext(ctx, `UPDATE user_scenes SET play_count = play_count + 1, play_seconds = play_seconds + ?,
					last_played_at = MAX(last_played_at, ?) WHERE user_id = ? AND scene_id = ?`, secs, at, u.ID, id)
				x.stats["plays"]++
			}
		}
	}
	return nil
}

// normTime turns Stash's timestamps into the RFC 3339 form used here.
func normTime(v string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	}
	return v
}
