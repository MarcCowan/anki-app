package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"anki-app/server/internal/srs"
)

// review applies rating r to state c at now using the real scheduler.
func review(t *testing.T, s *Store, c ReviewState, r srs.Rating, now time.Time) ReviewState {
	t.Helper()
	after, err := srs.NewScheduler(0.9).Next(c.Sched, now, r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ApplyReview(context.Background(), ReviewInput{
		StateID: c.ID, ExpectedReps: c.Sched.Reps, Rating: r,
		Before: c.Sched, After: after, ReviewedAt: now, DurationMs: 1500,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestApplyReviewWritesLogAndState(t *testing.T) {
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	card := mustCard(t, s, d.ID, "Q", t0)
	c := forward(t, s, card.ID)
	now := t0.Add(time.Minute)
	after, _ := srs.NewScheduler(0.9).Next(c.Sched, now, srs.Good)
	got := review(t, s, c, srs.Good, now)
	if got.Sched != after || got.CardID != card.ID || got.Reverse {
		t.Fatalf("state = %+v, want sched %+v", got, after)
	}
	if u, err := s.GetCard(context.Background(), card.ID); err != nil || !u.UpdatedAt.Equal(now) || u.Sched != after {
		t.Fatalf("card after review = %+v, %v", u, err)
	}
	var rating, stateBefore, dur int
	var reviewedAt int64
	err := s.db.QueryRow("SELECT rating, state_before, duration_ms, reviewed_at FROM review_logs WHERE review_state_id = ?", c.ID).
		Scan(&rating, &stateBefore, &dur, &reviewedAt)
	if err != nil {
		t.Fatal(err)
	}
	if rating != 3 || stateBefore != int(c.Sched.State) || dur != 1500 || reviewedAt != toMs(now) {
		t.Fatalf("log = rating %d stateBefore %d dur %d at %d", rating, stateBefore, dur, reviewedAt)
	}
}

func TestApplyReviewStale(t *testing.T) {
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	c := forward(t, s, mustCard(t, s, d.ID, "Q", t0).ID)
	_, err := s.ApplyReview(context.Background(), ReviewInput{
		StateID: c.ID, ExpectedReps: 5, Rating: srs.Good, Before: c.Sched, After: c.Sched, ReviewedAt: t0,
	}, t0)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v", err)
	}
	if n := count(t, s, "review_logs"); n != 0 {
		t.Fatalf("logs = %d", n)
	}
}

func TestApplyReviewMissingCard(t *testing.T) {
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	card := mustCard(t, s, d.ID, "Q", t0)
	c := forward(t, s, card.ID)
	if err := s.DeleteCard(context.Background(), card.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.ApplyReview(context.Background(), ReviewInput{
		StateID: c.ID, ExpectedReps: 0, Rating: srs.Good, Before: c.Sched, After: c.Sched, ReviewedAt: t0,
	}, t0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestUndoRestoresExactly(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	c := forward(t, s, mustCard(t, s, d.ID, "Q", t0).ID)
	// Two reviews, so the card has a LastReview and non-trivial state; undo the second.
	c1 := review(t, s, c, srs.Good, t0.Add(time.Minute))
	c2 := review(t, s, c1, srs.Again, c1.Sched.Due)
	if _, err := s.UndoLatestReview(ctx, c.ID, c2.Sched.Reps+1, t0); !errors.Is(err, ErrStale) {
		t.Fatalf("stale undo: %v", err)
	}
	got, err := s.UndoLatestReview(ctx, c.ID, c2.Sched.Reps, c1.Sched.Due.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got.Sched != c1.Sched {
		t.Fatalf("after undo:\n got %+v\nwant %+v", got.Sched, c1.Sched)
	}
	got, err = s.UndoLatestReview(ctx, c.ID, c1.Sched.Reps, t0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sched != c.Sched {
		t.Fatalf("after second undo:\n got %+v\nwant %+v", got.Sched, c.Sched)
	}
	if n := count(t, s, "review_logs"); n != 0 {
		t.Fatalf("logs = %d", n)
	}
}

func TestUndoNothing(t *testing.T) {
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	c := forward(t, s, mustCard(t, s, d.ID, "Q", t0).ID)
	if _, err := s.UndoLatestReview(context.Background(), c.ID, 0, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.UndoLatestReview(context.Background(), 999, 0, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing card: err = %v", err)
	}
}

func TestCountNewIntroducedSince(t *testing.T) {
	s := openTest(t)
	d := mustDeck(t, s, "Cpp")
	other := mustDeck(t, s, "Other")
	dayStart := t0.Add(-6 * time.Hour)
	yesterday := dayStart.Add(-2 * time.Hour)
	a := forward(t, s, mustCard(t, s, d.ID, "a", yesterday).ID)
	b := forward(t, s, mustCard(t, s, d.ID, "b", yesterday).ID)
	y := forward(t, s, mustCard(t, s, d.ID, "y", yesterday).ID)
	r := forward(t, s, mustCard(t, s, d.ID, "r", yesterday).ID)
	o := forward(t, s, mustCard(t, s, other.ID, "o", yesterday).ID)
	review(t, s, a, srs.Good, t0)        // new, today
	review(t, s, b, srs.Easy, t0)        // new, today
	review(t, s, y, srs.Good, yesterday) // new, yesterday
	review(t, s, o, srs.Good, t0)        // new, today, other deck
	rr := review(t, s, r, srs.Easy, yesterday)
	review(t, s, rr, srs.Good, t0) // Review-state card, today
	n, err := s.CountNewIntroducedSince(context.Background(), d.ID, dayStart)
	if err != nil || n != 2 {
		t.Fatalf("CountNewIntroducedSince = %d, %v", n, err)
	}
}
