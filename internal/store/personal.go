package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func (s *Store) ensureUserScene(ctx context.Context, uid, sceneID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO user_scenes (user_id, scene_id) SELECT ?, id FROM scenes WHERE id = ?`, uid, sceneID)
	return err
}

// MinePatch changes one person's marks on a scene (nil = unchanged).
type MinePatch struct {
	Rating        *int
	Favorite      *bool
	Hidden        *bool
	ResumeSeconds *float64
	PlayCount     *int
	OCount        *int
}

// PutMine applies one person's marks on a scene.
func (s *Store) PutMine(ctx context.Context, uid, sceneID int64, p MinePatch) error {
	if err := s.ensureUserScene(ctx, uid, sceneID); err != nil {
		return err
	}
	set := []string{"updated_at = ?"}
	args := []any{Now()}
	if p.Rating != nil {
		set = append(set, "rating = ?")
		args = append(args, min(max(*p.Rating, 0), 10))
	}
	if p.Favorite != nil {
		set = append(set, "favorite = ?")
		args = append(args, boolInt(*p.Favorite))
	}
	if p.Hidden != nil {
		set = append(set, "hidden = ?")
		args = append(args, boolInt(*p.Hidden))
	}
	if p.ResumeSeconds != nil {
		set = append(set, "resume_seconds = ?")
		args = append(args, max(*p.ResumeSeconds, 0))
	}
	if p.PlayCount != nil {
		set = append(set, "play_count = ?")
		args = append(args, max(*p.PlayCount, 0))
	}
	if p.OCount != nil {
		set = append(set, "o_count = ?")
		args = append(args, max(*p.OCount, 0))
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE user_scenes SET `+strings.Join(set, ", ")+` WHERE user_id = ? AND scene_id = ?`, append(args, uid, sceneID)...)
	return err
}

// StartPlay opens a sitting with a scene and returns its id.
func (s *Store) StartPlay(ctx context.Context, uid, sceneID int64, device string) (int64, error) {
	if err := s.ensureUserScene(ctx, uid, sceneID); err != nil {
		return 0, err
	}
	now := time.Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO plays (user_id, scene_id, started_at, device, day) VALUES (?, ?, ?, ?, ?)`,
		uid, sceneID, now.UTC().Format(time.RFC3339Nano), device, now.Format("2006-01-02"))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// BeatPlay records watching: seconds watched in this sitting so far and where
// the person is. A sitting counts as a play once countAfter seconds are watched.
// It reports whether this beat made it count.
func (s *Store) BeatPlay(ctx context.Context, uid, playID int64, watched int64, position, duration float64, countAfter int) (bool, error) {
	var sceneID, prev int64
	var counted bool
	if err := s.DB.QueryRowContext(ctx, `SELECT scene_id, seconds, counted FROM plays WHERE id = ? AND user_id = ?`, playID, uid).Scan(&sceneID, &prev, &counted); err != nil {
		return false, notFound(err)
	}
	if watched < prev {
		watched = prev
	}
	delta := watched - prev
	nowCounted := !counted && watched >= int64(max(countAfter, 1))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE plays SET seconds = ?, counted = ? WHERE id = ?`, watched, boolInt(counted || nowCounted), playID); err != nil {
		return false, err
	}
	resume := position
	if duration > 0 && position >= duration*0.95 {
		resume = 0 // finished: start from the beginning next time
	}
	q := `UPDATE user_scenes SET play_seconds = play_seconds + ?, resume_seconds = ?, updated_at = ?`
	args := []any{delta, max(resume, 0), Now()}
	if nowCounted {
		q += `, play_count = play_count + 1, last_played_at = ?`
		args = append(args, Now())
	}
	if _, err := tx.ExecContext(ctx, q+` WHERE user_id = ? AND scene_id = ?`, append(args, uid, sceneID)...); err != nil {
		return false, err
	}
	return nowCounted, tx.Commit()
}

// AddO records one O for a scene (at = ” means now) and returns the new count.
func (s *Store) AddO(ctx context.Context, uid, sceneID int64, at string) (int, error) {
	if err := s.ensureUserScene(ctx, uid, sceneID); err != nil {
		return 0, err
	}
	if at == "" {
		at = Now()
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO o_events (user_id, scene_id, at) VALUES (?, ?, ?)`, uid, sceneID, at); err != nil {
		return 0, err
	}
	return s.syncO(ctx, uid, sceneID)
}

// RemoveO takes back the newest O of a scene.
func (s *Store) RemoveO(ctx context.Context, uid, sceneID int64) (int, error) {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM o_events WHERE id = (SELECT id FROM o_events WHERE user_id = ? AND scene_id = ? ORDER BY at DESC LIMIT 1)`, uid, sceneID); err != nil {
		return 0, err
	}
	return s.syncO(ctx, uid, sceneID)
}

