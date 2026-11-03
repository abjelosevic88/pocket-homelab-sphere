# API

JSON over HTTP under `/api/v1`. Sign in with the session cookie (the web UI), or send a personal token from Settings → API & shortcuts:

```
Authorization: Bearer psp_…
```

Everything personal (ratings, favourites, O's, plays, saved views, downloads, followed channels, settings) belongs to the person the token belongs to; the library itself is shared.

## Scenes

| | |
|---|---|
| `GET /scenes` | The list. Filters: `q`, `performers=1,2`, `tags=…` (`tags_all=1`), `studios=…`, `groups=…`, `vr=yes\|no`, `min_h`, `max_h`, `min_dur`/`max_dur` (minutes), `rating` (0–10), `favorite=1`, `unwatched=1`, `watched=1`, `progress=1`, `organized=yes\|no`, `markers=1`, `folder`, `missing=1`, `trash=1`, `hidden=1`. Order: `sort=added\|date\|title\|rating\|played\|plays\|o\|duration\|resolution\|size\|path\|random` (`seed`), `dir=asc\|desc`. Pages: `page`, `per` |
| `GET /scenes/ids?…` | The same filters, only the ids (a play queue) |
| `GET /scenes/{id}` | One scene with its file facts and how it plays |
| `PUT /scenes/{id}` | Edit: `title`, `date`, `details`, `code`, `director`, `urls`, `studio_id` / `studio` (new by name), `performers` / `new_performers`, `tags` / `new_tags`, `groups` (`{"id": number}`), `organized`, `projection` (`flat\|equirect180\|equirect360\|fisheye`), `stereo` (`mono\|sbs\|tb`), `fov`, `vr_auto` |
| `PUT /scenes/{id}/mine` | Yours: `rating` (0–10), `favorite`, `hidden`, `resume_seconds`, `play_count` |
| `POST` / `DELETE /scenes/{id}/o` | Add an O / take the last one back |
| `POST /scenes/{id}/plays` → `PUT /plays/{pid}` | A sitting: `{"watched": seconds, "position": seconds, "duration": seconds}` |
| `GET /scenes/{id}/stream` | The file (Range requests); `?download=1` as an attachment |
| `GET /scenes/{id}/stream.mp4?start=&q=` | A live MP4 (`q=original` repackages, `q=1080` converts) |
| `GET /scenes/{id}/preview.mp4`, `/sprite.jpg`, `/sprite.json`, `/cover`, `/frame?at=` | Made pictures and clips |
| `GET /scenes/{id}/identify?q=` → `POST /scenes/{id}/identify` | Look it up on StashDB / TPDB; apply `{"result": …, "fields": {…}}` |
| `POST /scenes/bulk` | `{"ids": […], "add_tags", "remove_tags", "add_performers", "studio_id", "organized", "rating", "favorite", "hidden", "regenerate", "identify", "trash"}` |
| `DELETE /scenes/{id}` · `POST /scenes/{id}/restore` · `DELETE /scenes/{id}/purge` | Trash, restore, delete for good (admin) |

## Everything else

`/performers` (`/lookup?q=`, `/{id}/apply`, `/{id}/merge`, `/{id}/image`), `/studios`, `/tags` (`/{id}/merge`), `/groups`, `/markers` (`?tag=`), `/scenes/{id}/markers`, `/galleries`, `/images/{id}`, `/filters`, `/history`, `/stats?days=`, `/dashboard`, `/home`, `/folders`, `/duplicates?distance=`, `/library` (status), `POST /library/scan`, `POST /library/autotag`, `POST /library/regenerate`.

## Discover

| | |
|---|---|
| `GET /performers/{id}/discover?source=stashdb\|tpdb&page=` (and `/studios/{id}/discover`) | Their scenes on the lookup site, newest first, each with `have_id` (already in the library) and `wanted_id` |
| `GET` / `POST /wanted` · `DELETE /wanted/{id}` | Your wishlist (`{"scene": …}` from a discover list); found ones get `found_scene_id` when a file is identified as them |
| `GET` / `POST /follows` (`{"kind": "performer\|studio", "item_id": 1}`) · `DELETE /follows/{id}` · `POST /follows/{id}/check` | Following; checked about once a day, the first check only remembers what exists |
| `GET /releases` · `POST /releases/{id}/dismiss` · `POST /releases/dismiss` | New scenes found by following |

## Downloads

| | |
|---|---|
| `POST /downloads` | `{"url": "https://…"}` (or `"urls": […]`), optional `"options": {"quality", "format", "folder", "name_prefix", "clip_start", "clip_end", "items_limit", "password", "auto_start", "own_folder", "performers", "tags", "studio_id"}` |
| `GET /downloads` | Yours, with progress |
| `PUT /downloads/{id}` | `{"action": "retry\|pause\|start"}` |
| `DELETE /downloads/{id}` | Cancel / remove |
| `GET` / `POST` / `PUT` / `DELETE /subscriptions` | Followed channels and playlists (`check_minutes`, `title_filter`, `enabled`, `options`) |
| `PUT` / `DELETE /downloads/cookies` | Your cookies.txt |

An iOS Shortcut that shares a page to Sphere: *Get URLs from Shortcut Input* → *Get contents of* `https://sphere.example.com/api/v1/downloads`, method POST, header `Authorization: Bearer psp_…`, JSON body `url` = the URL.

## Headset feeds

`/vr/<key>/deovr` (DeoVR) and `/vr/<key>/heresphere` (HereSphere) with the personal key from Settings → Headset. HereSphere writes favourites and ratings back and reports play position through `/vr/<key>/heresphere/events/{id}`.

## Health

`/healthz`, `/readyz`, `/metrics` (`pocket_sphere_*`).
