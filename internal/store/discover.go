package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Wanted is a scene someone wants and does not have yet.
type Wanted struct {
	ID           int64           `json:"id"`
	Endpoint     string          `json:"endpoint"`
	RemoteID     string          `json:"remote_id"`
	Title        string          `json:"title"`
	ReleaseDate  string          `json:"release_date"`
	Scene        json.RawMessage `json:"scene"`
	FoundSceneID *int64          `json:"found_scene_id"`
	FoundAt      string          `json:"found_at"`
	CreatedAt    string          `json:"created_at"`
}

// AddWanted keeps a scene on a person's wishlist (again: no change).
func (s *Store) AddWanted(ctx context.Context, uid int64, w *Wanted) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO wanted (user_id, endpoint, remote_id, title, release_date, scene_json) VALUES (?,?,?,?,?,?)
		ON CONFLICT(user_id, endpoint, remote_id) DO UPDATE SET title = excluded.title, release_date = excluded.release_date, scene_json = excluded.scene_json`,
		uid, w.Endpoint, w.RemoteID, w.Title, w.ReleaseDate, string(w.Scene))
	if err != nil {
		return err
	}
	return s.DB.QueryRowContext(ctx, `SELECT id FROM wanted WHERE user_id = ? AND endpoint = ? AND remote_id = ?`, uid, w.Endpoint, w.RemoteID).Scan(&w.ID)
}

// ListWanted returns a person's wishlist, newest first (found ones last).
func (s *Store) ListWanted(ctx context.Context, uid int64) ([]Wanted, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, endpoint, remote_id, title, release_date, scene_json, found_scene_id, found_at, created_at
		FROM wanted WHERE user_id = ? ORDER BY found_at != '', created_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Wanted{}
	for rows.Next() {
		var w Wanted
		var sj string
		var found sql.NullInt64
		if err := rows.Scan(&w.ID, &w.Endpoint, &w.RemoteID, &w.Title, &w.ReleaseDate, &sj, &found, &w.FoundAt, &w.CreatedAt); err != nil {
			return nil, err
		}
		w.Scene = json.RawMessage(sj)
		if found.Valid {
			w.FoundSceneID = &found.Int64
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteWanted takes a scene off a person's wishlist.
func (s *Store) DeleteWanted(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM wanted WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// WantedKeys maps "endpoint|remote id" to wishlist ids for one person.
func (s *Store) WantedKeys(ctx context.Context, uid int64) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, endpoint, remote_id FROM wanted WHERE user_id = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var ep, rid string
		if err := rows.Scan(&id, &ep, &rid); err != nil {
			return nil, err
		}
		out[ep+"|"+rid] = id
	}
	return out, rows.Err()
}

// MarkWantedFound ticks off everyone's wish for this remote scene.
func (s *Store) MarkWantedFound(ctx context.Context, endpoint, remoteID string, sceneID int64) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE wanted SET found_scene_id = ?, found_at = ? WHERE endpoint = ? AND remote_id = ? AND found_at = ''`, sceneID, Now(), endpoint, remoteID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LibraryKeys maps "endpoint|remote id" (from the scenes' stash ids) and
// normalized titles to scene ids: what the library already has.
func (s *Store) LibraryKeys(ctx context.Context) (map[string]int64, map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, stash_ids, title FROM scenes WHERE trashed_at = ''`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ids, titles := map[string]int64{}, map[string]int64{}
	for rows.Next() {
		var id int64
		var sj, title string
		if err := rows.Scan(&id, &sj, &title); err != nil {
			return nil, nil, err
		}
		for ep, rid := range decodeMap(sj) {
			ids[ep+"|"+rid] = id
		}
		if t := NormTitle(title); len(t) >= 6 {
			titles[t] = id
		}
	}
	return ids, titles, rows.Err()
}

// NormTitle is a title made comparable: lower-case letters and digits only.
func NormTitle(t string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(t) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------- follows ----------

// Follow is a performer or studio someone follows.
type Follow struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"-"`
	Kind          string `json:"kind"`
	ItemID        int64  `json:"item_id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	LastCheckedAt string `json:"last_checked_at"`
	LastError     string `json:"last_error"`
	CreatedAt     string `json:"created_at"`
	New           int    `json:"new"`
}

const followSelect = `SELECT f.id, f.user_id, f.kind, f.item_id, f.last_checked_at, f.last_error, f.created_at,
	COALESCE(p.name, x.name, '') AS name, COALESCE(p.image_path, x.image_path, ''),
	(SELECT COUNT(1) FROM releases r WHERE r.follow_id = f.id AND r.dismissed = 0)
	FROM follows f
	LEFT JOIN performers p ON f.kind = 'performer' AND p.id = f.item_id
	LEFT JOIN studios x ON f.kind = 'studio' AND x.id = f.item_id`

func scanFollows(rows *sql.Rows) ([]Follow, error) {
	defer rows.Close()
	out := []Follow{}
	for rows.Next() {
		var f Follow
		if err := rows.Scan(&f.ID, &f.UserID, &f.Kind, &f.ItemID, &f.LastCheckedAt, &f.LastError, &f.CreatedAt, &f.Name, &f.Image, &f.New); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListFollows returns a person's follows (uid 0 = everyone's, for the checker).
func (s *Store) ListFollows(ctx context.Context, uid int64) ([]Follow, error) {
	q, args := followSelect, []any{}
	if uid > 0 {
		q += ` WHERE f.user_id = ?`
		args = append(args, uid)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY f.kind, name COLLATE NOCASE`, args...)
	if err != nil {
		return nil, err
	}
	return scanFollows(rows)
}

