package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/corani/lunacy/internal/logger"
	_ "modernc.org/sqlite"
)

// Store wraps the application SQLite database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (or creates) the lunacy.db at dataDir and applies migrations.
func Open(ctx context.Context, dataDir string, log *logger.Logger) (*Store, error) {
	path := filepath.Join(dataDir, "lunacy.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(30000)")
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := migrate(ctx, db, log); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for use by query helpers.
func (s *Store) DB() *sql.DB {
	return s.db
}
