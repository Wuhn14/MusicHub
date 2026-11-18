-- Persistent background jobs (identification for now). A file has at most
-- one job of each kind. Times are Unix nanoseconds.
CREATE TABLE jobs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL,
    file_id    TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    attempts   INTEGER NOT NULL DEFAULT 0,
    run_after  INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE (kind, file_id)
);
CREATE INDEX jobs_run_after ON jobs(run_after);

-- The best matches proposed for a file that could not be identified
-- automatically. Score is 0 to 100.
CREATE TABLE identification_candidates (
    file_id      TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    rank         INTEGER NOT NULL,
    recording_id TEXT NOT NULL,
    album_id     TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL DEFAULT '',
    artist       TEXT NOT NULL DEFAULT '',
    album        TEXT NOT NULL DEFAULT '',
    length_ms    INTEGER NOT NULL DEFAULT 0,
    score        INTEGER NOT NULL DEFAULT 0,
    source       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (file_id, rank)
);

CREATE INDEX files_recording_id ON files(recording_id);
CREATE INDEX files_album_id ON files(album_id);
CREATE INDEX album_tracks_recording_id ON album_tracks(recording_id);
