package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"anki-app/server/internal/backup"
	"anki-app/server/internal/clock"
	"anki-app/server/internal/config"
	"anki-app/server/internal/store"
)

func backupEnv(t *testing.T) (func(string) string, string) {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "b.db")
	bk := filepath.Join(dir, "bk")
	return func(k string) string {
		switch k {
		case "DB_PATH":
			return db
		case "BACKUP_DIR":
			return bk
		case "TZ":
			return "UTC"
		}
		return ""
	}, bk
}

func testBackupCommand(t *testing.T, args []string, sub string) {
	env, bk := backupEnv(t)
	st, err := store.Open(context.Background(), env("DB_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	var out, errOut bytes.Buffer
	clk := &clock.Fake{T: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
	if err := run(context.Background(), args, env, clk, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout = %q, want one line", out.String())
	}
	p := lines[0]
	if !filepath.IsAbs(p) {
		t.Fatalf("path %q is not absolute", p)
	}
	if filepath.Dir(p) != filepath.Join(bk, sub) {
		t.Fatalf("path %q not under %s", p, sub)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}

func TestBackupCommandDaily(t *testing.T) {
	testBackupCommand(t, []string{"backup"}, "daily")
}

func TestBackupCommandPreDeploy(t *testing.T) {
	testBackupCommand(t, []string{"backup", "--pre-deploy"}, "pre-deploy")
}

func TestBackupCommandMissingDB(t *testing.T) {
	env, bk := backupEnv(t)
	err := run(context.Background(), []string{"backup"}, env, clock.Real{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v, want database does not exist", err)
	}
	if _, statErr := os.Stat(env("DB_PATH")); statErr == nil {
		t.Fatal("backup created the database")
	}
	if _, statErr := os.Stat(bk); statErr == nil {
		t.Fatal("backup created the backup dir")
	}
}

func TestBackupCommandRejectsUnknownArgs(t *testing.T) {
	env, _ := backupEnv(t)
	for _, args := range [][]string{{"backup", "--nope"}, {"backup", "--pre-deploy", "x"}} {
		err := run(context.Background(), args, env, clock.Real{}, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || err.Error() != usage {
			t.Errorf("%v: err = %v, want usage", args, err)
		}
	}
}

func TestUsageText(t *testing.T) {
	const want = "usage: anki-app [serve] | anki-app import <file.json> | anki-app backup [--pre-deploy] | anki-app version"
	err := run(context.Background(), []string{"bogus"}, func(string) string { return "" }, clock.Real{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// slowSnap blocks inside VacuumInto until release is closed, then runs the
// real snapshot.
type slowSnap struct {
	real    backup.Snapshotter
	started chan struct{}
	release chan struct{}
}

func (s *slowSnap) VacuumInto(ctx context.Context, path string) error {
	close(s.started)
	<-s.release
	return s.real.VacuumInto(ctx, path)
}

func TestServeWaitsForSchedulerBeforeClosingStore(t *testing.T) {
	env, _ := backupEnv(t)
	addr := func(k string) string {
		if k == "ADDR" {
			return "127.0.0.1:0"
		}
		return env(k)
	}
	snap := &slowSnap{started: make(chan struct{}), release: make(chan struct{})}
	d := defaultServeDeps()
	d.NewBackupWriter = func(st *store.Store, cfg config.Config, clk clock.Clock, log *slog.Logger) *backup.Writer {
		snap.real = st
		w := backupWriter(st, cfg, clk, log)
		w.Src = snap
		return w
	}

	clk := &clock.Fake{T: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncBuffer
	done := make(chan error, 1)
	go func() { done <- serve(ctx, addr, clk, &log, d) }()

	select {
	case <-snap.started:
	case <-time.After(10 * time.Second):
		t.Fatalf("backup never started; log = %q", log.String())
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("run returned (%v) while a backup was still running", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(snap.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after the backup finished")
	}
	if !strings.Contains(log.String(), "scheduled backup interrupted by shutdown") {
		t.Fatalf("missing interrupted-by-shutdown line; log = %q", log.String())
	}
}

// errListener fails Accept once told to, like a dying listener.
type errListener struct {
	net.Listener
	fail chan struct{}
}

func (l *errListener) Accept() (net.Conn, error) {
	<-l.fail
	return nil, errors.New("accept failed")
}

func TestServeReturnsServeErrorPromptly(t *testing.T) {
	env, _ := backupEnv(t)
	addr := func(k string) string {
		if k == "ADDR" {
			return "127.0.0.1:0"
		}
		return env(k)
	}
	el := &errListener{fail: make(chan struct{})}
	d := defaultServeDeps()
	d.Listen = func(network, address string) (net.Listener, error) {
		ln, err := net.Listen(network, address)
		el.Listener = ln
		return el, err
	}

	clk := &clock.Fake{T: time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)}
	var log syncBuffer
	done := make(chan error, 1)
	go func() { done <- serve(context.Background(), addr, clk, &log, d) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(log.String(), "msg=listening") {
		if time.Now().After(deadline) {
			t.Fatalf("no listening line; log = %q", log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(el.fail)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want the serve error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run hung after a serve error (scheduler not stopped)")
	}
}
