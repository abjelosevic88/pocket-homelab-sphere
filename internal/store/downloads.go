package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

const downloadCols = `id, user_id, parent_id, subscription_id, url, kind, title, thumbnail, options, status, percent, speed, eta,
	size_bytes, file_path, scene_id, error, created_at, updated_at`

func scanDownload(row interface{ Scan(...any) error }) (*Download, error) {
	var d Download
	var sub, scene sql.NullInt64
	var opts string
	if err := row.Scan(&d.ID, &d.UserID, &d.ParentID, &sub, &d.URL, &d.Kind, &d.Title, &d.Thumbnail, &opts, &d.Status, &d.Percent, &d.Speed, &d.ETA,
		&d.SizeBytes, &d.FilePath, &scene, &d.Error, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(opts), &d.Options)
	d.Options.Password = "" // never shown back
	if sub.Valid {
		d.SubscriptionID = &sub.Int64
	}
	if scene.Valid {
		d.SceneID = &scene.Int64
	}
	return &d, nil
}

// DownloadOptionsOf returns a download's options including the password.
func (s *Store) DownloadOptionsOf(ctx context.Context, id string) (DownloadOptions, error) {
	var opts string
	var o DownloadOptions
	if err := s.DB.QueryRowContext(ctx, `SELECT options FROM downloads WHERE id = ?`, id).Scan(&opts); err != nil {
		return o, notFound(err)
	}
	_ = json.Unmarshal([]byte(opts), &o)
	return o, nil
}

// CreateDownload queues a link.
func (s *Store) CreateDownload(ctx context.Context, d *Download) error {
	opts, _ := json.Marshal(d.Options)
	if d.Status == "" {
		d.Status = "queued"
	}
	if d.Kind == "" {
		d.Kind = "video"
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO downloads (id, user_id, parent_id, subscription_id, url, kind, title, thumbnail, options, status) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.UserID, d.ParentID, nullI(d.SubscriptionID), d.URL, d.Kind, d.Title, d.Thumbnail, string(opts), d.Status)
	return conflict(err)
}

// GetDownload loads one download (uid 0 = anyone's: background work).
func (s *Store) GetDownload(ctx context.Context, uid int64, id string) (*Download, error) {
	if uid == 0 {
		return scanDownload(s.DB.QueryRowContext(ctx, `SELECT `+downloadCols+` FROM downloads WHERE id = ?`, id))
	}
	return scanDownload(s.DB.QueryRowContext(ctx, `SELECT `+downloadCols+` FROM downloads WHERE id = ? AND user_id = ?`, id, uid))
}

// ListDownloads returns one person's downloads, newest first.
func (s *Store) ListDownloads(ctx context.Context, uid int64, limit int) ([]Download, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+downloadCols+` FROM downloads WHERE user_id = ? ORDER BY
		CASE status WHEN 'downloading' THEN 0 WHEN 'processing' THEN 0 WHEN 'queued' THEN 1 WHEN 'paused' THEN 1 ELSE 2 END, created_at DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Download{}
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// QueuedDownloads lists what is waiting to start, oldest first.
func (s *Store) QueuedDownloads(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM downloads WHERE status = 'queued' ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DownloadPatch changes a download; nil fields stay.
type DownloadPatch struct {
	Status, Title, Thumbnail, Speed, ETA, FilePath, Error *string
	Percent                                               *float64
	SizeBytes                                             *int64
	SceneID                                               *int64
}

// UpdateDownload applies a patch.
func (s *Store) UpdateDownload(ctx context.Context, id string, p DownloadPatch) error {
	set := []string{"updated_at = ?"}
	args := []any{Now()}
	for col, v := range map[string]*string{"status": p.Status, "title": p.Title, "thumbnail": p.Thumbnail, "speed": p.Speed, "eta": p.ETA, "file_path": p.FilePath, "error": p.Error} {
		if v != nil {
			set = append(set, col+" = ?")
			args = append(args, *v)
		}
	}
	if p.Percent != nil {
		set = append(set, "percent = ?")
		args = append(args, *p.Percent)
	}
	if p.SizeBytes != nil {
		set = append(set, "size_bytes = ?")
		args = append(args, *p.SizeBytes)
	}
	if p.SceneID != nil {
		set = append(set, "scene_id = ?")
		args = append(args, nullI(p.SceneID))
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE downloads SET `+strings.Join(set, ", ")+` WHERE id = ?`, append(args, id)...)
	return err
}

