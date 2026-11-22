package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func startScheduler(w *Writer, every time.Duration) (cancel func(), done <-chan struct{}) {
	ctx, c := context.WithCancel(context.Background())
	d := make(chan struct{})
	go func() { defer close(d); w.RunScheduler(ctx, every) }()
	return c, d
}

func stopScheduler(t *testing.T, cancel func(), done <-chan struct{}) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop after cancel")
	}
}

func TestSchedulerTakesOneBackupPerDay(t *testing.T) {
	loc := london(t)
	w, _, root := newWriter(t, newStore(t), time.Date(2026, 11, 5, 3, 10, 0, 0, loc))
	dir := filepath.Join(root, "daily")
	cancel, done := startScheduler(w, 10*time.Millisecond)
	waitFor(t, "first backup", func() bool { l, _ := List(dir); return len(l) >= 1 })
	time.Sleep(100 * time.Millisecond) // many more ticks
	stopScheduler(t, cancel, done)
	if l, _ := List(dir); len(l) != 1 {
		t.Errorf("backups = %d, want 1", len(l))
	}
}

func TestSchedulerLogsFailureAndContinues(t *testing.T) {
	var mu sync.Mutex
	n := 0
	snap := funcSnap(func(context.Context, string) error {
		mu.Lock()
		n++
		mu.Unlock()
		return errors.New("disk on fire")
	})
	w, _, _ := newWriter(t, snap, time.Date(2026, 11, 5, 3, 10, 0, 0, time.UTC))
	buf := &syncBuf{}
	w.Log = slog.New(slog.NewTextHandler(buf, nil))
	cancel, done := startScheduler(w, 5*time.Millisecond)
	// A second attempt proves it kept running after the first failure.
	waitFor(t, "two attempts", func() bool { mu.Lock(); defer mu.Unlock(); return n >= 2 })
	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Errorf("no ERROR log: %s", buf.String())
	}
	select {
	case <-done:
		t.Fatal("scheduler exited on failure")
	default:
	}
	stopScheduler(t, cancel, done)
}
