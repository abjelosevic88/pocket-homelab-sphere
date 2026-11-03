-- Discover: scenes you do not have yet, found on StashDB / TPDB. All per person.

-- Scenes you want (a lookup result kept as JSON); found_scene_id is set
-- once a file in the library is identified as it.
CREATE TABLE wanted (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint       TEXT NOT NULL,                  -- the stash-box address, or "tpdb"
  remote_id      TEXT NOT NULL,
  title          TEXT NOT NULL DEFAULT '',
  release_date   TEXT NOT NULL DEFAULT '',
  scene_json     TEXT NOT NULL DEFAULT '{}',
  found_scene_id INTEGER REFERENCES scenes(id) ON DELETE SET NULL,
  found_at       TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, endpoint, remote_id)
);
CREATE INDEX wanted_user ON wanted(user_id, created_at);

-- Performers and studios you follow: new scenes of theirs are looked for daily.
CREATE TABLE follows (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind            TEXT NOT NULL,                 -- performer | studio
  item_id         INTEGER NOT NULL,
  last_checked_at TEXT NOT NULL DEFAULT '',
  last_error      TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, kind, item_id)
);

CREATE TABLE follow_seen (
  follow_id INTEGER NOT NULL REFERENCES follows(id) ON DELETE CASCADE,
  remote_id TEXT NOT NULL,
  PRIMARY KEY (follow_id, remote_id)
);

-- New scenes found by following, until dismissed.
CREATE TABLE releases (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  follow_id    INTEGER REFERENCES follows(id) ON DELETE SET NULL,
  endpoint     TEXT NOT NULL,
  remote_id    TEXT NOT NULL,
  release_date TEXT NOT NULL DEFAULT '',
  scene_json   TEXT NOT NULL DEFAULT '{}',
  dismissed    INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, endpoint, remote_id)
);
CREATE INDEX releases_user ON releases(user_id, dismissed, release_date);
