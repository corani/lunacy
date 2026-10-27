package store

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store wraps the application SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the lunacy.db at dataDir and applies migrations.
func Open(dataDir string) (*Store, error) {
	path := filepath.Join(dataDir, "lunacy.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for use by query helpers.
func (s *Store) DB() *sql.DB {
	return s.db
}
