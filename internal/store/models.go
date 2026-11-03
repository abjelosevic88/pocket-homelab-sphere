package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Ref is a named thing a scene points at (performer, tag, studio, group).
type Ref struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image,omitempty"`
	Extra string `json:"extra,omitempty"` // a performer's disambiguation, a group's scene number
}

// Mine is one person's relation to a scene.
type Mine struct {
	Rating        int     `json:"rating"`
	Favorite      bool    `json:"favorite"`
	OCount        int     `json:"o_count"`
	PlayCount     int     `json:"play_count"`
	PlaySeconds   int64   `json:"play_seconds"`
	ResumeSeconds float64 `json:"resume_seconds"`
	LastPlayedAt  string  `json:"last_played_at"`
	Hidden        bool    `json:"hidden"`
}

// Scene is one video in the library.
type Scene struct {
	ID           int64             `json:"id"`
	Path         string            `json:"path"`
	SizeBytes    int64             `json:"size_bytes"`
	FileMtime    string            `json:"file_mtime"`
	OSHash       string            `json:"oshash"`
	PHash        string            `json:"phash"`
	Duration     float64           `json:"duration"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	FPS          float64           `json:"fps"`
	VideoCodec   string            `json:"video_codec"`
	AudioCodec   string            `json:"audio_codec"`
	Container    string            `json:"container"`
	Bitrate      int64             `json:"bitrate"`
	ProbedAt     string            `json:"-"`
	Title        string            `json:"title"`
	Code         string            `json:"code"`
	Details      string            `json:"details"`
	Director     string            `json:"director"`
	Date         string            `json:"date"`
	URLs         []string          `json:"urls"`
	StudioID     *int64            `json:"studio_id"`
	Organized    bool              `json:"organized"`
	StashIDs     map[string]string `json:"stash_ids"`
	SourceURL    string            `json:"source_url"`
	Projection   string            `json:"projection"`
	Stereo       string            `json:"stereo"`
	FOV          int               `json:"fov"`
	VRSet        bool              `json:"vr_set"`
	CoverPath    string            `json:"-"`
	CoverSource  string            `json:"cover_source"`
	GenState     string            `json:"gen_state"`
	GenError     string            `json:"gen_error"`
	GenVersion   int               `json:"gen_version"`
	HasPreview   bool              `json:"has_preview"`
	HasSprite    bool              `json:"has_sprite"`
	MissingSince string            `json:"missing_since"`
	TrashedAt    string            `json:"trashed_at"`
	TrashPath    string            `json:"-"`
	AddedBy      *int64            `json:"added_by"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`

	// joined for lists and pages
	Studio     *Ref  `json:"studio"`
	Performers []Ref `json:"performers"`
	Tags       []Ref `json:"tags"`
	Groups     []Ref `json:"groups,omitempty"`
	Markers    int   `json:"markers"`
	Mine       Mine  `json:"mine"`

	// filled in by the server
	Name    string `json:"name"`    // the title, or the file name
	Cover   string `json:"cover"`   // URL
	Preview string `json:"preview"` // URL of the hover clip ('' = none)
	Sprite  string `json:"sprite"`  // URL of the scrubber picture sheet ('' = none)
	IsVR    bool   `json:"is_vr"`
	VRLabel string `json:"vr_label"` // "VR 180° · side by side"
}

// SceneFilter narrows a scene list.
type SceneFilter struct {
	UserID      int64
	Query       string
	Performers  []int64
	Tags        []int64
	TagsAll     bool // every tag (default: any)
	Studios     []int64
	Groups      []int64
	RatingMin   int
	Favorite    bool
	Unwatched   bool
	Watched     bool
	InProgress  bool
	VR          string // '' | yes | no
	MinHeight   int
	MaxHeight   int
	MinDuration int // seconds
	MaxDuration int
	Organized   string // '' | yes | no
	HasMarkers  bool
	Folder      string
	Missing     bool // only files that disappeared
	Trashed     bool
	Hidden      bool // include hidden scenes
	IDs         []int64
	Sort        string // date | added | title | rating | duration | plays | played | o | size | resolution | random | path | updated
	Desc        bool
	Seed        int64
	Limit       int
	Offset      int
}

// Performer is a person in scenes.
type Performer struct {
	ID             int64             `json:"id"`
	Name           string            `json:"name"`
	Disambiguation string            `json:"disambiguation"`
	Aliases        string            `json:"aliases"`
	Gender         string            `json:"gender"`
	Birthdate      string            `json:"birthdate"`
	DeathDate      string            `json:"death_date"`
	Country        string            `json:"country"`
	Ethnicity      string            `json:"ethnicity"`
	HairColor      string            `json:"hair_color"`
	EyeColor       string            `json:"eye_color"`
	HeightCM       int               `json:"height_cm"`
	WeightKG       int               `json:"weight_kg"`
	Measurements   string            `json:"measurements"`
	FakeTits       string            `json:"fake_tits"`
	Tattoos        string            `json:"tattoos"`
	Piercings      string            `json:"piercings"`
	Career         string            `json:"career"`
	Details        string            `json:"details"`
	URLs           []string          `json:"urls"`
	ImagePath      string            `json:"-"`
	StashIDs       map[string]string `json:"stash_ids"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`

	Scenes    int    `json:"scenes"`
	Galleries int    `json:"galleries"`
	Favorite  bool   `json:"favorite"`
	Rating    int    `json:"rating"`
	OCount    int    `json:"o_count"`
	Image     string `json:"image"`
	Age       int    `json:"age"`
}

// Studio makes scenes.
type Studio struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	ParentID  *int64            `json:"parent_id"`
	Aliases   string            `json:"aliases"`
	URL       string            `json:"url"`
	Details   string            `json:"details"`
	ImagePath string            `json:"-"`
	StashIDs  map[string]string `json:"stash_ids"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`

	Parent   *Ref   `json:"parent"`
	Children []Ref  `json:"children,omitempty"`
	Scenes   int    `json:"scenes"`
	Favorite bool   `json:"favorite"`
	Rating   int    `json:"rating"`
	Image    string `json:"image"`
}

// Tag labels scenes and markers.
type Tag struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Aliases     string `json:"aliases"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id"`
	ImagePath   string `json:"-"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`

	Parent   *Ref   `json:"parent"`
	Children []Ref  `json:"children,omitempty"`
	Scenes   int    `json:"scenes"`
	Markers  int    `json:"markers"`
	Favorite bool   `json:"favorite"`
	Image    string `json:"image"`
}

