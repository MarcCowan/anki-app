package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestOpenRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "n.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	latest := ms[len(ms)-1].version
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", latest+1)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Open(ctx, path)
	want := fmt.Sprintf("database schema v%d is newer than this build (v%d)", latest+1, latest)
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// Migration 0002 rewrites em dashes (U+2014) in card text to en dashes (U+2013).
func TestMigration0002ReplacesEmDashes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.CreateDeck(ctx, "d", false, t0)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCard(ctx, d.ID, NewCard{Front: "a — b", Back: "x—y – keep"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	// Re-run migration 0002's SQL on its own (rewinding user_version would
	// also re-run later migrations, which aren't repeatable).
	m2, err := migrationFS.ReadFile("migrations/0002_en_dashes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(string(m2)); err != nil {
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
	got, err := s.GetCard(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Front != "a – b" || got.Back != "x–y – keep" {
		t.Errorf("got front=%q back=%q", got.Front, got.Back)
	}
	if v := pragma(t, s, "user_version"); v != "4" {
		t.Errorf("user_version = %s", v)
	}
}

func TestMigration0003CreatesMCPTables(t *testing.T) {
	s := openTest(t)
	for _, tbl := range []string{"oauth_clients", "oauth_codes", "oauth_grants", "oauth_tokens", "mcp_batches"} {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("table %s missing", tbl)
		}
	}
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('cards') WHERE name = 'batch_id'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("cards.batch_id missing")
	}
}