// GetFollow loads one follow of a person.
func (s *Store) GetFollow(ctx context.Context, uid, id int64) (*Follow, error) {
	rows, err := s.DB.QueryContext(ctx, followSelect+` WHERE f.id = ? AND f.user_id = ?`, id, uid)
	if err != nil {
		return nil, err
	}
	list, err := scanFollows(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// FollowOf returns the id of a person's follow of a performer/studio (0 = none).
func (s *Store) FollowOf(ctx context.Context, uid int64, kind string, itemID int64) int64 {
	var id int64
	_ = s.DB.QueryRowContext(ctx, `SELECT id FROM follows WHERE user_id = ? AND kind = ? AND item_id = ?`, uid, kind, itemID).Scan(&id)
	return id
}

// AddFollow follows a performer or studio (again: the same follow).
func (s *Store) AddFollow(ctx context.Context, uid int64, kind string, itemID int64) (int64, error) {
	if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO follows (user_id, kind, item_id) VALUES (?, ?, ?)`, uid, kind, itemID); err != nil {
		return 0, err
	}
	return s.FollowOf(ctx, uid, kind, itemID), nil
}

// DeleteFollow stops following.
func (s *Store) DeleteFollow(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM follows WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FollowChecked records a check.
func (s *Store) FollowChecked(ctx context.Context, id int64, errMsg string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE follows SET last_checked_at = ?, last_error = ? WHERE id = ?`, Now(), errMsg, id)
	return err
}

// FollowSeen returns the remote scenes a follow has already looked at.
func (s *Store) FollowSeen(ctx context.Context, id int64) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT remote_id FROM follow_seen WHERE follow_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var r string
		if rows.Scan(&r) == nil {
			out[r] = true
		}
	}
	return out, rows.Err()
}

// MarkFollowSeen remembers remote scenes a follow has looked at.
func (s *Store) MarkFollowSeen(ctx context.Context, id int64, remote []string) error {
	for _, r := range remote {
		if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO follow_seen (follow_id, remote_id) VALUES (?, ?)`, id, r); err != nil {
			return err
		}
	}
	return nil
}

// ---------- new releases ----------

// Release is a new scene found by following.
type Release struct {
	ID          int64           `json:"id"`
	FollowID    *int64          `json:"follow_id"`
	Endpoint    string          `json:"endpoint"`
	RemoteID    string          `json:"remote_id"`
	ReleaseDate string          `json:"release_date"`
	Scene       json.RawMessage `json:"scene"`
	Dismissed   bool            `json:"dismissed"`
	CreatedAt   string          `json:"created_at"`
}

// AddRelease records a new scene (once per person).
func (s *Store) AddRelease(ctx context.Context, uid, followID int64, r *Release) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO releases (user_id, follow_id, endpoint, remote_id, release_date, scene_json) VALUES (?,?,?,?,?,?)`,
		uid, followID, r.Endpoint, r.RemoteID, r.ReleaseDate, string(r.Scene))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListReleases returns a person's new scenes (dismissed too when asked).
func (s *Store) ListReleases(ctx context.Context, uid int64, all bool) ([]Release, error) {
	q := `SELECT id, follow_id, endpoint, remote_id, release_date, scene_json, dismissed, created_at FROM releases WHERE user_id = ?`
	if !all {
		q += ` AND dismissed = 0`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY release_date DESC, id DESC LIMIT 500`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Release{}
	for rows.Next() {
		var r Release
		var sj string
		var f sql.NullInt64
		if err := rows.Scan(&r.ID, &f, &r.Endpoint, &r.RemoteID, &r.ReleaseDate, &sj, &r.Dismissed, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Scene = json.RawMessage(sj)
		if f.Valid {
			r.FollowID = &f.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DismissReleases hides new scenes (id 0 = all of the person's).
func (s *Store) DismissReleases(ctx context.Context, uid, id int64) error {
	q, args := `UPDATE releases SET dismissed = 1 WHERE user_id = ?`, []any{uid}
	if id > 0 {
		q += ` AND id = ?`
		args = append(args, id)
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

// NewReleaseCount is how many new scenes wait for a person.
func (s *Store) NewReleaseCount(ctx context.Context, uid int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM releases WHERE user_id = ? AND dismissed = 0`, uid).Scan(&n)
	return n
}