// Marker is a moment in a scene.
type Marker struct {
	ID           int64    `json:"id"`
	SceneID      int64    `json:"scene_id"`
	Title        string   `json:"title"`
	Seconds      float64  `json:"seconds"`
	EndSeconds   *float64 `json:"end_seconds"`
	PrimaryTagID *int64   `json:"primary_tag_id"`
	CreatedAt    string   `json:"created_at"`

	Tag        *Ref   `json:"tag"`
	SceneName  string `json:"scene_name,omitempty"`
	Stream     string `json:"stream,omitempty"`  // the scene's video
	Preview    string `json:"preview,omitempty"` // a short clip of the moment
	Screenshot string `json:"screenshot,omitempty"`
}

// Group is a movie or series: scenes in an order.
type Group struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Aliases   string   `json:"aliases"`
	Date      string   `json:"date"`
	Director  string   `json:"director"`
	Synopsis  string   `json:"synopsis"`
	StudioID  *int64   `json:"studio_id"`
	URLs      []string `json:"urls"`
	FrontPath string   `json:"-"`
	BackPath  string   `json:"-"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`

	Studio   *Ref    `json:"studio"`
	Scenes   int     `json:"scenes"`
	Duration float64 `json:"duration"`
	Favorite bool    `json:"favorite"`
	Rating   int     `json:"rating"`
	Front    string  `json:"front"`
	Back     string  `json:"back"`
}

