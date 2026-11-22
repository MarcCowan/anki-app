package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func pragma(t *testing.T, s *Store, name string) string {
	t.Helper()
	var v string
	if err := s.db.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenAppliesPragmas(t *testing.T) {
	s := openTest(t)
	if v := pragma(t, s, "foreign_keys"); v != "1" {
		t.Errorf("foreign_keys = %s", v)
	}
	if v := pragma(t, s, "journal_mode"); v != "wal" {
		t.Errorf("journal_mode = %s", v)
	}
	if v := pragma(t, s, "busy_timeout"); v != "5000" {
		t.Errorf("busy_timeout = %s", v)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	// A directory with a space checks the DSN handles Windows-style paths.
	path := filepath.Join(t.TempDir(), "with space", "x.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v := pragma(t, s, "user_version"); v != "4" {
		t.Errorf("user_version = %s", v)
	}
}

func TestSettingsSeeded(t *testing.T) {
	s := openTest(t)
	got := map[string]string{}
	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		got[k] = v
	}
	if got["desired_retention"] != "0.9" || got["day_rollover_hour"] != "4" {
		t.Fatalf("settings = %v", got)
	}
}
