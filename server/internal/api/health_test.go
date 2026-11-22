package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"anki-app/server/internal/backup"
	"anki-app/server/internal/config"
)

type healthBody struct {
	Version      string `json:"version"`
	LastBackupAt *int64 `json:"lastBackupAt"`
}

func TestHealthNoBackupProvider(t *testing.T) {
	s := newServer(t)
	w := s.do("GET", "/api/health", "")
	expectStatus(t, w, 200)
	b := decode[healthBody](t, w)
	if b.Version != "abc123" || b.LastBackupAt != nil {
		t.Fatalf("body = %+v", b)
	}
}

func TestHealthLastBackupSet(t *testing.T) {
	s := newServerWith(t, func() (time.Time, bool, error) { return t0, true, nil })
	b := decode[healthBody](t, s.do("GET", "/api/health", ""))
	if b.LastBackupAt == nil || *b.LastBackupAt != t0.UnixMilli() {
		t.Fatalf("body = %+v", b)
	}
}

func TestHealthLastBackupNone(t *testing.T) {
	s := newServerWith(t, func() (time.Time, bool, error) { return time.Time{}, false, nil })
	if b := decode[healthBody](t, s.do("GET", "/api/health", "")); b.LastBackupAt != nil {
		t.Fatalf("body = %+v", b)
	}
}

func TestHealthLastBackupErrorIsNull(t *testing.T) {
	s := newServerWith(t, func() (time.Time, bool, error) { return time.Time{}, false, os.ErrPermission })
	w := s.do("GET", "/api/health", "")
	expectStatus(t, w, 200)
	if b := decode[healthBody](t, w); b.LastBackupAt != nil {
		t.Fatalf("body = %+v", b)
	}
}

func TestHealthNeedsNoSession(t *testing.T) {
	s := newServer(t, func(c *config.Config) { c.Passphrase = "correct horse battery" })
	expectStatus(t, s.do("GET", "/api/health", ""), 200)
	expectError(t, s.do("GET", "/api/decks", ""), 401, "unauthorized")
}

func TestHealthRejectsBadHost(t *testing.T) {
	s := newServer(t)
	expectStatus(t, s.do("GET", "/api/health", "", map[string]string{"Host": "evil.example"}), 403)
}

func TestHealthUnavailableWhenDBClosed(t *testing.T) {
	s := newServer(t)
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	expectError(t, s.do("GET", "/api/health", ""), 503, "unavailable")
}

func TestHealthLastBackupIgnoresPartials(t *testing.T) {
	dir := t.TempDir()
	real := t0.Add(-time.Hour)
	if err := os.WriteFile(filepath.Join(dir, backup.FileName(real)), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	newer := filepath.Join(dir, backup.FileName(t0)+".partial")
	if err := os.WriteFile(newer, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newServerWith(t, func() (time.Time, bool, error) { return backup.Latest(dir) })
	b := decode[healthBody](t, s.do("GET", "/api/health", ""))
	if b.LastBackupAt == nil || *b.LastBackupAt != real.UnixMilli() {
		t.Fatalf("body = %+v, want %d", b, real.UnixMilli())
	}
}
