package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"anki-app/server/internal/srs"
)

func mustDeck(t *testing.T, s *Store, name string) Deck {
	t.Helper()
	d, err := s.CreateDeck(context.Background(), name, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustCard(t *testing.T, s *Store, deckID int64, front string, now time.Time) Card {
	t.Helper()
	c, err := s.CreateCard(context.Background(), deckID, NewCard{Front: front, Back: "A"}, now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// setSched overwrites a card's forward state and due time directly.
func setSched(t *testing.T, s *Store, cardID int64, st srs.State, due time.Time) {
	t.Helper()
	if _, err := s.db.Exec("UPDATE review_states SET state = ?, due = ? WHERE card_id = ? AND reverse = 0",
		int(st), toMs(due), cardID); err != nil {
		t.Fatal(err)
	}
}

// stateOf returns one direction of a card.
func stateOf(t *testing.T, s *Store, cardID int64, reverse bool) ReviewState {
	t.Helper()
	r, err := scanState(s.db.QueryRow("SELECT "+stateCols+" FROM review_states s WHERE s.card_id = ? AND s.reverse = ?", cardID, reverse))
	if err != nil {
		t.Fatalf("state of card %d (reverse %v): %v", cardID, reverse, err)
	}
	return r
}

func forward(t *testing.T, s *Store, cardID int64) ReviewState { return stateOf(t, s, cardID, false) }

func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCardCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	c, err := s.CreateCard(ctx, d.ID, NewCard{Front: "Q", Back: "A"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID <= 0 || c.DeckID != d.ID || c.Front != "Q" || c.Back != "A" || !c.CreatedAt.Equal(t0) || !c.UpdatedAt.Equal(t0) {
		t.Fatalf("created %+v", c)
	}
	if c.Sched != srs.NewCardState(t0) {
		t.Fatalf("Sched = %+v, want new card state", c.Sched)
	}
	g, err := s.GetCard(ctx, c.ID)
	if err != nil || g != c {
		t.Fatalf("GetCard = %+v, %v", g, err)
	}
	t1 := t0.Add(time.Hour)
	u, err := s.UpdateCard(ctx, c.ID, CardPatch{Front: ptr("Q2")}, t1)
	if err != nil || u.Front != "Q2" || u.Back != "A" || !u.UpdatedAt.Equal(t1) || u.Sched != c.Sched {
		t.Fatalf("UpdateCard = %+v, %v", u, err)
	}
	list, err := s.ListCards(ctx, d.ID)
	if err != nil || len(list) != 1 || list[0] != u {
		t.Fatalf("ListCards = %+v, %v", list, err)
	}
	if err := s.DeleteCard(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCard(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCard after delete: %v", err)
	}
	if err := s.DeleteCard(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteCard twice: %v", err)
	}
	if _, err := s.UpdateCard(ctx, c.ID, CardPatch{Front: ptr("x")}, t1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateCard missing: %v", err)
	}
}

func TestCreateCardMissingDeck(t *testing.T) {
	s := openTest(t)
	if _, err := s.CreateCard(context.Background(), 999, NewCard{Front: "Q", Back: "A"}, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.ListCards(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListCards missing deck: %v", err)
	}
}

func TestDeleteDeckCascades(t *testing.T) {
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	c := mustCard(t, s, d.ID, "Q1", t0)
	mustCard(t, s, d.ID, "Q2", t0)
	_, err := s.db.Exec(`INSERT INTO review_logs (review_state_id, rating, reviewed_at, duration_ms, state_before, due_before,
		stability_before, difficulty_before, elapsed_days_before, scheduled_days_before, reps_before, lapses_before,
		remaining_steps_before) VALUES (?, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)`, forward(t, s, c.ID).ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDeck(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "cards"); n != 0 {
		t.Errorf("cards = %d", n)
	}
	if n := count(t, s, "review_logs"); n != 0 {
		t.Errorf("review_logs = %d", n)
	}
	if n := count(t, s, "review_states"); n != 0 {
		t.Errorf("review_states = %d", n)
	}
}

func TestNewCardOrder(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, err := s.ImportDeck(ctx, "Cpp", []NewCard{{Front: "Q1", Back: "A"}, {Front: "Q2", Back: "A"}, {Front: "Q3", Back: "A"}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := s.ListCards(ctx, d.ID)
	if err != nil || len(cards) != 3 {
		t.Fatalf("ListCards = %d, %v", len(cards), err)
	}
	c, ok, err := s.FirstNew(ctx, d.ID, t0)
	if err != nil || !ok || c.CardID != cards[0].ID || c.Reverse {
		t.Fatalf("FirstNew = %+v, %v, %v", c, ok, err)
	}
	n, err := s.CountState(ctx, d.ID, srs.New, t0)
	if err != nil || n != 3 {
		t.Fatalf("CountState = %d, %v", n, err)
	}
}

func TestFirstDueOrderAndFilter(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	other := mustDeck(t, s, "Other")
	a := mustCard(t, s, d.ID, "learn -5m", t0)
	b := mustCard(t, s, d.ID, "relearn -10m", t0)
	r := mustCard(t, s, d.ID, "review -1d", t0)
	f := mustCard(t, s, d.ID, "learn +5m", t0)
	o := mustCard(t, s, other.ID, "other deck -1h", t0)
	setSched(t, s, a.ID, srs.Learning, t0.Add(-5*time.Minute))
	setSched(t, s, b.ID, srs.Relearning, t0.Add(-10*time.Minute))
	setSched(t, s, r.ID, srs.Review, t0.Add(-24*time.Hour))
	setSched(t, s, f.ID, srs.Learning, t0.Add(5*time.Minute))
	setSched(t, s, o.ID, srs.Learning, t0.Add(-time.Hour))

	learning := []srs.State{srs.Learning, srs.Relearning}
	c, ok, err := s.FirstDue(ctx, d.ID, learning, t0, t0)
	if err != nil || !ok || c.CardID != b.ID {
		t.Fatalf("FirstDue = card %d, %v, %v; want %d", c.CardID, ok, err, b.ID)
	}
	if n, err := s.CountDue(ctx, d.ID, learning, t0, t0); err != nil || n != 2 {
		t.Fatalf("CountDue learning = %d, %v", n, err)
	}
	c, ok, err = s.FirstDue(ctx, d.ID, []srs.State{srs.Review}, t0, t0)
	if err != nil || !ok || c.CardID != r.ID {
		t.Fatalf("FirstDue review = card %d, %v, %v", c.CardID, ok, err)
	}
	if _, ok, err := s.FirstDue(ctx, d.ID, []srs.State{srs.Review}, t0.Add(-48*time.Hour), t0); err != nil || ok {
		t.Fatalf("FirstDue none = %v, %v", ok, err)
	}
}

func TestImportDeckAtomic(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, err := s.ImportDeck(ctx, "Cpp", []NewCard{{Front: "Q1", Back: "A1"}, {Front: "Q2", Back: "A2"}}, t0)
	if err != nil || d.Name != "Cpp" {
		t.Fatalf("ImportDeck = %+v, %v", d, err)
	}
	if _, err := s.ImportDeck(ctx, "Cpp", []NewCard{{Front: "Q3", Back: "A3"}}, t0); !errors.Is(err, ErrConflict) {
		t.Fatalf("second import: %v", err)
	}
	if n := count(t, s, "cards"); n != 2 {
		t.Fatalf("cards = %d", n)
	}
}

func TestMinDue(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	learning := []srs.State{srs.Learning, srs.Relearning}
	if _, ok, err := s.MinDue(ctx, d.ID, learning, t0); err != nil || ok {
		t.Fatalf("MinDue empty = %v, %v", ok, err)
	}
	a := mustCard(t, s, d.ID, "a", t0)
	b := mustCard(t, s, d.ID, "b", t0)
	mustCard(t, s, d.ID, "new", t0) // New, due t0: must be ignored
	setSched(t, s, a.ID, srs.Learning, t0.Add(30*time.Minute))
	setSched(t, s, b.ID, srs.Relearning, t0.Add(20*time.Minute))
	got, ok, err := s.MinDue(ctx, d.ID, learning, t0)
	if err != nil || !ok || !got.Equal(t0.Add(20*time.Minute)) {
		t.Fatalf("MinDue = %v, %v, %v", got, ok, err)
	}
}