func (s *Store) syncO(ctx context.Context, uid, sceneID int64) (int, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM o_events WHERE user_id = ? AND scene_id = ?`, uid, sceneID).Scan(&n); err != nil {
		return 0, err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE user_scenes SET o_count = ?, updated_at = ? WHERE user_id = ? AND scene_id = ?`, n, Now(), uid, sceneID)
	return n, err
}

// SceneHistory is one person's plays and O's of a scene.
type SceneHistory struct {
	Plays []Play   `json:"plays"`
	Os    []string `json:"os"`
}

// History returns the plays and O's of one scene for one person.
func (s *Store) History(ctx context.Context, uid, sceneID int64) (*SceneHistory, error) {
	h := &SceneHistory{Plays: []Play{}, Os: []string{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, scene_id, started_at, seconds, counted, device, day FROM plays WHERE user_id = ? AND scene_id = ? ORDER BY started_at DESC LIMIT 200`, uid, sceneID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p Play
		if err := rows.Scan(&p.ID, &p.SceneID, &p.StartedAt, &p.Seconds, &p.Counted, &p.Device, &p.Day); err != nil {
			rows.Close()
			return nil, err
		}
		h.Plays = append(h.Plays, p)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT at FROM o_events WHERE user_id = ? AND scene_id = ? ORDER BY at DESC`, uid, sceneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var at string
		if rows.Scan(&at) == nil {
			h.Os = append(h.Os, at)
		}
	}
	return h, rows.Err()
}

// DeletePlay removes one sitting from the history (and its count).
func (s *Store) DeletePlay(ctx context.Context, uid, playID int64) error {
	var sceneID, secs int64
	var counted bool
	if err := s.DB.QueryRowContext(ctx, `SELECT scene_id, seconds, counted FROM plays WHERE id = ? AND user_id = ?`, playID, uid).Scan(&sceneID, &secs, &counted); err != nil {
		return notFound(err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM plays WHERE id = ?`, playID); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE user_scenes SET play_seconds = MAX(play_seconds - ?, 0), play_count = MAX(play_count - ?, 0),
		last_played_at = COALESCE((SELECT MAX(started_at) FROM plays WHERE user_id = ? AND scene_id = ? AND counted = 1), '')
		WHERE user_id = ? AND scene_id = ?`, secs, boolInt(counted), uid, sceneID, uid, sceneID)
	return err
}

// RecentPlays lists one person's latest sittings (all scenes).
func (s *Store) RecentPlays(ctx context.Context, uid int64, limit int) ([]Play, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.scene_id, CASE WHEN s.title = '' THEN s.path ELSE s.title END, p.started_at, p.seconds, p.counted, p.device, p.day
		FROM plays p JOIN scenes s ON s.id = p.scene_id WHERE p.user_id = ? AND p.seconds > 0 ORDER BY p.started_at DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Play{}
	for rows.Next() {
		var p Play
		if err := rows.Scan(&p.ID, &p.SceneID, &p.SceneName, &p.StartedAt, &p.Seconds, &p.Counted, &p.Device, &p.Day); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------- saved filters ----------

// ListFilters returns one person's saved filters.
func (s *Store) ListFilters(ctx context.Context, uid int64) ([]SavedFilter, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, mode, name, query, pinned, created_at FROM saved_filters WHERE user_id = ? ORDER BY mode, name COLLATE NOCASE`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedFilter{}
	for rows.Next() {
		var f SavedFilter
		if err := rows.Scan(&f.ID, &f.Mode, &f.Name, &f.Query, &f.Pinned, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SaveFilter inserts (ID 0) or updates a saved filter.
func (s *Store) SaveFilter(ctx context.Context, uid int64, f *SavedFilter) error {
	if f.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO saved_filters (user_id, mode, name, query, pinned) VALUES (?,?,?,?,?)`, uid, f.Mode, f.Name, f.Query, boolInt(f.Pinned))
		if err != nil {
			return err
		}
		f.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE saved_filters SET name = ?, query = ?, pinned = ? WHERE id = ? AND user_id = ?`, f.Name, f.Query, boolInt(f.Pinned), f.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFilter removes a saved filter.
func (s *Store) DeleteFilter(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM saved_filters WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- headset key ----------

// UserByHeadsetKey finds the person whose headset address has this secret.
func (s *Store) UserByHeadsetKey(ctx context.Context, key string) (*User, error) {
	if len(key) < 16 {
		return nil, ErrNotFound
	}
	b, _ := json.Marshal(key)
	var uid int64
	if err := s.DB.QueryRowContext(ctx, `SELECT user_id FROM user_settings WHERE key = 'headset_key' AND value = ?`, string(b)).Scan(&uid); err != nil {
		return nil, notFound(err)
	}
	return s.GetUser(ctx, uid)
}

// ---------- numbers ----------

// Dashboard is the sidebar's counts for one person.
type Dashboard struct {
	Scenes     int   `json:"scenes"`
	VR         int   `json:"vr"`
	Favorites  int   `json:"favorites"`
	Unwatched  int   `json:"unwatched"`
	InProgress int   `json:"in_progress"`
	Performers int   `json:"performers"`
	Studios    int   `json:"studios"`
	Tags       int   `json:"tags"`
	Groups     int   `json:"groups"`
	Galleries  int   `json:"galleries"`
	Markers    int   `json:"markers"`
	Missing    int   `json:"missing"`
	Trash      int   `json:"trash"`
	Downloads  int   `json:"downloads"` // running or waiting
	Discover   int   `json:"discover"`  // new scenes from follows
	Bytes      int64 `json:"bytes"`
	Seconds    int64 `json:"seconds"`
}

// GetDashboard counts what the sidebar shows.
func (s *Store) GetDashboard(ctx context.Context, uid int64) (*Dashboard, error) {
	d := &Dashboard{}
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1),
		COALESCE(SUM(s.projection NOT IN ('', 'flat')),0),
		COALESCE(SUM(COALESCE(us.favorite,0)),0),
		COALESCE(SUM(COALESCE(us.play_count,0) = 0),0),
		COALESCE(SUM(COALESCE(us.resume_seconds,0) > 30 AND (s.duration = 0 OR us.resume_seconds < s.duration * 0.95)),0),
		COALESCE(SUM(s.missing_since != ''),0),
		COALESCE(SUM(s.size_bytes),0), CAST(COALESCE(SUM(s.duration),0) AS INTEGER)
		FROM scenes s LEFT JOIN user_scenes us ON us.scene_id = s.id AND us.user_id = ?
		WHERE s.trashed_at = '' AND COALESCE(us.hidden,0) = 0`, uid).
		Scan(&d.Scenes, &d.VR, &d.Favorites, &d.Unwatched, &d.InProgress, &d.Missing, &d.Bytes, &d.Seconds)
	if err != nil {
		return nil, err
	}
	for q, dst := range map[string]*int{
		`SELECT COUNT(1) FROM performers`:                         &d.Performers,
		`SELECT COUNT(1) FROM studios`:                            &d.Studios,
		`SELECT COUNT(1) FROM tags`:                               &d.Tags,
		`SELECT COUNT(1) FROM groups_`:                            &d.Groups,
		`SELECT COUNT(1) FROM galleries WHERE missing_since = ''`: &d.Galleries,
		`SELECT COUNT(1) FROM markers mk JOIN scenes s ON s.id = mk.scene_id WHERE s.trashed_at = ''`: &d.Markers,
		`SELECT COUNT(1) FROM scenes WHERE trashed_at != ''`:                                          &d.Trash,
	} {
		_ = s.DB.QueryRowContext(ctx, q).Scan(dst)
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM downloads WHERE user_id = ? AND kind = 'video' AND status IN ('queued','downloading','processing')`, uid).Scan(&d.Downloads)
	d.Discover = s.NewReleaseCount(ctx, uid)
	return d, nil
}

// Stats are one person's numbers over a period.
type Stats struct {
	Plays         int            `json:"plays"`
	Seconds       int64          `json:"seconds"`
	Os            int            `json:"os"`
	Days          []DayStat      `json:"days"`
	TopScenes     []CountRef     `json:"top_scenes"`
	TopPerformers []CountRef     `json:"top_performers"`
	TopTags       []CountRef     `json:"top_tags"`
	TopStudios    []CountRef     `json:"top_studios"`
	Hours         [24]int        `json:"hours"`
	Library       map[string]any `json:"library"`
}

// DayStat is one day's watching.
type DayStat struct {
	Day     string `json:"day"`
	Seconds int64  `json:"seconds"`
	Plays   int    `json:"plays"`
	Os      int    `json:"os"`
}

// CountRef is a named thing with a number.
type CountRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
	Image string `json:"image,omitempty"`
}

// GetStats counts one person's watching since a day.
func (s *Store) GetStats(ctx context.Context, uid int64, since string) (*Stats, error) {
	st := &Stats{Days: []DayStat{}, TopScenes: []CountRef{}, TopPerformers: []CountRef{}, TopTags: []CountRef{}, TopStudios: []CountRef{}}
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(counted),0), COALESCE(SUM(seconds),0) FROM plays WHERE user_id = ? AND day >= ?`, uid, since).Scan(&st.Plays, &st.Seconds); err != nil {
		return nil, err
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM o_events WHERE user_id = ? AND at >= ?`, uid, since).Scan(&st.Os)
	days := map[string]*DayStat{}
	rows, err := s.DB.QueryContext(ctx, `SELECT day, SUM(seconds), SUM(counted) FROM plays WHERE user_id = ? AND day >= ? GROUP BY day`, uid, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d DayStat
		if rows.Scan(&d.Day, &d.Seconds, &d.Plays) == nil {
			days[d.Day] = &d
		}
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT at FROM o_events WHERE user_id = ? AND at >= ?`, uid, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var at string
		if rows.Scan(&at) == nil {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
				day := t.Local().Format("2006-01-02")
				if days[day] == nil {
					days[day] = &DayStat{Day: day}
				}
				days[day].Os++
			}
		}
	}
	rows.Close()
	for d := since; d <= Today(); d = AddDays(d, 1) {
		if x := days[d]; x != nil {
			st.Days = append(st.Days, *x)
		} else {
			st.Days = append(st.Days, DayStat{Day: d})
		}
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT started_at, seconds FROM plays WHERE user_id = ? AND day >= ?`, uid, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var at string
		var secs int64
		if rows.Scan(&at, &secs) == nil {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil && secs > 0 {
				st.Hours[t.Local().Hour()]++
			}
		}
	}
	rows.Close()
	top := func(q string, dst *[]CountRef) error {
		rows, err := s.DB.QueryContext(ctx, q, uid, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c CountRef
			if err := rows.Scan(&c.ID, &c.Name, &c.Count, &c.Image); err != nil {
				return err
			}
			*dst = append(*dst, c)
		}
		return rows.Err()
	}
	if err := top(`SELECT s.id, CASE WHEN s.title = '' THEN s.path ELSE s.title END, SUM(p.seconds), '' FROM plays p JOIN scenes s ON s.id = p.scene_id
		WHERE p.user_id = ? AND p.day >= ? GROUP BY s.id HAVING SUM(p.seconds) > 0 ORDER BY 3 DESC LIMIT 8`, &st.TopScenes); err != nil {
		return nil, err
	}
	if err := top(`SELECT x.id, x.name, SUM(p.seconds), x.image_path FROM plays p JOIN scene_performers sp ON sp.scene_id = p.scene_id JOIN performers x ON x.id = sp.performer_id
		WHERE p.user_id = ? AND p.day >= ? GROUP BY x.id HAVING SUM(p.seconds) > 0 ORDER BY 3 DESC LIMIT 8`, &st.TopPerformers); err != nil {
		return nil, err
	}
	if err := top(`SELECT x.id, x.name, SUM(p.seconds), '' FROM plays p JOIN scene_tags st ON st.scene_id = p.scene_id JOIN tags x ON x.id = st.tag_id
		WHERE p.user_id = ? AND p.day >= ? GROUP BY x.id HAVING SUM(p.seconds) > 0 ORDER BY 3 DESC LIMIT 8`, &st.TopTags); err != nil {
		return nil, err
	}
	if err := top(`SELECT x.id, x.name, SUM(p.seconds), x.image_path FROM plays p JOIN scenes s ON s.id = p.scene_id JOIN studios x ON x.id = s.studio_id
		WHERE p.user_id = ? AND p.day >= ? GROUP BY x.id HAVING SUM(p.seconds) > 0 ORDER BY 3 DESC LIMIT 8`, &st.TopStudios); err != nil {
		return nil, err
	}
	d, err := s.GetDashboard(ctx, uid)
	if err == nil {
		var watched, total int
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(us.play_count > 0),0), COUNT(1) FROM scenes s LEFT JOIN user_scenes us ON us.scene_id = s.id AND us.user_id = ? WHERE s.trashed_at = ''`, uid).Scan(&watched, &total)
		var allOs int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM o_events WHERE user_id = ?`, uid).Scan(&allOs)
		st.Library = map[string]any{"scenes": d.Scenes, "vr": d.VR, "bytes": d.Bytes, "seconds": d.Seconds, "watched": watched, "total": total,
			"performers": d.Performers, "studios": d.Studios, "tags": d.Tags, "markers": d.Markers, "galleries": d.Galleries, "os": allOs}
	}
	return st, nil
}

// lastPlayed returns when a person last watched anything (” = never).
func (s *Store) lastPlayed(ctx context.Context, uid int64) string {
	var at sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(started_at) FROM plays WHERE user_id = ?`, uid).Scan(&at)
	return at.String
}