// RequeueInterrupted puts downloads cut off by a restart back in the queue.
func (s *Store) RequeueInterrupted(ctx context.Context) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE downloads SET status = 'queued', speed = '', eta = '' WHERE status IN ('downloading', 'processing')`)
}

// DeleteDownload removes a download row (and the rows of a playlist).
func (s *Store) DeleteDownload(ctx context.Context, uid int64, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM downloads WHERE user_id = ? AND (id = ? OR parent_id = ?)`, uid, id, id)
	return err
}

// ClearDownloads removes finished (done, canceled) rows; failed too when asked.
func (s *Store) ClearDownloads(ctx context.Context, uid int64, failed bool) (int64, error) {
	q := `DELETE FROM downloads WHERE user_id = ? AND status IN ('done', 'canceled'`
	if failed {
		q += `, 'failed'`
	}
	res, err := s.DB.ExecContext(ctx, q+`)`, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DownloadedURL reports whether a person already fetched this link successfully.
func (s *Store) DownloadedURL(ctx context.Context, uid int64, url string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM downloads WHERE user_id = ? AND url = ? AND status IN ('done', 'queued', 'downloading', 'processing')`, uid, url).Scan(&n)
	return n > 0
}

// ---------- subscriptions ----------

const subCols = `id, user_id, url, name, options, check_minutes, title_filter, enabled, last_checked_at, last_error, found, created_at`

func scanSub(row interface{ Scan(...any) error }) (*Subscription, error) {
	var x Subscription
	var opts string
	if err := row.Scan(&x.ID, &x.UserID, &x.URL, &x.Name, &opts, &x.CheckMinutes, &x.TitleFilter, &x.Enabled, &x.LastCheckedAt, &x.LastError, &x.Found, &x.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(opts), &x.Options)
	return &x, nil
}

// ListSubscriptions returns one person's subscriptions (uid 0 = everyone's).
func (s *Store) ListSubscriptions(ctx context.Context, uid int64) ([]Subscription, error) {
	q := `SELECT ` + subCols + ` FROM subscriptions`
	var args []any
	if uid > 0 {
		q += ` WHERE user_id = ?`
		args = append(args, uid)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY name COLLATE NOCASE`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		x, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// GetSubscription loads one subscription of a person.
func (s *Store) GetSubscription(ctx context.Context, uid, id int64) (*Subscription, error) {
	return scanSub(s.DB.QueryRowContext(ctx, `SELECT `+subCols+` FROM subscriptions WHERE id = ? AND user_id = ?`, id, uid))
}

// SaveSubscription inserts (ID 0) or updates a subscription.
func (s *Store) SaveSubscription(ctx context.Context, x *Subscription) error {
	opts, _ := json.Marshal(x.Options)
	if x.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO subscriptions (user_id, url, name, options, check_minutes, title_filter, enabled) VALUES (?,?,?,?,?,?,?)`,
			x.UserID, x.URL, x.Name, string(opts), x.CheckMinutes, x.TitleFilter, boolInt(x.Enabled))
		if err != nil {
			return conflict(err)
		}
		x.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE subscriptions SET name = ?, options = ?, check_minutes = ?, title_filter = ?, enabled = ? WHERE id = ? AND user_id = ?`,
		x.Name, string(opts), x.CheckMinutes, x.TitleFilter, boolInt(x.Enabled), x.ID, x.UserID)
	return err
}

// SubscriptionChecked records a check.
func (s *Store) SubscriptionChecked(ctx context.Context, id int64, name, errMsg string, found int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE subscriptions SET last_checked_at = ?, last_error = ?, found = found + ?, name = CASE WHEN name = '' THEN ? ELSE name END WHERE id = ?`,
		Now(), errMsg, found, name, id)
	return err
}

// DeleteSubscription removes a subscription.
func (s *Store) DeleteSubscription(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM subscriptions WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SeenVideos returns the video ids a subscription has already handled.
func (s *Store) SeenVideos(ctx context.Context, subID int64) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT video_id FROM subscription_seen WHERE subscription_id = ?`, subID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if rows.Scan(&v) == nil {
			out[v] = true
		}
	}
	return out, rows.Err()
}

// MarkSeen remembers handled video ids.
func (s *Store) MarkSeen(ctx context.Context, subID int64, ids []string) error {
	for _, id := range ids {
		if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_seen (subscription_id, video_id) VALUES (?, ?)`, subID, id); err != nil {
			return err
		}
	}
	return nil
}
