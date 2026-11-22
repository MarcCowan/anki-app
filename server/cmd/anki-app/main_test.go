package main

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"anki-app/server/internal/clock"
)

func TestRunVersionCommand(t *testing.T) {
	old := version
	version = "v1.2.3-test"
	t.Cleanup(func() { version = old })

	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"version"}, func(string) string { return "" }, clock.Real{}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "v1.2.3-test\n" {
		t.Fatalf("stdout = %q", got)
	}
	if err := run(context.Background(), []string{"version", "extra"}, func(string) string { return "" }, clock.Real{}, &out, &errOut); err == nil {
		t.Fatal("version with an argument: want error")
	}
}

// syncBuffer lets the test read the log while serve writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestServeLogsVersion(t *testing.T) {
	old := version
	version = "v1.2.3-test"
	t.Cleanup(func() { version = old })

	db := filepath.Join(t.TempDir(), "serve.db")
	env := func(k string) string {
		switch k {
		case "DB_PATH":
			return db
		case "TZ":
			return "UTC"
		case "ADDR":
			return "127.0.0.1:0"
		}
		return ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"serve"}, env, &clock.Fake{T: time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)}, &bytes.Buffer{}, &log)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(log.String(), "msg=listening") {
		if time.Now().After(deadline) {
			t.Fatalf("no listening line; log = %q", log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(log.String(), "version=v1.2.3-test") {
		t.Fatalf("listening line has no version; log = %q", log.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// recordListen wraps net.Listen and records each requested address.
type recordListen struct {
	mu    sync.Mutex
	addrs []string
}

func (l *recordListen) listen(network, addr string) (net.Listener, error) {
	l.mu.Lock()
	l.addrs = append(l.addrs, addr)
	l.mu.Unlock()
	return net.Listen(network, addr)
}

func (l *recordListen) got() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.addrs...)
}

func serveWith(t *testing.T, extra map[string]string) ([]string, string) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "serve.db")
	env := func(k string) string {
		switch k {
		case "DB_PATH":
			return db
		case "TZ":
			return "UTC"
		case "ADDR":
			return "127.0.0.1:0"
		}
		return extra[k]
	}
	rec := &recordListen{}
	deps := defaultServeDeps()
	deps.Listen = rec.listen
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, env, &clock.Fake{T: time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)}, &log, deps)
	}()
	want := "msg=listening "
	if extra["MCP_PUBLIC_URL"] != "" {
		want = "msg=listening_public"
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(log.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q line; log = %q", want, log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return rec.got(), log.String()
}

func TestServeStartsPublicListenerWhenMCPEnabled(t *testing.T) {
	addrs, log := serveWith(t, map[string]string{
		"MCP_PUBLIC_URL": "https://anki.test:8443",
		"PASSPHRASE":     "correct horse battery staple",
		"MCP_ADDR":       "127.0.0.1:0",
	})
	if len(addrs) != 2 || addrs[0] != "127.0.0.1:0" || addrs[1] != "127.0.0.1:0" {
		t.Fatalf("listened on %v", addrs)
	}
	if !strings.Contains(log, "public_url=https://anki.test:8443") {
		t.Errorf("listening_public line lacks the URL: %q", log)
	}
}

func TestServeOneListenerWhenMCPOff(t *testing.T) {
	addrs, _ := serveWith(t, nil)
	if len(addrs) != 1 {
		t.Fatalf("listened on %v", addrs)
	}
}
