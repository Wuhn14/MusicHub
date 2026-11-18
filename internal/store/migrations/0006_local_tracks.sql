-- Tracks the user described by hand ("titre local") for files MusicBrainz
-- does not know. A file with status 'local' points to one through
-- files.local_track_id. cover_path is the base name of the uploaded cover in
-- the local-covers folder ('' when none).
CREATE TABLE local_tracks (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    artist       TEXT NOT NULL,
    album        TEXT NOT NULL DEFAULT '',
    album_artist TEXT NOT NULL DEFAULT '',
    disc         INTEGER NOT NULL DEFAULT 0,
    position     INTEGER NOT NULL DEFAULT 0,
    year         INTEGER NOT NULL DEFAULT 0,
    cover_path   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL
);
CREATE INDEX files_local_track_id ON files(local_track_id);

-- The name an uploaded file had on the sender's device (last path segment
-- only), shown in "À identifier". Never used as a path.
ALTER TABLE files ADD COLUMN upload_name TEXT NOT NULL DEFAULT '';

-- While the organizer moves a file, the path it is moved from. The row's
-- path already names the destination (so a scan finding the file there does
-- not add it twice); if MusicHub stops before the move is finished, startup
-- uses this to put the row back on the file that really exists.
ALTER TABLE files ADD COLUMN placing_from TEXT NOT NULL DEFAULT '';
