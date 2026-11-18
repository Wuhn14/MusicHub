-- When the last identification run of a file ended without a match (NULL:
-- never, or the file changed since). The startup backlog skips files that
-- were already tried.
ALTER TABLE files ADD COLUMN identify_attempted_at TEXT;
