package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-sphere/internal/store"
)

// A person's own data as JSON: ratings, favourites, O's, plays, where each
// video was left, favourites of performers/studios/tags, saved views and
// followed channels. Scenes are named by their fingerprint and path, so an
// export moves to another box with the same files. Importing twice changes
// nothing the second time.

type exportScene struct {
	Path          string   `json:"path"`
	OSHash        string   `json:"oshash,omitempty"`
	Rating        int      `json:"rating,omitempty"`
	Favorite      bool     `json:"favorite,omitempty"`
	Hidden        bool     `json:"hidden,omitempty"`
	ResumeSeconds float64  `json:"resume_seconds,omitempty"`
	Os            []string `json:"os,omitempty"`
	Plays         []struct {
		StartedAt string `json:"started_at"`
		Seconds   int64  `json:"seconds"`
		Counted   bool   `json:"counted"`
		Device    string `json:"device"`
	} `json:"plays,omitempty"`
}

type exportMark struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Favorite bool   `json:"favorite,omitempty"`
	Rating   int    `json:"rating,omitempty"`
}

type exportDoc struct {
	App           string               `json:"app"`
	Version       string               `json:"version"`
	ExportedAt    string               `json:"exported_at"`
	Scenes        []exportScene        `json:"scenes"`
	Marks         []exportMark         `json:"marks"`
	Filters       []store.SavedFilter  `json:"filters"`
	Subscriptions []store.Subscription `json:"subscriptions"`
}

