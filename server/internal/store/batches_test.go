package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func nCards(n int) []NewCard {
	cs := make([]NewCard, n)
	for i := range cs {
		cs[i] = NewCard{Front: fmt.Sprintf("f%d", i), Back: fmt.Sprintf("b%d", i)}
	}
	return cs
}

func mustBatch(t *testing.T, s *Store, deckID int64, n int, at time.Time) Batch {
	t.Helper()
	b, err := s.AddBatch(context.Background(), 0, deckID, nCards(n), at, 200)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAddBatchSetsBatchID(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	b := mustBatch(t, s, d.ID, 3, t0)
	if b.ID == 0 || b.DeckID != d.ID || b.Added != 3 {
		t.Fatalf("batch = %+v", b)
	}
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM cards c JOIN review_states s ON s.card_id = c.id WHERE c.batch_id = ? AND s.state = 0 AND s.due = ?", b.ID, toMs(t0)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("new cards with batch_id = %d, want 3", n)
	}
	cs, _ := s.ListCards(ctx, d.ID)
	if len(cs) != 3 || cs[0].Front != "f0" || cs[2].Back != "b2" {
		t.Errorf("cards = %+v", cs)
	}
}

func TestAddBatchUnknownDeck(t *testing.T) {
	s := openTest(t)
	if _, err := s.AddBatch(context.Background(), 0, 99, nCards(1), t0, 200); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAddBatchDailyLimitAllOrNothing(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	for i := range 4 { // 4 × 45 + 10 = 190 cards
		mustBatch(t, s, d.ID, 45, t0.Add(time.Duration(i)*time.Hour))
	}
	mustBatch(t, s, d.ID, 10, t0.Add(5*time.Hour))
	before := count(t, s, "cards")
	_, err := s.AddBatch(ctx, 0, d.ID, nCards(20), t0.Add(6*time.Hour), 200)
	var dl *DailyLimitError
	if !errors.As(err, &dl) || !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("err = %v, want *DailyLimitError", err)
	}
	if dl.Remaining != 10 || !dl.FreesAt.Equal(t0.Add(24*time.Hour)) {
		t.Errorf("remaining = %d, frees at %v", dl.Remaining, dl.FreesAt)
	}
	if n := count(t, s, "cards"); n != before {
		t.Errorf("cards = %d, want %d (nothing written)", n, before)
	}
	if n := count(t, s, "mcp_batches"); n != 5 {
		t.Errorf("batches = %d, want 5", n)
	}
}

func TestAddBatchWindowRolls(t *testing.T) {
	s := openTest(t)
	d := mustDeck(t, s, "d")
	for range 4 {
		mustBatch(t, s, d.ID, 50, t0)
	}
	late := t0.Add(24*time.Hour + time.Millisecond)
	if _, err := s.AddBatch(context.Background(), 0, d.ID, nCards(50), late, 200); err != nil {
		t.Fatalf("after the window: %v", err)
	}
	if _, err := s.AddBatch(context.Background(), 0, d.ID, nCards(1), t0.Add(time.Hour), 200); !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("inside the window: %v", err)
	}
}

func TestDeleteBatchRemovesCardsAndRefunds(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	keep, err := s.CreateCard(ctx, d.ID, NewCard{Front: "mine", Back: "x"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	b := mustBatch(t, s, d.ID, 50, t0)
	for range 3 {
		mustBatch(t, s, d.ID, 50, t0)
	}
	var cardID int64
	_ = s.db.QueryRow("SELECT id FROM cards WHERE batch_id = ? LIMIT 1", b.ID).Scan(&cardID)
	if _, err := s.db.Exec(`INSERT INTO review_logs (review_state_id, rating, reviewed_at, duration_ms, state_before, due_before,
		stability_before, difficulty_before, elapsed_days_before, scheduled_days_before, reps_before, lapses_before,
		remaining_steps_before) VALUES (?, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)`, forward(t, s, cardID).ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBatch(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "review_logs"); n != 0 {
		t.Errorf("review logs = %d", n)
	}
	if n := count(t, s, "cards"); n != 151 {
		t.Errorf("cards = %d, want 151", n)
	}
	if _, err := s.GetCard(ctx, keep.ID); err != nil {
		t.Errorf("hand-made card deleted: %v", err)
	}
	if _, err := s.AddBatch(ctx, 0, d.ID, nCards(50), t0, 200); err != nil {
		t.Errorf("limit not refunded: %v", err)
	}
	if err := s.DeleteBatch(ctx, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete err = %v", err)
	}
}

func TestListBatchesRemaining(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "Go")
	b1 := mustBatch(t, s, d.ID, 2, t0)
	b2 := mustBatch(t, s, d.ID, 3, t0.Add(time.Minute))
	var cardID int64
	_ = s.db.QueryRow("SELECT id FROM cards WHERE batch_id = ? LIMIT 1", b2.ID).Scan(&cardID)
	if err := s.DeleteCard(ctx, cardID); err != nil {
		t.Fatal(err)
	}
	bs, err := s.ListBatches(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	want := []BatchSummary{
		{ID: b2.ID, DeckID: d.ID, DeckName: "Go", CreatedAt: t0.Add(time.Minute), CardCount: 3, Remaining: 2},
		{ID: b1.ID, DeckID: d.ID, DeckName: "Go", CreatedAt: t0, CardCount: 2, Remaining: 2},
	}
	if len(bs) != 2 || bs[0] != want[0] || bs[1] != want[1] {
		t.Errorf("batches = %+v", bs)
	}
	if bs, _ := s.ListBatches(ctx, 1); len(bs) != 1 {
		t.Errorf("limit ignored: %d", len(bs))
	}
}

func TestDeleteDeckRemovesBatches(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	mustBatch(t, s, d.ID, 2, t0)
	if err := s.DeleteDeck(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if bs, err := s.ListBatches(ctx, 20); err != nil || len(bs) != 0 {
		t.Errorf("batches = %v, %v", bs, err)
	}
}

func TestDeleteGrantKeepsBatches(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	g := mustGrant(t, s, "A", 1, 2)
	d := mustDeck(t, s, "d")
	if _, err := s.AddBatch(ctx, g, d.ID, nCards(1), t0, 200); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAllGrants(ctx); err != nil {
		t.Fatal(err)
	}
	if bs, _ := s.ListBatches(ctx, 20); len(bs) != 1 {
		t.Errorf("batches = %v", bs)
	}
}
