package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/corani/lunacy/internal/logger"
)

// migrate applies any pending migrations to db using PRAGMA user_version.
func migrate(ctx context.Context, db *sql.DB, log *logger.Logger) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK") //nolint:errcheck
	var current int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	for i := current; i < len(migrations); i++ {
		m := migrations[i]
		log.Info("applying migration", "version", i+1, "title", m.title)
		if _, err := conn.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("migration %d (%s): %w", i+1, m.title, err)
		}
		// user_version must use Sprintf; PRAGMA doesn't accept parameters.
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			return fmt.Errorf("set user_version %d: %w", i+1, err)
		}
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}
