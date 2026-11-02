# Configuration

Everything is set with environment variables; every one has a default.

| Variable | Default | What it does |
|---|---|---|
| `PORT` / `HOST` | `8080` / `0.0.0.0` | Where it listens |
| `BASE_URL` | — | The public address (sign-in callbacks, headset feeds, links in notifications) |
| `DATA_DIR` | `./data` (`/data` in the image) | Database, covers and photos, cookies, yt-dlp |
| `DB_PATH` | `DATA_DIR/pocket-sphere.db` | The SQLite file |
| `LIBRARY_DIR` | `DATA_DIR/library` (`/library`) | Your videos and picture folders; downloads land here |
| `GENERATED_DIR` | `DATA_DIR/generated` | Scrubber pictures, previews, marker clips — made again when lost; leave out of backups |
| `SCAN_INTERVAL` | `15m` | How often the library folder is read (`0` = only by hand) |
| `TRASH_DAYS` | `30` | Deleted scenes stay restorable this long |
| `FFMPEG` / `FFPROBE` | `ffmpeg` / `ffprobe` | The tools (in the image) |
| `GENERATE_WORKERS` | `1` | Videos worked on at the same time in the background |
| `GENERATE_PREVIEWS` | `true` | Make the short hover previews |
| `HWACCEL` | — | `vaapi` converts on an Intel/AMD GPU (pass `/dev/dri` and the render group) |
| `VAAPI_DEVICE` | `/dev/dri/renderD128` | The GPU |
| `YTDLP` | — | A yt-dlp of your own; empty = the app keeps its own copy in `DATA_DIR/bin` |
| `YTDLP_UPDATE` | `true` | Update the app's own copy once a day |
| `MAX_DOWNLOADS` | `2` | Downloads at the same time |
| `DOWNLOADS_FOR_MEMBERS` | `true` | Members (not only admins) may download from links |
| `HEADSET_APIS` | `true` | The DeoVR / HereSphere feeds under `/vr/<key>/` |
| `AUTH_MODE` | `local` | `local` (passwords, Pocket Core / OIDC), `proxy` (a trusted header), `none` (no login — local testing only) |
| `SESSION_TTL` | `720h` | How long a sign-in lasts |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | — | The first admin, created while the database has no users |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_NAME` | — | Sign in with any OpenID Connect provider |
| `NOTIFY_URL` / `NOTIFY_FORMAT` | — / `apprise` | The owner's push target (everyone can set their own) |
| `OPENAI_API_KEY` / `OPENAI_MODEL` / `OPENAI_BASE_URL` | — | The owner's AI (everyone can set their own) |
| `CORE_URL` / `CORE_REGISTRATION_KEY` / `CORE_APP_URL` | — | Join Pocket Core |
| `TRUST_PROXY` | `true` | Use `X-Forwarded-For` for the client address |
| `METRICS_ENABLED` | `true` | Prometheus metrics on `/metrics` |
| `MAX_BODY_BYTES` | `64 MiB` | Largest JSON import |
| `DEMO_DATA` | `false` | Seed a made-up library (placeholder videos) |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | Logging |

StashDB / ThePornDB keys, download defaults, the PIN lock and the headset address are personal settings in the app.