// Gallery is a folder or zip of pictures.
type Gallery struct {
	ID           int64    `json:"id"`
	Path         string   `json:"path"`
	IsZip        bool     `json:"is_zip"`
	Title        string   `json:"title"`
	Date         string   `json:"date"`
	Details      string   `json:"details"`
	Photographer string   `json:"photographer"`
	URLs         []string `json:"urls"`
	StudioID     *int64   `json:"studio_id"`
	SceneID      *int64   `json:"scene_id"`
	ImageCount   int      `json:"image_count"`
	CoverImage   int64    `json:"cover_image"`
	FileMtime    string   `json:"-"`
	MissingSince string   `json:"missing_since"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`

	Name       string `json:"name"`
	Studio     *Ref   `json:"studio"`
	Performers []Ref  `json:"performers"`
	Tags       []Ref  `json:"tags"`
	Favorite   bool   `json:"favorite"`
	Rating     int    `json:"rating"`
	Cover      string `json:"cover"`
}

// Image is one picture of a gallery.
type Image struct {
	ID        int64  `json:"id"`
	GalleryID int64  `json:"gallery_id"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	SizeBytes int64  `json:"size_bytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	URL       string `json:"url"`
	Thumb     string `json:"thumb"`
}

// Play is one sitting with a scene.
type Play struct {
	ID        int64  `json:"id"`
	SceneID   int64  `json:"scene_id"`
	SceneName string `json:"scene_name,omitempty"`
	StartedAt string `json:"started_at"`
	Seconds   int64  `json:"seconds"`
	Counted   bool   `json:"counted"`
	Device    string `json:"device"`
	Day       string `json:"day"`
}

// SavedFilter is a named list view.
type SavedFilter struct {
	ID        int64  `json:"id"`
	Mode      string `json:"mode"`
	Name      string `json:"name"`
	Query     string `json:"query"`
	Pinned    bool   `json:"pinned"`
	CreatedAt string `json:"created_at"`
}

// DownloadOptions are how a link is fetched.
type DownloadOptions struct {
	Quality    string  `json:"quality,omitempty"` // best | 2160 | 1440 | 1080 | 720 | 480
	Format     string  `json:"format,omitempty"`  // auto | mp4 | mkv | webm
	Folder     string  `json:"folder,omitempty"`  // inside the library ('' = the person's default)
	NamePrefix string  `json:"name_prefix,omitempty"`
	ClipStart  string  `json:"clip_start,omitempty"` // "2:26"
	ClipEnd    string  `json:"clip_end,omitempty"`
	ItemsLimit int     `json:"items_limit,omitempty"` // playlists: the newest N
	Password   string  `json:"password,omitempty"`    // a video password (never shown back)
	AutoStart  *bool   `json:"auto_start,omitempty"`
	OwnFolder  *bool   `json:"own_folder,omitempty"` // one folder per video
	Performers []int64 `json:"performers,omitempty"` // put on the scene once it is in
	Tags       []int64 `json:"tags,omitempty"`
	StudioID   int64   `json:"studio_id,omitempty"`
	// From Discover: one video only (a page of many clips is not it), and the
	// length the scene should have (a much shorter file is only a preview).
	Single        bool `json:"single,omitempty"`
	ExpectSeconds int  `json:"expect_seconds,omitempty"`
	// A page with many videos (not a playlist): fetch them all after all.
	AllowList bool `json:"allow_list,omitempty"`
}

