package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/corani/lunacy/internal/logger"
)

func TestMigrateCanceledContext(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "lunacy.db"))
	must(t, err)
	defer db.Close() //nolint:errcheck
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := migrate(ctx, db, logger.New("test")); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled migration, got %v", err)
	}
	var version int
	must(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	if version != 0 {
		t.Fatalf("canceled migration advanced schema version to %d", version)
	}
	must(t, migrate(context.Background(), db, logger.New("test")))
}
