package store

import (
	"context"
	"testing"
	"time"

	"anki-app/server/internal/srs"
)

// checkInvariant fails if any card lacks a forward state, or has a reverse
// state that disagrees with cards.two_sided (two-sided spec §2).
func checkInvariant(t *testing.T, s *Store) {
	t.Helper()
	var bad int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM cards c WHERE
		(SELECT COUNT(*) FROM review_states s WHERE s.card_id = c.id AND s.reverse = 0) <> 1 OR
		(SELECT COUNT(*) FROM review_states s WHERE s.card_id = c.id AND s.reverse = 1) <> c.two_sided OR
		EXISTS (SELECT 1 FROM review_states s WHERE s.card_id = c.id AND s.deck_id <> c.deck_id)`).Scan(&bad)
	if err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Fatalf("%d cards break the two-sided invariant", bad)
	}
}

func hasReverse(t *testing.T, s *Store, cardID int64) bool {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM review_states WHERE card_id = ? AND reverse = 1", cardID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func logsFor(t *testing.T, s *Store, stateID int64) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM review_logs WHERE review_state_id = ?", stateID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateCardFollowsDeckDefault(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	one := mustDeck(t, s, "one")
	two, err := s.CreateDeck(ctx, "two", true, t0)
	if err != nil || !two.TwoSided {
		t.Fatalf("CreateDeck two-sided = %+v, %v", two, err)
	}
	for _, c := range []struct {
		deck     int64
		override *bool
		want     bool
	}{
		{one.ID, nil, false},
		{two.ID, nil, true},
		{one.ID, ptr(true), true},
		{two.ID, ptr(false), false},
	} {
		card, err := s.CreateCard(ctx, c.deck, NewCard{Front: "Q", Back: "A", TwoSided: c.override}, t0)
		if err != nil {
			t.Fatal(err)
		}
		if card.TwoSided != c.want || hasReverse(t, s, card.ID) != c.want {
			t.Errorf("deck %d override %v: TwoSided = %v, reverse = %v; want %v",
				c.deck, c.override, card.TwoSided, hasReverse(t, s, card.ID), c.want)
		}
		if c.want {
			if r := stateOf(t, s, card.ID, true); r.Sched != srs.NewCardState(t0) || r.DeckID != c.deck {
				t.Errorf("reverse state = %+v", r)
			}
		}
	}
	checkInvariant(t, s)
}

func TestUpdateCardTwoSidedOffDeletesReverseHistoryOnly(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	card, err := s.CreateCard(ctx, d.ID, NewCard{Front: "Q", Back: "A", TwoSided: ptr(true)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	fwd, rev := forward(t, s, card.ID), stateOf(t, s, card.ID, true)
	review(t, s, fwd, srs.Good, t0.Add(time.Minute))
	review(t, s, rev, srs.Good, t0.Add(time.Minute))

	// Turning on again keeps the existing reverse (and its history).
	if _, err := s.UpdateCard(ctx, card.ID, CardPatch{TwoSided: ptr(true)}, t0); err != nil {
		t.Fatal(err)
	}
	if logsFor(t, s, rev.ID) != 1 {
		t.Fatal("turning two-sided on again touched the existing reverse")
	}

	u, err := s.UpdateCard(ctx, card.ID, CardPatch{TwoSided: ptr(false)}, t0.Add(time.Hour))
	if err != nil || u.TwoSided {
		t.Fatalf("UpdateCard off = %+v, %v", u, err)
	}
	if hasReverse(t, s, card.ID) || logsFor(t, s, rev.ID) != 0 {
		t.Error("reverse state or its log survived turning two-sided off")
	}
	if logsFor(t, s, fwd.ID) != 1 {
		t.Error("forward history was touched")
	}
	checkInvariant(t, s)

	// Back on: a fresh New reverse.
	t2 := t0.Add(2 * time.Hour)
	if _, err := s.UpdateCard(ctx, card.ID, CardPatch{TwoSided: ptr(true)}, t2); err != nil {
		t.Fatal(err)
	}
	if r := stateOf(t, s, card.ID, true); r.Sched != srs.NewCardState(t2) {
		t.Errorf("re-created reverse = %+v, want new", r.Sched)
	}
	checkInvariant(t, s)
}

func TestDeckTwoSidedToggle(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	other := mustDeck(t, s, "other")
	a := mustCard(t, s, d.ID, "a", t0)
	b, err := s.CreateCard(ctx, d.ID, NewCard{Front: "b", Back: "B", TwoSided: ptr(true)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	bRev := review(t, s, stateOf(t, s, b.ID, true), srs.Good, t0.Add(time.Minute))
	o := mustCard(t, s, other.ID, "o", t0)

	t1 := t0.Add(time.Hour)
	got, err := s.UpdateDeck(ctx, d.ID, DeckPatch{TwoSided: ptr(true)}, t1)
	if err != nil || !got.TwoSided {
		t.Fatalf("UpdateDeck on = %+v, %v", got, err)
	}
	if !hasReverse(t, s, a.ID) || stateOf(t, s, a.ID, true).Sched != srs.NewCardState(t1) {
		t.Error("card a did not get a new reverse")
	}
	if stateOf(t, s, b.ID, true).Sched != bRev.Sched {
		t.Error("card b's existing reverse was replaced")
	}
	if hasReverse(t, s, o.ID) {
		t.Error("a card in another deck got a reverse")
	}
	checkInvariant(t, s)

	// Off without removing: only the default changes.
	if got, err = s.UpdateDeck(ctx, d.ID, DeckPatch{TwoSided: ptr(false)}, t1); err != nil || got.TwoSided {
		t.Fatalf("UpdateDeck off = %+v, %v", got, err)
	}
	if !hasReverse(t, s, a.ID) || !hasReverse(t, s, b.ID) {
		t.Error("turning the default off removed reverses")
	}
	checkInvariant(t, s)

	if _, err := s.UpdateDeck(ctx, d.ID, DeckPatch{RemoveReverses: true}, t1); err == nil {
		t.Error("RemoveReverses without TwoSided false was accepted")
	}
	if _, err := s.UpdateDeck(ctx, d.ID, DeckPatch{TwoSided: ptr(false), RemoveReverses: true}, t1); err != nil {
		t.Fatal(err)
	}
	if hasReverse(t, s, a.ID) || hasReverse(t, s, b.ID) || logsFor(t, s, bRev.ID) != 0 {
		t.Error("RemoveReverses left a reverse or its history")
	}
	checkInvariant(t, s)
}

func TestBuryingSiblings(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	card, err := s.CreateCard(ctx, d.ID, NewCard{Front: "Q", Back: "A", TwoSided: ptr(true)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	dayStart := t0.Add(-time.Hour)
	if n, err := s.CountState(ctx, d.ID, srs.New, dayStart); err != nil || n != 2 {
		t.Fatalf("both directions new: %d, %v", n, err)
	}
	fwd := forward(t, s, card.ID)
	review(t, s, fwd, srs.Good, t0.Add(time.Minute))

	// The reverse is buried for the rest of today...
	if _, ok, err := s.FirstNew(ctx, d.ID, dayStart); err != nil || ok {
		t.Fatalf("FirstNew today = %v, %v; want buried", ok, err)
	}
	if n, err := s.CountState(ctx, d.ID, srs.New, dayStart); err != nil || n != 0 {
		t.Fatalf("CountState today = %d, %v", n, err)
	}
	// ...but the forward direction itself is not buried by its own review.
	later := t0.Add(time.Hour)
	if r, ok, err := s.FirstDue(ctx, d.ID, []srs.State{srs.Learning}, later, dayStart); err != nil || !ok || r.ID != fwd.ID {
		t.Fatalf("FirstDue forward = %+v, %v, %v", r, ok, err)
	}
	if n, err := s.CountDue(ctx, d.ID, []srs.State{srs.Learning}, later, dayStart); err != nil || n != 1 {
		t.Fatalf("CountDue = %d, %v", n, err)
	}
	// After the next rollover it comes back.
	tomorrow := dayStart.Add(24 * time.Hour)
	r, ok, err := s.FirstNew(ctx, d.ID, tomorrow)
	if err != nil || !ok || !r.Reverse || r.CardID != card.ID {
		t.Fatalf("FirstNew tomorrow = %+v, %v, %v", r, ok, err)
	}
}

func TestDeleteCardRemovesBothDirections(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	card, err := s.CreateCard(ctx, d.ID, NewCard{Front: "Q", Back: "A", TwoSided: ptr(true)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	review(t, s, stateOf(t, s, card.ID, true), srs.Good, t0.Add(time.Minute))
	if err := s.DeleteCard(ctx, card.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "review_states"); n != 0 {
		t.Errorf("review_states = %d", n)
	}
	if n := count(t, s, "review_logs"); n != 0 {
		t.Errorf("review_logs = %d", n)
	}
}

// MCP add_cards and import create cards with TwoSided nil: they follow the deck.
func TestAddBatchFollowsDeckDefault(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, err := s.CreateDeck(ctx, "two", true, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBatch(ctx, 0, d.ID, []NewCard{{Front: "Q", Back: "A"}}, t0, 50); err != nil {
		t.Fatal(err)
	}
	cards, err := s.ListCards(ctx, d.ID)
	if err != nil || len(cards) != 1 || !cards[0].TwoSided || !hasReverse(t, s, cards[0].ID) {
		t.Fatalf("batch cards = %+v, %v", cards, err)
	}
	checkInvariant(t, s)
}