// Download is a video being fetched from a link.
type Download struct {
	ID             string          `json:"id"`
	UserID         int64           `json:"-"`
	ParentID       string          `json:"parent_id"`
	SubscriptionID *int64          `json:"subscription_id"`
	URL            string          `json:"url"`
	Kind           string          `json:"kind"`
	Title          string          `json:"title"`
	Thumbnail      string          `json:"thumbnail"`
	Options        DownloadOptions `json:"options"`
	Status         string          `json:"status"`
	Percent        float64         `json:"percent"`
	Speed          string          `json:"speed"`
	ETA            string          `json:"eta"`
	SizeBytes      int64           `json:"size_bytes"`
	FilePath       string          `json:"file_path"`
	SceneID        *int64          `json:"scene_id"`
	Error          string          `json:"error"`
	Ask            string          `json:"ask"`   // '' | list (many videos on the page: fetch them all?)
	Found          int             `json:"found"` // videos on the page
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

// Subscription is a channel or playlist checked for new videos.
type Subscription struct {
	ID            int64           `json:"id"`
	UserID        int64           `json:"-"`
	URL           string          `json:"url"`
	Name          string          `json:"name"`
	Options       DownloadOptions `json:"options"`
	CheckMinutes  int             `json:"check_minutes"`
	TitleFilter   string          `json:"title_filter"`
	Enabled       bool            `json:"enabled"`
	LastCheckedAt string          `json:"last_checked_at"`
	LastError     string          `json:"last_error"`
	Found         int             `json:"found"`
	CreatedAt     string          `json:"created_at"`
}

// Settings are the box-wide values plus one person's preferences.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`

	// Lists.
	GridSize     string `json:"grid_size"`     // s | m | l
	DefaultSort  string `json:"default_sort"`  // date | added | played | title | rating | random …
	HoverPreview bool   `json:"hover_preview"` // play the short clip when pointing at a card
	ShowMissing  bool   `json:"show_missing"`

	// Privacy.
	Discreet    bool   `json:"discreet"`      // blur pictures until pointed at
	PanicURL    string `json:"panic_url"`     // Esc twice goes here ('' = off)
	LockPINHash string `json:"lock_pin_hash"` // '' = no lock
	LockMinutes int    `json:"lock_minutes"`  // lock after this long away (0 = only on start)
	HeadsetKey  string `json:"headset_key"`   // the secret in the headset address ('' = off)

	// The player.
	AutoPlay      bool   `json:"auto_play"`       // start playing when a scene opens
	ContinueQueue bool   `json:"continue_queue"`  // play the next one of the list at the end
	ResumePlay    bool   `json:"resume_play"`     // continue where you stopped
	PlayerVolume  int    `json:"player_volume"`   // 0..100
	StreamQuality string `json:"stream_quality"`  // original | 2160 | 1080 | 720 | 480 (when the file must be converted)
	SkipSeconds   int    `json:"skip_seconds"`    // ← → jump
	VRDefaultMode string `json:"vr_default_mode"` // view | flat — how VR scenes open
	CountAfter    int    `json:"count_after"`     // seconds watched before a sitting counts as a play

	// Downloads.
	DownloadFolder  string `json:"download_folder"`  // inside the library
	DownloadQuality string `json:"download_quality"` // best | 2160 | 1440 | 1080 | 720 | 480
	DownloadFormat  string `json:"download_format"`  // auto | mp4 | mkv | webm
	OwnFolder       bool   `json:"own_folder"`       // one folder per video (with its picture), like the rest of the library
	SiteTags        bool   `json:"site_tags"`        // take the site's tags
	UploaderStudio  bool   `json:"uploader_studio"`  // the uploader / channel becomes the studio
	AutoIdentify    bool   `json:"auto_identify"`    // look a new video up on StashDB / TPDB

	// Looking scenes up.
	StashDBKey  string `json:"stashdb_key"`
	StashBoxURL string `json:"stashbox_url"` // another stash-box instance ('' = StashDB)
	TPDBKey     string `json:"tpdb_key"`

	// Telling.
	NotifyURL       string `json:"notify_url"`
	NotifyFormat    string `json:"notify_format"` // apprise | ntfy | json
	NotifyDownloads bool   `json:"notify_downloads"`
	NotifyQuiet     bool   `json:"notify_quiet"` // no titles in notifications

	// AI (the Ask panel and descriptions).
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }

// Lines splits a multi-line setting into its trimmed, non-empty lines.
func Lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
