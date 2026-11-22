// Command anki-app is the flashcard server.
//
// Usage:
//
//	anki-app [serve]            run the web server (default)
//	anki-app import <file.json> create a deck from a seed file
//	anki-app backup [--pre-deploy] write one database backup and print its path
//	anki-app version            print the build version
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"anki-app/server/internal/api"
	"anki-app/server/internal/backup"
	"anki-app/server/internal/clock"
	"anki-app/server/internal/config"
	"anki-app/server/internal/store"
	"anki-app/server/internal/study"
	"anki-app/server/internal/webui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, clock.Real{}, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "anki-app:", err)
		os.Exit(1)
	}
}

// version is set at build time by scripts/build.sh:
// -ldflags "-X main.version=<git describe>".
var version = "dev"

const usage = "usage: anki-app [serve] | anki-app import <file.json> | anki-app backup [--pre-deploy] | anki-app version"

func run(ctx context.Context, args []string, getenv func(string) string, clk clock.Clock, stdout, stderr io.Writer) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		if len(args) > 1 {
			return errors.New(usage)
		}
		return serve(ctx, getenv, clk, stderr, defaultServeDeps())
	case "import":
		if len(args) != 2 {
			return errors.New(usage)
		}
		return runImport(ctx, args[1], getenv, stdout)
	case "backup":
		preDeploy := false
		switch {
		case len(args) == 1:
		case len(args) == 2 && args[1] == "--pre-deploy":
			preDeploy = true
		default:
			return errors.New(usage)
		}
		return runBackup(ctx, preDeploy, getenv, clk, stdout, stderr)
	case "version":
		if len(args) != 1 {
			return errors.New(usage)
		}
		_, err := fmt.Fprintln(stdout, version)
		return err
	}
	return errors.New(usage)
}

func runImport(ctx context.Context, path string, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	d, n, err := importFile(ctx, st, path, clock.Real{}.Now())
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	fmt.Fprintf(stdout, "imported %d cards into deck %q\n", n, d.Name)
	return nil
}

// serveDeps are the pieces of serve that tests replace.
type serveDeps struct {
	Listen          func(network, addr string) (net.Listener, error)
	NewBackupWriter func(*store.Store, config.Config, clock.Clock, *slog.Logger) *backup.Writer
}

func defaultServeDeps() serveDeps {
	return serveDeps{Listen: net.Listen, NewBackupWriter: backupWriter}
}

func serve(ctx context.Context, getenv func(string) string, clk clock.Clock, stderr io.Writer, d serveDeps) error {
	log := slog.New(slog.NewTextHandler(stderr, nil))
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.DeleteExpiredSessions(ctx, clk.Now()); err != nil {
		return err
	}
	deps := api.Deps{
		Store:  st,
		Study:  &study.Service{Store: st, Clock: clk, Loc: cfg.Location},
		Clock:  clk,
		Config: cfg,
		Static: webui.Handler(),
		Logger: log,

		Version: version,
		LastBackup: func() (time.Time, bool, error) {
			return backup.Latest(filepath.Join(cfg.BackupDir, string(backup.Daily)))
		},
	}
	newServer := func(h http.Handler, write time.Duration) *http.Server {
		return &http.Server{
			Handler:           h,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      write,
			IdleTimeout:       120 * time.Second,
			ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		}
	}
	srvs := []*http.Server{newServer(api.New(deps), 30*time.Second)}
	ln, err := d.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	lns := []net.Listener{ln}
	log.Info("listening",
		slog.String("version", version),
		slog.String("addr", ln.Addr().String()),
		slog.String("db", cfg.DBPath),
		slog.Bool("passphrase_mode", cfg.Passphrase != ""),
		slog.Any("allowed_hosts", cfg.AllowedHosts),
		slog.String("tz", cfg.Location.String()),
	)
	if cfg.MCPEnabled() {
		// The Claude connector's public paths, on their own loopback
		// listener (MCP spec §3.1). Tool calls can be slow, hence 60 s.
		pln, err := d.Listen("tcp", cfg.MCPAddr)
		if err != nil {
			ln.Close()
			return err
		}
		srvs = append(srvs, newServer(api.NewPublic(deps), 60*time.Second))
		lns = append(lns, pln)
		log.Info("listening_public", slog.String("addr", pln.Addr().String()), slog.String("public_url", cfg.MCPPublicURL))
	}

	// The scheduler uses the store, so every exit path stops it and waits for
	// it before the deferred st.Close runs. It has its own cancel because an
	// early serve error leaves ctx alive.
	w := d.NewBackupWriter(st, cfg, clk, log)
	schedCtx, stopSched := context.WithCancel(ctx)
	var sched sync.WaitGroup
	sched.Add(1)
	go func() {
		defer sched.Done()
		w.RunScheduler(schedCtx, 15*time.Minute)
	}()
	defer func() { stopSched(); sched.Wait() }()

	errc := make(chan error, len(srvs))
	for i, srv := range srvs {
		go func() { errc <- srv.Serve(lns[i]) }()
	}
	var first error
	pending := len(srvs)
	select {
	case first = <-errc: // a server failed: stop the others too
		pending--
	case <-ctx.Done():
		log.Info("shutting down")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range srvs {
		if err := srv.Shutdown(shutdownCtx); err != nil && first == nil {
			first = err
		}
	}
	for range pending {
		if err := <-errc; !errors.Is(err, http.ErrServerClosed) && first == nil {
			first = err
		}
	}
	if errors.Is(first, http.ErrServerClosed) {
		return nil
	}
	return first
}
