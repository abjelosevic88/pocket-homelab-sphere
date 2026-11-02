# Backup & restore

Pocket Sphere keeps three kinds of things apart:

- **`DATA_DIR`** (`/data` in the container) — the app's own data:
  - `pocket-sphere.db` (+ `-wal` / `-shm` while running) — the catalogue (scenes, performers, studios, tags, groups, markers, galleries), everyone's ratings, favourites, O's, play history, saved views, downloads and followed channels, settings, users
  - `files/covers/`, `files/performers/`, `files/studios/`, `files/tags/`, `files/groups/` — covers, photos and logos (back these up: some came from lookups or uploads)
  - `cookies/` — each person's cookies.txt for downloads (private)
  - `bin/yt-dlp` — the app's own yt-dlp (fetched again when missing)
  - `backups/` — where the CLI writes snapshots
- **`GENERATED_DIR`** (`DATA_DIR/generated` by default) — scrubber pictures, hover previews, marker clips, gallery thumbnails. Everything here is made again from the videos; **leave it out of backups**.
- **`LIBRARY_DIR`** (`/library`) — the videos and pictures themselves, plus `.trash/` (deleted scenes, kept `TRASH_DAYS`) and `.downloads/` (downloads in progress). Back it up however you back up big media, or not at all.

## Consistent copies

Never copy a live WAL database file. Use one of:

- **UI**: Settings → Backup & data → *Download database*
- **API**: `GET /api/v1/backup` with a bearer token (admin)
- **CLI**: `docker exec pocket-sphere pocket-sphere backup` → prints the path under `/data/backups/`
- **sqlite3**: `sqlite3 data/pocket-sphere.db "VACUUM INTO 'snap.db'"`

Then back up `files/` with your usual tool (restic, rsync…). Exclude `pocket-sphere.db*`, `generated/` and `bin/` from raw file backups and include the snapshot instead.

## Restore

1. Stop the container.
2. Replace `data/pocket-sphere.db` with the snapshot (remove any stale `-wal` / `-shm`).
3. Restore `files/`.
4. Start the container. Migrations run forward automatically; the library is read again on start and missing pictures are made in the background.

## JSON export

Settings → Backup & data exports one person's ratings, favourites, O's, play history, resume points, favourite performers/studios/tags, saved views and followed channels as JSON. Scenes are named by fingerprint (oshash) and path, so the export works on another box with the same files. Importing it twice changes nothing the second time.
