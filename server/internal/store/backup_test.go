package store

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestVacuumIntoIncludesWALRows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateDeck(ctx, "walled", false, t0); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "snap.db")
	if err := s.VacuumInto(ctx, dst); err != nil {
		t.Fatal(err)
	}
	if err := QuickCheck(ctx, dst); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	decks, err := s2.ListDecks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range decks {
		found = found || d.Name == "walled"
	}
	if !found {
		t.Errorf("deck missing from snapshot: %v", decks)
	}
}

func TestVacuumIntoRefusesExistingFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dst := filepath.Join(dir, "exists.db")
	if err := s.VacuumInto(ctx, dst); err != nil {
		t.Fatal(err)
	}
	if err := s.VacuumInto(ctx, dst); err == nil {
		t.Fatal("expected error for existing target")
	}
}

func TestQuickCheckRejectsGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "junk.db")
	b := make([]byte, 4096)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := QuickCheck(context.Background(), p); err == nil {
		t.Fatal("expected error")
	}
}

func TestPingFailsWhenClosed(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := s.Ping(ctx); err == nil {
		t.Fatal("expected error after Close")
	}
}
