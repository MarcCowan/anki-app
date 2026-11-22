package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %s: missing numeric prefix", e.Name())
		}
		v, err := strconv.Atoi(prefix)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("migration %s: bad numeric prefix", e.Name())
		}
		b, err := fs.ReadFile(migrationFS, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{version: v, name: e.Name(), sql: string(b)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	return ms, nil
}

// migrate applies every embedded migration newer than PRAGMA user_version,
// each in its own transaction that also bumps user_version.
func (s *Store) migrate(ctx context.Context) error { return s.migrateTo(ctx, 0) }

// migrateTo is migrate, stopping after version upTo (0 = no limit). Tests use
// it to build an older schema and seed it before the next migration runs.
func (s *Store) migrateTo(ctx context.Context, upTo int) error {
	ms, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if len(ms) > 0 {
		if latest := ms[len(ms)-1].version; current > latest {
			return fmt.Errorf("database schema v%d is newer than this build (v%d)", current, latest)
		}
	}
	for _, m := range ms {
		if m.version <= current || (upTo > 0 && m.version > upTo) {
			continue
		}
		err := s.withTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return err
			}
			// PRAGMA takes no parameters; the version is an int parsed from a file name.
			_, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(m.version))
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
	}
	return nil
}
