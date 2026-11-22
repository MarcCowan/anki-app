package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/srs"
	"anki-app/server/internal/store"
)

var t0 = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "seed.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const twoCards = `{"deck":" Cpp ","cards":[{"front":"Q1","back":"A1"},{"front":"\n\n    int x;\r\n","back":"A2"}]}`

func TestImportOK(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	d, n, err := importFile(ctx, st, writeFile(t, twoCards), t0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Cpp" || n != 2 {
		t.Fatalf("deck %+v, n %d", d, n)
	}
	cards, err := st.ListCards(ctx, d.ID)
	if err != nil || len(cards) != 2 {
		t.Fatalf("cards %v, %v", cards, err)
	}
	for _, c := range cards {
		if c.Sched.State != srs.New {
			t.Fatalf("card %d state %v", c.ID, c.Sched.State)
		}
	}
	if cards[1].Front != "    int x;" {
		t.Fatalf("front not normalised: %q", cards[1].Front)
	}
}

func TestImportDuplicate(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	p := writeFile(t, twoCards)
	if _, _, err := importFile(ctx, st, p, t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := importFile(ctx, st, p, t0); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
}

func TestImportInvalidCard(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	p := writeFile(t, `{"deck":"Cpp","cards":[{"front":"Q1","back":"A1"},{"front":"Q2","back":""}]}`)
	_, _, err := importFile(ctx, st, p, t0)
	if err == nil || !strings.Contains(err.Error(), "card 2") {
		t.Fatalf("err = %v", err)
	}
	decks, err := st.ListDecks(ctx)
	if err != nil || len(decks) != 0 {
		t.Fatalf("decks = %v, %v", decks, err)
	}
	long := `{"deck":"Cpp","cards":[{"front":"` + strings.Repeat("x", 6001) + `","back":"A"}]}`
	if _, _, err := importFile(ctx, st, writeFile(t, long), t0); err == nil || !strings.Contains(err.Error(), "card 1") {
		t.Fatalf("too long: err = %v", err)
	}
}

func TestImportUnknownField(t *testing.T) {
	st := openStore(t)
	for _, body := range []string{
		`{"deck":"x","cards":[],"extra":1}`,
		`{"deck":"x","cards":[{"front":"a","back":"b","tags":[]}]}`,
		`{"deck":"x"}`,
		`{"deck":"","cards":[]}`,
		`not json`,
	} {
		if _, _, err := importFile(context.Background(), st, writeFile(t, body), t0); err == nil {
			t.Errorf("%s: want error", body)
		}
	}
}

func TestRunImportCommand(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cmd.db")
	env := func(k string) string {
		if k == "DB_PATH" {
			return db
		}
		return ""
	}
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"import", writeFile(t, twoCards)}, env, clock.Real{}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, `imported 2 cards into deck "Cpp"`) {
		t.Fatalf("stdout = %q", got)
	}
	if err := run(context.Background(), []string{"import"}, env, clock.Real{}, &out, &errOut); err == nil {
		t.Fatal("import without file: want error")
	}
	if err := run(context.Background(), []string{"bogus"}, env, clock.Real{}, &out, &errOut); err == nil {
		t.Fatal("unknown command: want error")
	}
}