var markTables = map[string]string{"performer": "performers", "studio": "studios", "tag": "tags", "group": "groups_", "gallery": "galleries"}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	doc := exportDoc{App: "pocket-sphere", Version: Version, ExportedAt: store.Now(), Scenes: []exportScene{}, Marks: []exportMark{}}
	scenes, _, err := s.store.ListScenes(ctx, store.SceneFilter{UserID: u.ID, Hidden: true, Limit: 1000000, Sort: "path"})
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	for _, sc := range scenes {
		m := sc.Mine
		h, _ := s.store.History(ctx, u.ID, sc.ID)
		if m.Rating == 0 && !m.Favorite && !m.Hidden && m.ResumeSeconds == 0 && (h == nil || (len(h.Plays) == 0 && len(h.Os) == 0)) {
			continue
		}
		e := exportScene{Path: sc.Path, OSHash: sc.OSHash, Rating: m.Rating, Favorite: m.Favorite, Hidden: m.Hidden, ResumeSeconds: m.ResumeSeconds}
		if h != nil {
			e.Os = h.Os
			for _, p := range h.Plays {
				e.Plays = append(e.Plays, struct {
					StartedAt string `json:"started_at"`
					Seconds   int64  `json:"seconds"`
					Counted   bool   `json:"counted"`
					Device    string `json:"device"`
				}{p.StartedAt, p.Seconds, p.Counted, p.Device})
			}
		}
		doc.Scenes = append(doc.Scenes, e)
	}
	for kind, table := range markTables {
		marks, _ := s.store.Marks(ctx, u.ID, kind)
		col := "name"
		if table == "galleries" {
			col = "path"
		}
		for id, m := range marks {
			var name string
			if s.store.DB.QueryRowContext(ctx, `SELECT `+col+` FROM `+table+` WHERE id = ?`, id).Scan(&name) == nil && (m.Favorite || m.Rating > 0) {
				doc.Marks = append(doc.Marks, exportMark{Kind: kind, Name: name, Favorite: m.Favorite, Rating: m.Rating})
			}
		}
	}
	doc.Filters, _ = s.store.ListFilters(ctx, u.ID)
	doc.Subscriptions, _ = s.store.ListSubscriptions(ctx, u.ID)
	for i := range doc.Subscriptions {
		doc.Subscriptions[i].Options.Password = ""
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-sphere-%s.json"`, time.Now().Format("2006-01-02")))
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the file is too big")
		return
	}
	var doc exportDoc
	if err := json.Unmarshal(b, &doc); err != nil || doc.App != "pocket-sphere" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Sphere export")
		return
	}
	res, err := s.importDoc(ctx, userFrom(ctx).ID, &doc)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) importDoc(ctx context.Context, uid int64, doc *exportDoc) (map[string]int, error) {
	res := map[string]int{"scenes": 0, "plays": 0, "os": 0, "marks": 0, "filters": 0, "subscriptions": 0, "not_found": 0}
	for _, e := range doc.Scenes {
		var id int64
		if e.OSHash != "" {
			_ = s.store.DB.QueryRowContext(ctx, `SELECT id FROM scenes WHERE oshash = ? AND trashed_at = '' LIMIT 1`, e.OSHash).Scan(&id)
		}
		if id == 0 {
			_ = s.store.DB.QueryRowContext(ctx, `SELECT id FROM scenes WHERE path = ?`, e.Path).Scan(&id)
		}
		if id == 0 {
			res["not_found"]++
			continue
		}
		p := store.MinePatch{}
		if e.Rating > 0 {
			p.Rating = &e.Rating
		}
		if e.Favorite {
			p.Favorite = boolp(true)
		}
		if e.Hidden {
			p.Hidden = boolp(true)
		}
		if e.ResumeSeconds > 0 {
			p.ResumeSeconds = &e.ResumeSeconds
		}
		if err := s.store.PutMine(ctx, uid, id, p); err != nil {
			return nil, err
		}
		res["scenes"]++
		for _, at := range e.Os {
			var n int
			_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM o_events WHERE user_id = ? AND scene_id = ? AND at = ?`, uid, id, at).Scan(&n)
			if n == 0 {
				if _, err := s.store.AddO(ctx, uid, id, at); err == nil {
					res["os"]++
				}
			}
		}
		for _, pl := range e.Plays {
			var n int
			_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM plays WHERE user_id = ? AND scene_id = ? AND started_at = ?`, uid, id, pl.StartedAt).Scan(&n)
			if n > 0 {
				continue
			}
			day := pl.StartedAt
			if t, err := time.Parse(time.RFC3339Nano, pl.StartedAt); err == nil {
				day = t.Local().Format("2006-01-02")
			}
			if len(day) > 10 {
				day = day[:10]
			}
			_, _ = s.store.DB.ExecContext(ctx, `INSERT INTO plays (user_id, scene_id, started_at, seconds, counted, device, day) VALUES (?,?,?,?,?,?,?)`,
				uid, id, pl.StartedAt, pl.Seconds, pl.Counted, pl.Device, day)
			if pl.Counted {
				_, _ = s.store.DB.ExecContext(ctx, `UPDATE user_scenes SET play_count = play_count + 1, last_played_at = MAX(last_played_at, ?) WHERE user_id = ? AND scene_id = ?`, pl.StartedAt, uid, id)
			}
			_, _ = s.store.DB.ExecContext(ctx, `UPDATE user_scenes SET play_seconds = play_seconds + ? WHERE user_id = ? AND scene_id = ?`, pl.Seconds, uid, id)
			res["plays"]++
		}
	}
	for _, m := range doc.Marks {
		table, ok := markTables[m.Kind]
		if !ok {
			continue
		}
		col := "name"
		if table == "galleries" {
			col = "path"
		}
		var id int64
		if s.store.DB.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE `+col+` = ? LIMIT 1`, m.Name).Scan(&id) != nil {
			continue
		}
		rating := m.Rating
		if err := s.store.SetMark(ctx, uid, m.Kind, id, boolp(m.Favorite), &rating); err == nil {
			res["marks"]++
		}
	}
	have, _ := s.store.ListFilters(ctx, uid)
	for _, f := range doc.Filters {
		dup := false
		for _, h := range have {
			if h.Mode == f.Mode && h.Name == f.Name {
				dup = true
			}
		}
		if !dup {
			f.ID = 0
			if s.store.SaveFilter(ctx, uid, &f) == nil {
				res["filters"]++
			}
		}
	}
	for _, x := range doc.Subscriptions {
		x.ID, x.UserID, x.LastCheckedAt = 0, uid, ""
		if s.store.SaveSubscription(ctx, &x) == nil {
			res["subscriptions"]++
		}
	}
	return res, nil
}
