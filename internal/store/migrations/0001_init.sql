CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- ============ the catalogue (shared by everyone on the box) ============
-- Scenes and galleries mirror the library folder. Ids are never reused
-- (AUTOINCREMENT): headset apps, links and history point at them.

CREATE TABLE studios (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  parent_id   INTEGER REFERENCES studios(id) ON DELETE SET NULL,
  aliases     TEXT NOT NULL DEFAULT '',          -- "Badoink VR, BadoinkVR"
  url         TEXT NOT NULL DEFAULT '',
  details     TEXT NOT NULL DEFAULT '',
  image_path  TEXT NOT NULL DEFAULT '',          -- FILES_DIR relative
  stash_ids   TEXT NOT NULL DEFAULT '{}',        -- {"https://stashdb.org/graphql": "uuid"}
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE performers (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL COLLATE NOCASE,
  disambiguation TEXT NOT NULL DEFAULT '',
  aliases       TEXT NOT NULL DEFAULT '',
  gender        TEXT NOT NULL DEFAULT '',        -- female | male | trans_female | trans_male | non_binary | intersex
  birthdate     TEXT NOT NULL DEFAULT '',
  death_date    TEXT NOT NULL DEFAULT '',
  country       TEXT NOT NULL DEFAULT '',
  ethnicity     TEXT NOT NULL DEFAULT '',
  hair_color    TEXT NOT NULL DEFAULT '',
  eye_color     TEXT NOT NULL DEFAULT '',
  height_cm     INTEGER NOT NULL DEFAULT 0,
  weight_kg     INTEGER NOT NULL DEFAULT 0,
  measurements  TEXT NOT NULL DEFAULT '',
  fake_tits     TEXT NOT NULL DEFAULT '',
  tattoos       TEXT NOT NULL DEFAULT '',
  piercings     TEXT NOT NULL DEFAULT '',
  career        TEXT NOT NULL DEFAULT '',        -- "2012 - 2020"
  details       TEXT NOT NULL DEFAULT '',
  urls          TEXT NOT NULL DEFAULT '[]',
  image_path    TEXT NOT NULL DEFAULT '',
  stash_ids     TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (name, disambiguation)
);

CREATE TABLE tags (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  aliases     TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  parent_id   INTEGER REFERENCES tags(id) ON DELETE SET NULL,
  image_path  TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE scenes (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  path          TEXT NOT NULL UNIQUE,            -- relative to the library
  size_bytes    INTEGER NOT NULL DEFAULT 0,
  file_mtime    TEXT NOT NULL DEFAULT '',
  oshash        TEXT NOT NULL DEFAULT '',
  phash         TEXT NOT NULL DEFAULT '',        -- 16 hex digits, '' = not yet
  -- the file (ffprobe)
  duration      REAL NOT NULL DEFAULT 0,
  width         INTEGER NOT NULL DEFAULT 0,
  height        INTEGER NOT NULL DEFAULT 0,
  fps           REAL NOT NULL DEFAULT 0,
  video_codec   TEXT NOT NULL DEFAULT '',
  audio_codec   TEXT NOT NULL DEFAULT '',
  container     TEXT NOT NULL DEFAULT '',
  bitrate       INTEGER NOT NULL DEFAULT 0,
  probed_at     TEXT NOT NULL DEFAULT '',
  -- what it is
  title         TEXT NOT NULL DEFAULT '',        -- '' = shown as the file name
  code          TEXT NOT NULL DEFAULT '',
  details       TEXT NOT NULL DEFAULT '',
  director      TEXT NOT NULL DEFAULT '',
  date          TEXT NOT NULL DEFAULT '',        -- YYYY-MM-DD (or YYYY-MM, YYYY)
  urls          TEXT NOT NULL DEFAULT '[]',
  studio_id     INTEGER REFERENCES studios(id) ON DELETE SET NULL,
  organized     INTEGER NOT NULL DEFAULT 0,
  stash_ids     TEXT NOT NULL DEFAULT '{}',
  source_url    TEXT NOT NULL DEFAULT '',        -- the page it was downloaded from
  -- VR: '' projection = decide from the file (name, shape); vr_set = chosen by hand
  projection    TEXT NOT NULL DEFAULT '',        -- flat | equirect180 | equirect360 | fisheye
  stereo        TEXT NOT NULL DEFAULT '',        -- mono | sbs | tb
  fov           INTEGER NOT NULL DEFAULT 0,      -- fisheye lens: 180 | 190 | 200 | 220
  vr_set        INTEGER NOT NULL DEFAULT 0,
  -- pictures
  cover_path    TEXT NOT NULL DEFAULT '',        -- FILES_DIR relative
  cover_source  TEXT NOT NULL DEFAULT '',        -- sidecar | frame | upload | stashdb | download | stash
  gen_state     TEXT NOT NULL DEFAULT '',        -- '' (to do) | done | failed
  gen_error     TEXT NOT NULL DEFAULT '',
  gen_version   INTEGER NOT NULL DEFAULT 0,      -- bumps when the files are made again (cache busting)
  has_preview   INTEGER NOT NULL DEFAULT 0,
  has_sprite    INTEGER NOT NULL DEFAULT 0,
  -- life
  missing_since TEXT NOT NULL DEFAULT '',
  trashed_at    TEXT NOT NULL DEFAULT '',
  trash_path    TEXT NOT NULL DEFAULT '',
  added_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX scenes_studio ON scenes(studio_id);
CREATE INDEX scenes_oshash ON scenes(oshash);
CREATE INDEX scenes_gen ON scenes(gen_state);

CREATE TABLE scene_performers (
  scene_id     INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
  performer_id INTEGER NOT NULL REFERENCES performers(id) ON DELETE CASCADE,
  PRIMARY KEY (scene_id, performer_id)
);
CREATE INDEX scene_performers_p ON scene_performers(performer_id);

CREATE TABLE scene_tags (
  scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
  tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (scene_id, tag_id)
);
CREATE INDEX scene_tags_t ON scene_tags(tag_id);

-- Moments in a scene (a position, a chapter), shared like the scene.
CREATE TABLE markers (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  scene_id       INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
  title          TEXT NOT NULL DEFAULT '',
  seconds        REAL NOT NULL,
  end_seconds    REAL,                           -- NULL = a point in time
  primary_tag_id INTEGER REFERENCES tags(id) ON DELETE SET NULL,
  created_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX markers_scene ON markers(scene_id, seconds);
CREATE INDEX markers_tag ON markers(primary_tag_id);

-- Groups (movies, series): scenes in an order.
CREATE TABLE groups_ (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  aliases     TEXT NOT NULL DEFAULT '',
  date        TEXT NOT NULL DEFAULT '',
  director    TEXT NOT NULL DEFAULT '',
  synopsis    TEXT NOT NULL DEFAULT '',
  studio_id   INTEGER REFERENCES studios(id) ON DELETE SET NULL,
  urls        TEXT NOT NULL DEFAULT '[]',
  front_path  TEXT NOT NULL DEFAULT '',
  back_path   TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE group_scenes (
  group_id    INTEGER NOT NULL REFERENCES groups_(id) ON DELETE CASCADE,
  scene_id    INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
  scene_index INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (group_id, scene_id)
);
CREATE INDEX group_scenes_s ON group_scenes(scene_id);

-- Galleries: a folder (or .zip) of pictures.
CREATE TABLE galleries (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  path          TEXT NOT NULL UNIQUE,            -- relative to the library
  is_zip        INTEGER NOT NULL DEFAULT 0,
  title         TEXT NOT NULL DEFAULT '',
  date          TEXT NOT NULL DEFAULT '',
  details       TEXT NOT NULL DEFAULT '',
  photographer  TEXT NOT NULL DEFAULT '',
  urls          TEXT NOT NULL DEFAULT '[]',
  studio_id     INTEGER REFERENCES studios(id) ON DELETE SET NULL,
  scene_id      INTEGER REFERENCES scenes(id) ON DELETE SET NULL,
  image_count   INTEGER NOT NULL DEFAULT 0,
  cover_image   INTEGER NOT NULL DEFAULT 0,      -- images.id, 0 = the first
  file_mtime    TEXT NOT NULL DEFAULT '',
  missing_since TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE images (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  gallery_id  INTEGER NOT NULL REFERENCES galleries(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,                     -- inside the folder or zip
  position    INTEGER NOT NULL DEFAULT 0,
  size_bytes  INTEGER NOT NULL DEFAULT 0,
  width       INTEGER NOT NULL DEFAULT 0,
  height      INTEGER NOT NULL DEFAULT 0,
  UNIQUE (gallery_id, name)
);

CREATE TABLE gallery_performers (
  gallery_id   INTEGER NOT NULL REFERENCES galleries(id) ON DELETE CASCADE,
  performer_id INTEGER NOT NULL REFERENCES performers(id) ON DELETE CASCADE,
  PRIMARY KEY (gallery_id, performer_id)
);
CREATE TABLE gallery_tags (
  gallery_id INTEGER NOT NULL REFERENCES galleries(id) ON DELETE CASCADE,
  tag_id     INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (gallery_id, tag_id)
);

-- ============ per person ============

-- One person's relation to a scene.
CREATE TABLE user_scenes (
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scene_id       INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
  rating         INTEGER NOT NULL DEFAULT 0,     -- 0 = none, 1..10 (half stars)
  favorite       INTEGER NOT NULL DEFAULT 0,
  o_count        INTEGER NOT NULL DEFAULT 0,
  play_count     INTEGER NOT NULL DEFAULT 0,
  play_seconds   INTEGER NOT NULL DEFAULT 0,
  resume_seconds REAL NOT NULL DEFAULT 0,
  last_played_at TEXT NOT NULL DEFAULT '',
  hidden         INTEGER NOT NULL DEFAULT 0,
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, scene_id)
);
CREATE INDEX user_scenes_played ON user_scenes(user_id, last_played_at);

-- One sitting with a scene.
CREATE TABLE plays (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scene_id   INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
  started_at TEXT NOT NULL,
  seconds    INTEGER NOT NULL DEFAULT 0,
  counted    INTEGER NOT NULL DEFAULT 0,         -- long enough to count as a play
  device     TEXT NOT NULL DEFAULT 'browser',    -- browser | headset | deovr | heresphere
  day        TEXT NOT NULL
);
CREATE INDEX plays_user ON plays(user_id, started_at);
CREATE INDEX plays_scene ON plays(user_id, scene_id, started_at);

CREATE TABLE o_events (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
  at       TEXT NOT NULL
);
CREATE INDEX o_events_user ON o_events(user_id, scene_id, at);

-- Favourites and ratings of performers, studios, tags, groups, galleries.
CREATE TABLE user_marks (
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind     TEXT NOT NULL,                        -- performer | studio | tag | group | gallery
  item_id  INTEGER NOT NULL,
  favorite INTEGER NOT NULL DEFAULT 0,
  rating   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, kind, item_id)
);

-- Saved views of a list ("4K VR, newest first").
CREATE TABLE saved_filters (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  mode       TEXT NOT NULL DEFAULT 'scenes',     -- scenes | performers | markers | galleries
  name       TEXT NOT NULL,
  query      TEXT NOT NULL DEFAULT '',           -- the URL query of the list
  pinned     INTEGER NOT NULL DEFAULT 0,         -- shown in the sidebar
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX saved_filters_user ON saved_filters(user_id, mode, name);

-- Videos fetched from a link (yt-dlp). A playlist or channel link becomes
-- one row per video (parent_id = the playlist row).
CREATE TABLE downloads (
  id              TEXT PRIMARY KEY,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  parent_id       TEXT NOT NULL DEFAULT '',
  subscription_id INTEGER,
  url             TEXT NOT NULL,
  kind            TEXT NOT NULL DEFAULT 'video', -- video | playlist
  title           TEXT NOT NULL DEFAULT '',
  thumbnail       TEXT NOT NULL DEFAULT '',
  options         TEXT NOT NULL DEFAULT '{}',    -- quality, format, folder, clip, …
  status          TEXT NOT NULL DEFAULT 'queued',-- queued | paused | downloading | processing | done | failed | canceled
  percent         REAL NOT NULL DEFAULT 0,
  speed           TEXT NOT NULL DEFAULT '',
  eta             TEXT NOT NULL DEFAULT '',
  size_bytes      INTEGER NOT NULL DEFAULT 0,
  file_path       TEXT NOT NULL DEFAULT '',      -- library relative, when done
  scene_id        INTEGER,
  error           TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX downloads_user ON downloads(user_id, created_at);
CREATE INDEX downloads_status ON downloads(status);

-- Channels and playlists checked for new videos.
CREATE TABLE subscriptions (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  url             TEXT NOT NULL,
  name            TEXT NOT NULL DEFAULT '',
  options         TEXT NOT NULL DEFAULT '{}',
  check_minutes   INTEGER NOT NULL DEFAULT 60,
  title_filter    TEXT NOT NULL DEFAULT '',      -- a regular expression the title must match
  enabled         INTEGER NOT NULL DEFAULT 1,
  last_checked_at TEXT NOT NULL DEFAULT '',
  last_error      TEXT NOT NULL DEFAULT '',
  found           INTEGER NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, url)
);

CREATE TABLE subscription_seen (
  subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
  video_id        TEXT NOT NULL,
  PRIMARY KEY (subscription_id, video_id)
);
