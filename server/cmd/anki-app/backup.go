package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"

	"anki-app/server/internal/backup"
	"anki-app/server/internal/clock"
	"anki-app/server/internal/config"
	"anki-app/server/internal/store"
)

func backupWriter(st *store.Store, cfg config.Config, clk clock.Clock, log *slog.Logger) *backup.Writer {
	return &backup.Writer{Src: st, Root: cfg.BackupDir, Clock: clk, Loc: cfg.Location, Log: log}
}

// runBackup writes one backup and prints only its absolute path on stdout;
// logs go to stderr.
func runBackup(ctx context.Context, preDeploy bool, getenv func(string) string, clk clock.Clock, stdout, stderr io.Writer) error {
	log := slog.New(slog.NewTextHandler(stderr, nil))
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	// A wrong DB_PATH must not silently create and back up an empty database.
	if _, err := os.Stat(cfg.DBPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("database %s does not exist", cfg.DBPath)
		}
		return fmt.Errorf("backup: %w", err)
	}
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	kind := backup.Daily
	if preDeploy {
		kind = backup.PreDeploy
	}
	path, err := backupWriter(st, cfg, clk, log).Write(ctx, kind)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	_, err = fmt.Fprintln(stdout, path)
	return err
}
