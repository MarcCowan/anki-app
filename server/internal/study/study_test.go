package study

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/srs"
	"anki-app/server/internal/store"
)

// t0 is 10:00 UTC; with the default rollover hour 4 the study day began at 04:00.
var t0 = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

type fixture struct {
	svc  *Service
	st   *store.Store
	clk  *clock.Fake
	deck store.Deck
	ctx  context.Context
	t    *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := &clock.Fake{T: t0}
	d, err := st.CreateDeck(ctx, "Cpp", false, t0.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{svc: &Service{Store: st, Clock: clk, Loc: time.UTC}, st: st, clk: clk, deck: d, ctx: ctx, t: t}
}

// card creates a New one-sided card created at `at` (before t0).
func (f *fixture) card(front string, at time.Time) store.Card {
	return f.cardSides(front, at, false)
}

func (f *fixture) cardSides(front string, at time.Time, twoSided bool) store.Card {
	f.t.Helper()
	c, err := f.st.CreateCard(f.ctx, f.deck.ID, store.NewCard{Front: front, Back: "A", TwoSided: &twoSided}, at)
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

// fwd returns a card's forward review state.
func (f *fixture) fwd(c store.Card) store.ReviewState {
	f.t.Helper()
	sts, err := f.st.CardStates(f.ctx, c.ID)
	if err != nil || len(sts) == 0 {
		f.t.Fatalf("states of card %d: %v, %v", c.ID, sts, err)
	}
	return sts[0]
}

// force sets a state's schedule through a review log entry at `at`.
func (f *fixture) force(r store.ReviewState, after srs.CardState, at time.Time) store.ReviewState {
	f.t.Helper()
	got, err := f.st.ApplyReview(f.ctx, store.ReviewInput{
		StateID: r.ID, ExpectedReps: r.Sched.Reps, Rating: srs.Good,
		Before: r.Sched, After: after, ReviewedAt: at,
	}, at)
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

func (f *fixture) next() NextResult {
	f.t.Helper()
	r, err := f.svc.Next(f.ctx, f.deck.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func (f *fixture) answer(r store.ReviewState, rating srs.Rating) store.ReviewState {
	f.t.Helper()
	got, err := f.svc.Answer(f.ctx, r.ID, rating, r.Sched.Reps, 1000)
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

func (f *fixture) counts() Counts {
	f.t.Helper()
	c, err := f.svc.Counts(f.ctx, f.deck.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f *fixture) setNewPerDay(n int) {
	f.t.Helper()
	if _, err := f.st.UpdateDeck(f.ctx, f.deck.ID, store.DeckPatch{NewPerDay: &n}, t0); err != nil {
		f.t.Fatal(err)
	}
}

func TestQueueOrder(t *testing.T) {
	f := newFixture(t)
	yesterday := t0.Add(-30 * time.Hour)
	learn := f.card("learning", yesterday)
	rev := f.card("review", yesterday)
	f.card("new 1", yesterday)
	f.card("new 2", yesterday)
	f.force(f.fwd(learn), srs.CardState{State: srs.Learning, Due: t0.Add(-5 * time.Minute), Reps: 1, Stability: 1, Difficulty: 5, RemainingSteps: 1, LastReview: yesterday}, yesterday)
	f.force(f.fwd(rev), srs.CardState{State: srs.Review, Due: t0.Add(-time.Hour), Reps: 1, Stability: 5, Difficulty: 5, ScheduledDays: 1, LastReview: yesterday}, yesterday)

	if c := f.counts(); c != (Counts{New: 2, Learning: 1, Review: 1}) {
		t.Fatalf("counts = %+v", c)
	}
	var order []string
	for range 4 {
		r := f.next()
		if r.Done {
			t.Fatalf("done early after %v", order)
		}
		order = append(order, r.Card.Front)
		f.answer(r.State, srs.Good)
	}
	want := []string{"learning", "review", "new 1", "new 2"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// new_per_day is a target, not a cap: nothing stops more new cards (two-sided spec §3).
func TestNewPerDayIsATarget(t *testing.T) {
	f := newFixture(t)
	f.setNewPerDay(2)
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		f.card(n, t0.Add(-time.Hour))
	}
	if c := f.counts(); c.New != 5 || c.NewToday != 0 {
		t.Fatalf("counts = %+v", c)
	}
	for range 3 {
		r := f.next()
		if r.Done || r.State.Sched.State != srs.New {
			t.Fatalf("expected a new card, got %+v", r)
		}
		f.answer(r.State, srs.Easy)
	}
	if c := f.counts(); c.New != 2 || c.NewToday != 3 {
		t.Fatalf("counts past the target = %+v", c)
	}
	if r := f.next(); r.Done {
		t.Fatal("done after passing the target; want more new cards")
	}
}

func TestRolloverResetsNewToday(t *testing.T) {
	f := newFixture(t)
	for _, n := range []string{"a", "b", "c"} {
		f.card(n, t0.Add(-time.Hour))
	}
	for range 2 {
		f.answer(f.next().State, srs.Easy)
	}
	f.clk.T = time.Date(2026, 11, 3, 3, 59, 0, 0, time.UTC)
	if c := f.counts(); c.NewToday != 2 {
		t.Fatalf("03:59 next day: counts = %+v", c)
	}
	f.clk.T = time.Date(2026, 11, 3, 4, 0, 0, 0, time.UTC)
	if c := f.counts(); c.NewToday != 0 {
		t.Fatalf("04:00 next day: counts = %+v", c)
	}
}

func TestTwoSidedCardStudiesBothDirectionsOnSeparateDays(t *testing.T) {
	f := newFixture(t)
	c := f.cardSides("q", t0.Add(-time.Hour), true)
	if n := f.counts(); n.New != 2 {
		t.Fatalf("counts = %+v, want both directions new", n)
	}
	r := f.next()
	if r.Done || r.Card.ID != c.ID || r.State.Reverse || r.Card.Front != "q" {
		t.Fatalf("first = %+v", r)
	}
	f.answer(r.State, srs.Easy) // forward graduates to Review, days away

	// The reverse is buried for the rest of the day.
	if n := f.counts(); n.New != 0 || n.NewToday != 1 {
		t.Fatalf("counts after forward = %+v", n)
	}
	if r := f.next(); !r.Done {
		t.Fatalf("reverse shown on the same day: %+v", r.State)
	}

	// Next study day: the reverse, with the card's content, as its own schedule.
	f.clk.T = time.Date(2026, 11, 3, 4, 0, 0, 0, time.UTC)
	r = f.next()
	if r.Done || !r.State.Reverse || r.Card.ID != c.ID || r.State.Sched.State != srs.New {
		t.Fatalf("next day = %+v", r)
	}
	f.answer(r.State, srs.Good)
	if n := f.counts(); n.NewToday != 1 {
		t.Fatalf("counts after reverse = %+v", n)
	}
}

func TestLearnAhead(t *testing.T) {
	for _, tc := range []struct {
		in       time.Duration
		wantDone bool
	}{{15 * time.Minute, false}, {25 * time.Minute, true}} {
		f := newFixture(t)
		c := f.card("learning", t0.Add(-time.Hour))
		due := t0.Add(tc.in)
		f.force(f.fwd(c), srs.CardState{State: srs.Learning, Due: due, Reps: 1, Stability: 1, Difficulty: 5, RemainingSteps: 1, LastReview: t0}, t0)
		r := f.next()
		if r.Done != tc.wantDone {
			t.Fatalf("due in %v: done = %v", tc.in, r.Done)
		}
		if tc.wantDone {
			if r.NextDueAt == nil || !r.NextDueAt.Equal(due) {
				t.Fatalf("NextDueAt = %v, want %v", r.NextDueAt, due)
			}
		} else if r.Card.ID != c.ID {
			t.Fatalf("got card %d, want %d", r.Card.ID, c.ID)
		}
	}
}

func TestPreviewsPresent(t *testing.T) {
	f := newFixture(t)
	f.card("q", t0.Add(-time.Hour))
	r := f.next()
	if r.Done || len(r.Previews) != 4 {
		t.Fatalf("next = %+v", r)
	}
	for _, rt := range []srs.Rating{srs.Again, srs.Hard, srs.Good, srs.Easy} {
		if p, ok := r.Previews[rt]; !ok || p.IntervalLabel == "" {
			t.Fatalf("preview %v = %+v", rt, p)
		}
	}
	if r.Previews[srs.Again].Due.After(r.Previews[srs.Easy].Due) {
		t.Fatal("Again due after Easy due")
	}
	if r.Remaining != (Counts{New: 1}) {
		t.Fatalf("remaining = %+v", r.Remaining)
	}
}

func TestPreviewLabelsWithSubMillisecondClock(t *testing.T) {
	f := newFixture(t)
	f.card("q", t0.Add(-time.Hour))
	f.clk.T = t0.Add(123456 * time.Nanosecond) // real clocks are not millisecond-aligned
	r := f.next()
	want := map[srs.Rating]string{srs.Again: "1m", srs.Hard: "6m", srs.Good: "10m", srs.Easy: "8d"}
	for rt, label := range want {
		if got := r.Previews[rt].IntervalLabel; got != label {
			t.Errorf("rating %v label = %q, want %q", rt, got, label)
		}
	}
}

func TestAnswerStale(t *testing.T) {
	f := newFixture(t)
	c := f.card("q", t0.Add(-time.Hour))
	if _, err := f.svc.Answer(f.ctx, f.fwd(c).ID, srs.Good, 3, 0); !errors.Is(err, store.ErrStale) {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.svc.Answer(f.ctx, 999, srs.Good, 0, 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing state: err = %v", err)
	}
}

func TestClampDuration(t *testing.T) {
	for in, want := range map[int64]int64{-5: 0, 0: 0, 1234: 1234, 600000: 600000, 600001: 600000} {
		if got := clampDuration(in); got != want {
			t.Errorf("clampDuration(%d) = %d, want %d", in, got, want)
		}
	}
	// A negative duration would violate the duration_ms >= 0 CHECK unless Answer clamps it.
	f := newFixture(t)
	c := f.card("q", t0.Add(-time.Hour))
	if _, err := f.svc.Answer(f.ctx, f.fwd(c).ID, srs.Good, 0, -5); err != nil {
		t.Fatalf("Answer with negative duration: %v", err)
	}
}

func TestUndoThenRequeue(t *testing.T) {
	f := newFixture(t)
	c := f.card("q", t0.Add(-time.Hour))
	f.card("q2", t0.Add(-time.Hour))
	before := f.counts()
	after := f.answer(f.next().State, srs.Good)
	if f.counts() == before {
		t.Fatal("counts did not change after answering")
	}
	if _, err := f.svc.Undo(f.ctx, after.ID, after.Sched.Reps); err != nil {
		t.Fatal(err)
	}
	if got := f.counts(); got != before {
		t.Fatalf("counts after undo = %+v, want %+v", got, before)
	}
	if r := f.next(); r.Done || r.Card.ID != c.ID {
		t.Fatalf("next after undo = %+v", r)
	}
}

// Undoing a two-sided card's answer un-buries its sibling.
func TestUndoUnburiesSibling(t *testing.T) {
	f := newFixture(t)
	f.cardSides("q", t0.Add(-time.Hour), true)
	after := f.answer(f.next().State, srs.Easy)
	if n := f.counts(); n.New != 0 {
		t.Fatalf("counts = %+v", n)
	}
	if _, err := f.svc.Undo(f.ctx, after.ID, after.Sched.Reps); err != nil {
		t.Fatal(err)
	}
	if n := f.counts(); n.New != 2 {
		t.Fatalf("counts after undo = %+v, want 2 new", n)
	}
}

func TestEmptyDeckDone(t *testing.T) {
	f := newFixture(t)
	r := f.next()
	if !r.Done || r.NextDueAt != nil {
		t.Fatalf("next = %+v", r)
	}
	if _, err := f.svc.Next(f.ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing deck: err = %v", err)
	}
}

func TestDoneReportsNextReviewDue(t *testing.T) {
	f := newFixture(t)
	c := f.card("q", t0.Add(-time.Hour))
	due := t0.Add(72 * time.Hour)
	f.force(f.fwd(c), srs.CardState{State: srs.Review, Due: due, Reps: 1, Stability: 5, Difficulty: 5, ScheduledDays: 3, LastReview: t0}, t0)
	r := f.next()
	if !r.Done || r.NextDueAt == nil || !r.NextDueAt.Equal(due) {
		t.Fatalf("next = %+v", r)
	}
}
