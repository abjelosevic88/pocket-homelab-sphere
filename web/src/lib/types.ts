export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export interface Ref { id: number; name: string; image?: string; extra?: string }

export interface Mine {
  rating: number; favorite: boolean; o_count: number; play_count: number; play_seconds: number
  resume_seconds: number; last_played_at: string; hidden: boolean
}

export type Projection = '' | 'flat' | 'equirect180' | 'equirect360' | 'fisheye'
export type Stereo = 'mono' | 'sbs' | 'tb'

export interface Scene {
  id: number; path: string; size_bytes: number; file_mtime: string; oshash: string; phash: string
  duration: number; width: number; height: number; fps: number; video_codec: string; audio_codec: string; container: string; bitrate: number
  title: string; code: string; details: string; director: string; date: string; urls: string[]; studio_id: number | null; organized: boolean
  stash_ids: Record<string, string>; source_url: string
  projection: Projection; stereo: Stereo; fov: number; vr_set: boolean
  cover_source: string; gen_state: string; gen_error: string; gen_version: number; has_preview: boolean; has_sprite: boolean
  missing_since: string; trashed_at: string; added_by: number | null; created_at: string; updated_at: string
  studio: Ref | null; performers: Ref[]; tags: Ref[]; groups?: Ref[]; markers: number; mine: Mine
  name: string; cover: string; preview: string; sprite: string; is_vr: boolean; vr_label: string
}

export interface SceneFile {
  path: string; size_bytes: number; duration: number; width: number; height: number; fps: number; video_codec: string; audio_codec: string
  container: string; bitrate: number; oshash: string; phash: string; mtime: string; working: string
}

export interface Playback {
  direct: string; direct_ok: boolean; mime: string; converted: string; remux_ok: boolean; duration: number
  markers: string; sprite: string; sprite_map: string; missing?: boolean
}

export interface SceneDetail { scene: Scene; file: SceneFile; playback: Playback }

export interface SpriteMap { cols: number; rows: number; width: number; height: number; interval: number; count: number }

export interface Page<T> { items: T[]; total: number; page?: number; per?: number }

export interface Performer {
  id: number; name: string; disambiguation: string; aliases: string; gender: string; birthdate: string; death_date: string
  country: string; ethnicity: string; hair_color: string; eye_color: string; height_cm: number; weight_kg: number
  measurements: string; fake_tits: string; tattoos: string; piercings: string; career: string; details: string; urls: string[]
  stash_ids: Record<string, string>; created_at: string; updated_at: string
  scenes: number; galleries: number; favorite: boolean; rating: number; o_count: number; image: string; age: number
}

export interface Studio {
  id: number; name: string; parent_id: number | null; aliases: string; url: string; details: string; stash_ids: Record<string, string>
  created_at: string; updated_at: string; parent: Ref | null; children?: Ref[]; scenes: number; favorite: boolean; rating: number; image: string
}

export interface Tag {
  id: number; name: string; aliases: string; description: string; parent_id: number | null; created_at: string; updated_at: string
  parent: Ref | null; children?: Ref[]; scenes: number; markers: number; favorite: boolean; image: string
}

export interface Group {
  id: number; name: string; aliases: string; date: string; director: string; synopsis: string; studio_id: number | null; urls: string[]
  created_at: string; updated_at: string; studio: Ref | null; scenes: number; duration: number; favorite: boolean; rating: number; front: string; back: string
}

export interface Marker {
  id: number; scene_id: number; title: string; seconds: number; end_seconds: number | null; primary_tag_id: number | null; created_at: string
  tag: Ref | null; scene_name?: string; stream?: string; preview?: string; screenshot?: string
}

export interface Gallery {
  id: number; path: string; is_zip: boolean; title: string; date: string; details: string; photographer: string; urls: string[]
  studio_id: number | null; scene_id: number | null; image_count: number; cover_image: number; missing_since: string; created_at: string; updated_at: string
  name: string; studio: Ref | null; performers: Ref[]; tags: Ref[]; favorite: boolean; rating: number; cover: string
}
export interface GalleryImage { id: number; gallery_id: number; name: string; position: number; size_bytes: number; width: number; height: number; url: string; thumb: string }

export interface Play { id: number; scene_id: number; scene_name?: string; started_at: string; seconds: number; counted: boolean; device: string; day: string }
export interface SceneHistory { plays: Play[]; os: string[] }

export interface SavedFilter { id: number; mode: string; name: string; query: string; pinned: boolean; created_at: string }

