package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/store"
)

const (
	partialSuffix = ".partial"
	preDeployKeep = 10
)

// Snapshotter writes a consistent copy of the live database to path.
// *store.Store satisfies it.
type Snapshotter interface {
	VacuumInto(ctx context.Context, path string) error
}

// Writer takes backups under Root/daily and Root/pre-deploy.
type Writer struct {
	Src   Snapshotter
	Root  string
	Clock clock.Clock
	Loc   *time.Location // local zone for retention and the daily schedule
	Log   *slog.Logger
}

func (w *Writer) dir(kind Kind) string { return filepath.Join(w.Root, string(kind)) }

// Write takes one backup of kind and returns its absolute path. Afterwards it
// prunes that folder; prune failures are logged, not returned.
func (w *Writer) Write(ctx context.Context, kind Kind) (string, error) {
	start := time.Now() // duration only, not logic
	dir := w.dir(kind)
	for _, k := range []Kind{Daily, PreDeploy} {
		if err := os.MkdirAll(w.dir(k), 0o700); err != nil {
			return "", fmt.Errorf("create backup folder: %w", err)
		}
	}
	if err := removePartials(dir); err != nil {
		return "", err
	}

	now := w.Clock.Now()
	final, err := filepath.Abs(filepath.Join(dir, FileName(now)))
	if err != nil {
		return "", fmt.Errorf("backup path: %w", err)
	}
	partial := final + partialSuffix
	if err := w.writePartial(ctx, partial); err != nil {
		_ = os.Remove(partial)
		return "", err
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("rename backup: %w", err)
	}
	syncDir(dir)

	var size int64
	if fi, err := os.Stat(final); err == nil {
		size = fi.Size()
	}
	w.Log.Info("backup written", "name", filepath.Base(final), "size", size, "duration", time.Since(start))

	if err := w.prune(kind, now); err != nil {
		w.Log.Error("backup prune failed", "kind", string(kind), "err", err)
	}
	return final, nil
}

// writePartial snapshots to path, verifies it and fsyncs it.
func (w *Writer) writePartial(ctx context.Context, path string) error {
	if err := w.Src.VacuumInto(ctx, path); err != nil {
		return err
	}
	if err := store.QuickCheck(ctx, path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open backup for sync: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync backup: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close backup: %w", err)
	}
	return nil
}

// removePartials deletes leftover backup .partial files in dir by exact name.
func removePartials(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read backup folder: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, partialSuffix) {
			continue
		}
		if _, ok := ParseName(strings.TrimSuffix(name, partialSuffix)); !ok {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove leftover partial: %w", err)
		}
	}
	return nil
}

// syncDir fsyncs a folder so the rename is durable. Best effort: Windows
// cannot fsync directories.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// prune removes the backups retention drops from kind's folder, one file at a time.
func (w *Writer) prune(kind Kind, now time.Time) error {
	dir := w.dir(kind)
	all, err := List(dir)
	if err != nil {
		return err
	}
	var drop []time.Time
	if kind == Daily {
		_, drop = Keep(all, now, w.Loc)
	} else {
		_, drop = KeepNewest(all, preDeployKeep)
	}
	var errs []error
	for _, t := range drop {
		if err := os.Remove(filepath.Join(dir, FileName(t))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
