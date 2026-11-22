package srs

import (
	"reflect"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

func mustNext(t *testing.T, s *Scheduler, c CardState, now time.Time, r Rating) CardState {
	t.Helper()
	n, err := s.Next(c, now, r)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustPreview(t *testing.T, s *Scheduler, c CardState, now time.Time) map[Rating]CardState {
	t.Helper()
	p, err := s.Preview(c, now)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// reviewCard returns a card in Review state whose last review was `ago` before now.
func reviewCard(t *testing.T, s *Scheduler, now time.Time, ago time.Duration) CardState {
	t.Helper()
	start := now.Add(-ago)
	c := mustNext(t, s, NewCardState(start), start, Easy)
	if c.State != Review {
		t.Fatalf("Easy on a new card gave state %v, want Review", c.State)
	}
	return c
}

func TestNewCardGoodSchedulesAhead(t *testing.T) {
	s := NewScheduler(0.9)
	c := mustNext(t, s, NewCardState(t0), t0, Good)
	if !c.Due.After(t0) || c.Reps != 1 || c.State == New {
		t.Fatalf("after Good: %+v", c)
	}
	if !c.LastReview.Equal(t0) {
		t.Fatalf("LastReview = %v", c.LastReview)
	}
}

func TestPreviewOrdering(t *testing.T) {
	s := NewScheduler(0.9)
	now := t0
	cards := map[string]CardState{
		"new":    NewCardState(now),
		"review": reviewCard(t, s, now, 10*24*time.Hour),
	}
	for name, c := range cards {
		p := mustPreview(t, s, c, now)
		if len(p) != 4 {
			t.Fatalf("%s: %d previews", name, len(p))
		}
		order := []Rating{Again, Hard, Good, Easy}
		for i := 1; i < len(order); i++ {
			if p[order[i]].Due.Before(p[order[i-1]].Due) {
				t.Errorf("%s: Due(%v)=%v before Due(%v)=%v", name, order[i], p[order[i]].Due, order[i-1], p[order[i-1]].Due)
			}
		}
	}
}

func TestNextMatchesPreview(t *testing.T) {
	s := NewScheduler(0.9)
	for _, c := range []CardState{NewCardState(t0), reviewCard(t, s, t0, 10*24*time.Hour)} {
		p := mustPreview(t, s, c, t0)
		for _, r := range []Rating{Again, Hard, Good, Easy} {
			if n := mustNext(t, s, c, t0, r); !reflect.DeepEqual(n, p[r]) {
				t.Errorf("rating %v: Next %+v != Preview %+v", r, n, p[r])
			}
		}
	}
}

func TestAgainOnReviewIsRelearning(t *testing.T) {
	s := NewScheduler(0.9)
	c := reviewCard(t, s, t0, 10*24*time.Hour)
	n := mustNext(t, s, c, t0, Again)
	if n.State != Relearning || n.Lapses != 1 {
		t.Fatalf("after Again: %+v", n)
	}
	if n.ElapsedDays != 10 {
		t.Fatalf("ElapsedDays = %d, want 10", n.ElapsedDays)
	}
}

func TestRoundTripStateFields(t *testing.T) {
	cases := []CardState{
		NewCardState(t0),
		{
			State: Relearning, Due: t0.Add(10 * time.Minute), Stability: 3.25, Difficulty: 6.5,
			ElapsedDays: 4, ScheduledDays: 7, Reps: 9, Lapses: 2, RemainingSteps: 1,
			LastReview: t0.Add(-time.Hour),
		},
	}
	for _, c := range cases {
		back := fromFSRS(toFSRS(c), c.ElapsedDays)
		if !reflect.DeepEqual(back, c) {
			t.Errorf("round trip:\n got %+v\nwant %+v", back, c)
		}
	}
	if !fromFSRS(toFSRS(NewCardState(t0)), 0).LastReview.IsZero() {
		t.Error("zero LastReview not preserved")
	}
}
