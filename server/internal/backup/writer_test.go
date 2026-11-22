package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newWriter(t *testing.T, src Snapshotter, now time.Time) (*Writer, *clock.Fake, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "backups")
	fc := &clock.Fake{T: now}
	return &Writer{Src: src, Root: root, Clock: fc, Loc: london(t), Log: discardLog()}, fc, root
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

type funcSnap func(ctx context.Context, path string) error

func (f funcSnap) VacuumInto(ctx context.Context, path string) error { return f(ctx, path) }

func TestWriteDaily(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.CreateDeck(ctx, "kept", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 3, 10, 0, 0, time.UTC)
	w, _, root := newWriter(t, s, now)
	got, err := w.Write(ctx, Daily)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "daily", FileName(now))
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	s2, err := store.Open(ctx, got)
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
		found = found || d.Name == "kept"
	}
	if !found {
		t.Errorf("deck missing from backup: %v", decks)
	}
	if runtime.GOOS != "windows" {
		for _, d := range []string{root, filepath.Join(root, "daily"), filepath.Join(root, "pre-deploy")} {
			fi, err := os.Stat(d)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o700 {
				t.Errorf("%s mode = %v, want 0700", d, fi.Mode().Perm())
			}
		}
	}
}

func TestWriteRemovesLeftoverPartials(t *testing.T) {
	ctx := context.Background()
	w, _, root := newWriter(t, newStore(t), time.Date(2026, 11, 5, 3, 10, 0, 0, time.UTC))
	dir := filepath.Join(root, "daily")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"anki-app-20260101T000000Z.db.partial", "notes.txt", "anki-app.db", "notes.partial", "anki-app.db.partial"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(root, "pre-deploy")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	otherPartial := filepath.Join(other, "anki-app-20260101T000000Z.db.partial")
	if err := os.WriteFile(otherPartial, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(ctx, Daily); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, n := range names(t, dir) {
		have[n] = true
	}
	if have["anki-app-20260101T000000Z.db.partial"] {
		t.Error("leftover partial survived")
	}
	for _, n := range []string{"notes.txt", "anki-app.db", "notes.partial", "anki-app.db.partial"} {
		if !have[n] {
			t.Errorf("unrelated file %s lost: %v", n, have)
		}
	}
	if _, err := os.Stat(otherPartial); err != nil {
		t.Errorf("other folder's partial removed: %v", err)
	}
}

func TestWriteFailureLeavesNoPartial(t *testing.T) {
	boom := errors.New("boom")
	snap := funcSnap(func(_ context.Context, path string) error {
		if err := os.WriteFile(path, []byte("some bytes"), 0o600); err != nil {
			return err
		}
		return boom
	})
	w, _, root := newWriter(t, snap, time.Date(2026, 11, 5, 3, 10, 0, 0, time.UTC))
	if _, err := w.Write(context.Background(), Daily); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if n := names(t, filepath.Join(root, "daily")); len(n) != 0 {
		t.Errorf("folder not empty: %v", n)
	}
}

func TestWriteQuickCheckFailure(t *testing.T) {
	snap := funcSnap(func(_ context.Context, path string) error {
		return os.WriteFile(path, []byte(strings.Repeat("not a database ", 500)), 0o600)
	})
	w, _, root := newWriter(t, snap, time.Date(2026, 11, 5, 3, 10, 0, 0, time.UTC))
	if _, err := w.Write(context.Background(), Daily); err == nil {
		t.Fatal("expected quick_check error")
	}
	if n := names(t, filepath.Join(root, "daily")); len(n) != 0 {
		t.Errorf("folder not empty: %v", n)
	}
}

func TestPreDeployKeepsTen(t *testing.T) {
	ctx := context.Background()
	w, fc, root := newWriter(t, newStore(t), time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC))
	dir := filepath.Join(root, "pre-deploy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep-me.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var times []time.Time
	for i := 0; i < 12; i++ {
		times = append(times, fc.Now())
		if _, err := w.Write(ctx, PreDeploy); err != nil {
			t.Fatal(err)
		}
		fc.Advance(time.Second)
	}
	all, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 10 {
		t.Fatalf("backups = %d, want 10", len(all))
	}
	for i, want := range times[2:] {
		if !all[i].Equal(want) {
			t.Errorf("survivor %d = %v, want %v", i, all[i], want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "keep-me.txt")); err != nil {
		t.Errorf("unrelated file gone: %v", err)
	}
}

func TestDailyAppliesRetention(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 5, 3, 10, 0, 0, time.UTC)
	w, _, root := newWriter(t, newStore(t), now)
	dir := filepath.Join(root, "daily")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var seeded []time.Time
	for i := 1; i <= 40; i++ {
		ts := now.AddDate(0, 0, -i)
		seeded = append(seeded, ts)
		if err := os.WriteFile(filepath.Join(dir, FileName(ts)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Write(ctx, Daily); err != nil {
		t.Fatal(err)
	}
	keep, _ := Keep(append(seeded, now), now, w.Loc)
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(keep) {
		t.Fatalf("got %d backups, want %d", len(got), len(keep))
	}
	for i := range got {
		if !got[i].Equal(keep[i]) {
			t.Errorf("backup %d = %v, want %v", i, got[i], keep[i])
		}
	}
	if len(keep) >= 41 {
		t.Error("retention dropped nothing; test is not exercising it")
	}
}
