CREATE TABLE artists (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    sort_name      TEXT NOT NULL DEFAULT '',
    disambiguation TEXT NOT NULL DEFAULT '',
    country        TEXT NOT NULL DEFAULT '',
    fetched_at     TEXT NOT NULL
);

-- An album is a MusicBrainz release group. artist_id is deliberately not a
-- foreign key: an album can be cached before its artist.
CREATE TABLE albums (
    id                 TEXT PRIMARY KEY,
    title              TEXT NOT NULL,
    artist_id          TEXT NOT NULL,
    artist_name        TEXT NOT NULL DEFAULT '',
    primary_type       TEXT NOT NULL DEFAULT '',
    secondary_types    TEXT NOT NULL DEFAULT '[]',
    first_release_date TEXT NOT NULL DEFAULT '',
    fetched_at         TEXT NOT NULL
);
CREATE INDEX albums_artist_id ON albums(artist_id);

CREATE TABLE recordings (
    id        TEXT PRIMARY KEY,
    title     TEXT NOT NULL,
    length_ms INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE album_tracks (
    album_id     TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    disc         INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    recording_id TEXT NOT NULL REFERENCES recordings(id),
    PRIMARY KEY (album_id, disc, position)
);
