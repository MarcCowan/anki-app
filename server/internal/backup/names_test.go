package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNameRoundTrip(t *testing.T) {
	ts := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)
	name := FileName(ts)
	if name != "anki-app-20261105T030000Z.db" {
		t.Fatalf("FileName = %q", name)
	}
	got, ok := ParseName(name)
	if !ok || !got.Equal(ts) {
		t.Fatalf("ParseName(%q) = %v, %v", name, got, ok)
	}
}

func TestFileNameUsesUTC(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	ts := time.Date(2026, 7, 1, 0, 30, 0, 0, loc) // 23:30 UTC the day before
	if got := FileName(ts); got != "anki-app-20260630T233000Z.db" {
		t.Fatalf("FileName = %q", got)
	}
}

func TestParseNameRejects(t *testing.T) {
	for _, name := range []string{
		"anki-app-20261105T030000Z.db.partial",
		"anki-app.db",
		"anki-app-2026115T030000Z.db",
		"ANKI-APP-20261105T030000Z.db",
		"anki-app-20261305T030000Z.db",
		"x anki-app-20261105T030000Z.db",
		"anki-app-20261105T030000Z.db ",
		"",
	} {
		if _, ok := ParseName(name); ok {
			t.Errorf("ParseName(%q) accepted", name)
		}
	}
}

func TestListIgnoresOthersAndIsAscending(t *testing.T) {
	dir := t.TempDir()
	a := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)
	b := time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC)
	c := time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)
	for _, n := range []string{FileName(a), FileName(b), FileName(c),
		FileName(a) + ".partial", "anki-app.db", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A subfolder with a backup-shaped name must be ignored.
	sub := FileName(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := os.Mkdir(filepath.Join(dir, sub), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Time{b, c, a}
	if len(got) != len(want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("List[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	latest, ok, err := Latest(dir)
	if err != nil || !ok || !latest.Equal(a) {
		t.Fatalf("Latest = %v, %v, %v", latest, ok, err)
	}
}

func TestListMissingFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	got, err := List(dir)
	if err != nil || got != nil {
		t.Fatalf("List = %v, %v", got, err)
	}
	_, ok, err := Latest(dir)
	if err != nil || ok {
		t.Fatalf("Latest ok=%v err=%v", ok, err)
	}
}
