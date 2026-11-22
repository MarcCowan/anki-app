// Package backup holds the pure rules for database backups: file names,
// retention and the daily schedule. It does no database work.
package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"time"
)

// Kind names a backup folder under BACKUP_DIR.
type Kind string

const (
	Daily     Kind = "daily"
	PreDeploy Kind = "pre-deploy"
)

const nameLayout = "20060102T150405Z"

var namePattern = regexp.MustCompile(`^anki-app-(\d{8}T\d{6}Z)\.db$`)

// FileName returns the backup file name for t, using its UTC time.
func FileName(t time.Time) string {
	return "anki-app-" + t.UTC().Format(nameLayout) + ".db"
}

// ParseName returns the time encoded in a backup file name. Anything that is
// not exactly a backup name (including .partial files) returns false.
func ParseName(name string) (time.Time, bool) {
	m := namePattern.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(nameLayout, m[1])
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// List returns the times of the backups in dir, ascending. A missing folder
// gives nil, nil. Other files and subfolders are ignored.
func List(dir string) ([]time.Time, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	var out []time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if t, ok := ParseName(e.Name()); ok {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, nil
}

// Latest returns the newest backup time in dir, if there is one.
func Latest(dir string) (time.Time, bool, error) {
	all, err := List(dir)
	if err != nil || len(all) == 0 {
		return time.Time{}, false, err
	}
	return all[len(all)-1], true, nil
}
