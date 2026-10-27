package store

// migrations holds SQL statements applied in order, indexed by version (1-based).
// To change the schema: append a new migration. Never edit existing entries.
var migrations = []string{
	// v1: initial schema
	`CREATE TABLE IF NOT EXISTS books (
		filename      TEXT PRIMARY KEY,
		title         TEXT NOT NULL DEFAULT '',
		author        TEXT NOT NULL DEFAULT '',
		category      TEXT NOT NULL DEFAULT '',
		series        TEXT NOT NULL DEFAULT '',
		series_index  REAL NOT NULL DEFAULT 0,
		favorite      TEXT NOT NULL DEFAULT '',
		rate          INTEGER NOT NULL DEFAULT 0,
		add_time      INTEGER NOT NULL DEFAULT 0,
		cover_file    TEXT NOT NULL DEFAULT '',
		is_stub       INTEGER NOT NULL DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS positions (
		filename      TEXT NOT NULL PRIMARY KEY REFERENCES books(filename) ON DELETE CASCADE,
		chapter       INTEGER,
		split_index   INTEGER,
		char_offset   INTEGER,
		page          INTEGER,
		pct           REAL NOT NULL DEFAULT 0,
		last_read     INTEGER NOT NULL DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS annotations (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		filename       TEXT NOT NULL REFERENCES books(filename) ON DELETE CASCADE,
		ann_id         INTEGER NOT NULL,
		chapter        INTEGER,
		split_index    INTEGER,
		char_offset    INTEGER,
		page           INTEGER,
		length         INTEGER NOT NULL DEFAULT 0,
		color          INTEGER NOT NULL DEFAULT 0,
		created_at     INTEGER NOT NULL DEFAULT 0,
		ann_type       INTEGER NOT NULL DEFAULT 1,
		underline      INTEGER NOT NULL DEFAULT 0,
		strikethrough  INTEGER NOT NULL DEFAULT 0,
		note           TEXT NOT NULL DEFAULT '',
		text           TEXT NOT NULL DEFAULT '',
		chapter_label  TEXT NOT NULL DEFAULT '',
		UNIQUE (filename, ann_id)
	)`,

	`CREATE TABLE IF NOT EXISTS reading_days (
		filename      TEXT NOT NULL REFERENCES books(filename) ON DELETE CASCADE,
		day           INTEGER NOT NULL,
		read_time_s   INTEGER NOT NULL DEFAULT 0,
		words_read    INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (filename, day)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_books_author   ON books(author)`,
	`CREATE INDEX IF NOT EXISTS idx_books_favorite ON books(favorite)`,
	`CREATE INDEX IF NOT EXISTS idx_books_series   ON books(series)`,

	// v2: book_authors join table for multi-author support
	`CREATE TABLE IF NOT EXISTS book_authors (
		filename  TEXT NOT NULL REFERENCES books(filename) ON DELETE CASCADE,
		author    TEXT NOT NULL,
		PRIMARY KEY (filename, author)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_book_authors_author ON book_authors(author)`,

	// v3: description field
	`ALTER TABLE books ADD COLUMN description TEXT NOT NULL DEFAULT ''`,

	// v4: read_state from Moon+ Read Filter (0=unread, 1=reading, 3=finished)
	`ALTER TABLE books ADD COLUMN read_state INTEGER NOT NULL DEFAULT 0`,

	// v5: pct per reading day; read_time_ms added (read_time_s repurposed to store ms)
	`ALTER TABLE reading_days ADD COLUMN pct REAL NOT NULL DEFAULT 0`,
}
