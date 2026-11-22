package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
)

// Ping checks that the database connection is usable.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// VacuumInto writes a consistent snapshot (including rows still in the WAL)
// to path. SQLite refuses to overwrite an existing file.
func (s *Store) VacuumInto(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}

// QuickCheck runs PRAGMA quick_check on the database file at path, read-only.
// The file must be a finished snapshot: immutable avoids creating -shm/-wal.
func QuickCheck(ctx context.Context, path string) (err error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&immutable=1")
	if err != nil {
		return fmt.Errorf("quick_check: open: %w", err)
	}
	defer func() {
		if cerr := db.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("quick_check: close: %w", cerr)
		}
	}()
	var row string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&row); err != nil {
		return fmt.Errorf("quick_check: %w", err)
	}
	if row != "ok" {
		return fmt.Errorf("quick_check: %s", row)
	}
	return nil
}
