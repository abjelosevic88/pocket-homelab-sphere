-- A link that turned out to hold many videos (not a playlist yt-dlp knows,
-- often a page of short previews) waits for "download them all" instead of
-- fetching them.
ALTER TABLE downloads ADD COLUMN ask TEXT NOT NULL DEFAULT '';   -- '' | list
ALTER TABLE downloads ADD COLUMN found INTEGER NOT NULL DEFAULT 0; -- videos on the page