export interface DownloadOptions {
  quality?: string; format?: string; folder?: string; name_prefix?: string; clip_start?: string; clip_end?: string; items_limit?: number
  password?: string; auto_start?: boolean; own_folder?: boolean; performers?: number[]; tags?: number[]; studio_id?: number
}
export interface Download {
  id: string; parent_id: string; subscription_id: number | null; url: string; kind: 'video' | 'playlist' | 'auto'; title: string; thumbnail: string
  options: DownloadOptions; status: 'queued' | 'paused' | 'downloading' | 'processing' | 'done' | 'failed' | 'canceled'
  percent: number; speed: string; eta: string; size_bytes: number; file_path: string; scene_id: number | null; error: string; created_at: string; updated_at: string
}
export interface Subscription {
  id: number; url: string; name: string; options: DownloadOptions; check_minutes: number; title_filter: string; enabled: boolean
  last_checked_at: string; last_error: string; found: number; created_at: string
}
export interface ToolStatus {
  ytdlp: { version: string; path: string; own: boolean; updated_at: string; error: string; updating: boolean }
  cookies: boolean; members: boolean; max: number; auto_update: boolean
}

export interface LookupStudio { id: string; name: string; url: string; image: string; parent: string }
export interface LookupPerformer {
  source: string; id: string; name: string; as?: string; disambiguation: string; aliases: string[] | null; gender: string; birthdate: string; death_date: string
  country: string; ethnicity: string; eye_color: string; hair_color: string; height_cm: number; measurements: string; fake_tits: string
  career: string; tattoos: string; piercings: string; urls: string[] | null; image: string
}
export interface LookupScene {
  source: 'stashdb' | 'tpdb'; endpoint: string; id: string; title: string; code: string; details: string; director: string; date: string
  duration: number; urls: string[]; image: string; studio: LookupStudio | null; performers: LookupPerformer[]; tags: string[]; matched?: string
}

export interface Dashboard {
  scenes: number; vr: number; favorites: number; unwatched: number; in_progress: number; performers: number; studios: number; tags: number
  groups: number; galleries: number; markers: number; missing: number; trash: number; downloads: number; discover: number; bytes: number; seconds: number
}

export interface Home { continue: Scene[]; recent: Scene[]; vr: Scene[]; favorites: Scene[]; unwatched: Scene[]; top_rated: Scene[]; performers: Performer[] }

export interface CountRef { id: number; name: string; count: number; image?: string }
export interface Stats {
  plays: number; seconds: number; os: number; days: { day: string; seconds: number; plays: number; os: number }[]
  top_scenes: CountRef[]; top_performers: CountRef[]; top_tags: CountRef[]; top_studios: CountRef[]; hours: number[]; library: Record<string, number>
}

export interface ScanStatus { running: boolean; at: string; ms: number; scenes: number; added: number; moved: number; missing: number; galleries: number; error: string }
export interface LibraryStatus {
  scan: ScanStatus; to_read: number; to_make: number; to_hash: number; working: Record<string, string>
  ffmpeg: boolean; ffmpeg_version: string; ffmpeg_error?: string; streams: number; hwaccel: string; previews: boolean; free_bytes: number
}

export interface Settings {
  core_configured: boolean; core_registered?: boolean
  title: string; date_format: string; time_format: string
  grid_size: 's' | 'm' | 'l'; default_sort: string; hover_preview: boolean; show_missing: boolean
  discreet: boolean; panic_url: string; lock_enabled: boolean; lock_minutes: number; headset_enabled: boolean
  auto_play: boolean; continue_queue: boolean; resume_play: boolean; player_volume: number; stream_quality: string; skip_seconds: number
  vr_default_mode: 'view' | 'eye' | 'flat'; count_after: number
  download_folder: string; download_quality: string; download_format: string; own_folder: boolean; site_tags: boolean; uploader_studio: boolean; auto_identify: boolean
  stashdb_key: string; stashbox_url: string; tpdb_key: string
  notify_url: string; notify_format: string; notify_configured: boolean; notify_downloads: boolean; notify_quiet: boolean
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string; headset_apis: boolean; trash_days: number; downloads_open: boolean
}
export interface AIStatus { ready: boolean; provider: string; detail: string }
export interface Headset { enabled: boolean; available: boolean; base?: string; deovr?: string; heresphere?: string }

export interface DiscoverItem { scene: LookupScene; have_id: number; wanted_id: number; release_id?: number }
export interface DiscoverPage { problem?: string; source?: string; sources: string[]; remote_id?: string; total?: number; page?: number; per?: number; items: DiscoverItem[] }
export interface Follow { id: number; kind: 'performer' | 'studio'; item_id: number; name: string; image: string; last_checked_at: string; last_error: string; created_at: string; new: number }
export interface WantedItem { id: number; endpoint: string; remote_id: string; title: string; release_date: string; scene: LookupScene; found_scene_id: number | null; found_at: string; created_at: string; have_id: number }
