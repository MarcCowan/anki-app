// Package srs wraps go-fsrs. It is the only package that imports it, and it
// is pure: no database, no clock. No go-fsrs type appears in its exported API.
package srs

import (
	"fmt"
	"time"

	fsrs "github.com/open-spaced-repetition/go-fsrs/v4"
)

// State is a card's learning stage.
type State int

// Card states, matching the values stored in the database.
const (
	New        State = 0
	Learning   State = 1
	Review     State = 2
	Relearning State = 3
)

// Rating is the answer button pressed.
type Rating int

// Ratings, matching the API's 1..4.
const (
	Again Rating = 1
	Hard  Rating = 2
	Good  Rating = 3
	Easy  Rating = 4
)

// CardState is a card's full scheduling state.
type CardState struct {
	State                      State
	Due                        time.Time
	Stability, Difficulty      float64
	ElapsedDays, ScheduledDays int64 // ElapsedDays: whole days between the previous review and the latest one
	Reps, Lapses               int64
	RemainingSteps             int64     // position in the (re)learning steps
	LastReview                 time.Time // zero = never reviewed
}

// NewCardState is the state of a card that has never been reviewed.
func NewCardState(now time.Time) CardState {
	return fromFSRS(fsrs.NewCard(now.UTC()), 0)
}

// Scheduler computes next states for one desired retention.
type Scheduler struct {
	f *fsrs.FSRS
}

// NewScheduler uses the library's default weights with short-term
// (same-day learning-step) scheduling enabled.
func NewScheduler(desiredRetention float64) *Scheduler {
	p := fsrs.DefaultParam()
	p.RequestRetention = desiredRetention
	p.EnableShortTerm = true
	return &Scheduler{f: fsrs.NewFSRS(p)}
}

// Next returns the card's state after answering r at now.
func (s *Scheduler) Next(c CardState, now time.Time, r Rating) (CardState, error) {
	if r < Again || r > Easy {
		return CardState{}, fmt.Errorf("srs: invalid rating %d", r)
	}
	info, err := s.f.Next(toFSRS(c), now, fsrs.Rating(r))
	if err != nil {
		return CardState{}, fmt.Errorf("srs: next: %w", err)
	}
	return fromFSRS(info.Card, elapsedDays(c, now)), nil
}

// Preview returns the next state for all four ratings.
func (s *Scheduler) Preview(c CardState, now time.Time) (map[Rating]CardState, error) {
	log, err := s.f.Repeat(toFSRS(c), now)
	if err != nil {
		return nil, fmt.Errorf("srs: preview: %w", err)
	}
	ed := elapsedDays(c, now)
	out := make(map[Rating]CardState, 4)
	for _, r := range []Rating{Again, Hard, Good, Easy} {
		info, ok := log[fsrs.Rating(r)]
		if !ok {
			return nil, fmt.Errorf("srs: preview: missing rating %d", r)
		}
		out[r] = fromFSRS(info.Card, ed)
	}
	return out, nil
}

func elapsedDays(c CardState, now time.Time) int64 {
	if c.LastReview.IsZero() || !now.After(c.LastReview) {
		return 0
	}
	return int64(now.Sub(c.LastReview) / (24 * time.Hour))
}

func toFSRS(c CardState) fsrs.Card {
	return fsrs.Card{
		Due:            c.Due,
		Stability:      c.Stability,
		Difficulty:     c.Difficulty,
		ScheduledDays:  uint64(max(c.ScheduledDays, 0)),
		Reps:           uint64(max(c.Reps, 0)),
		Lapses:         uint64(max(c.Lapses, 0)),
		State:          fsrs.State(c.State),
		LastReview:     c.LastReview,
		RemainingSteps: int(c.RemainingSteps),
	}
}

func fromFSRS(c fsrs.Card, elapsed int64) CardState {
	return CardState{
		State:          State(c.State),
		Due:            msUTC(c.Due),
		Stability:      c.Stability,
		Difficulty:     c.Difficulty,
		ElapsedDays:    elapsed,
		ScheduledDays:  int64(c.ScheduledDays),
		Reps:           int64(c.Reps),
		Lapses:         int64(c.Lapses),
		RemainingSteps: int64(c.RemainingSteps),
		LastReview:     msUTC(c.LastReview),
	}
}

// msUTC truncates to the millisecond precision the database stores, in UTC.
// The zero time stays zero.
func msUTC(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.Truncate(time.Millisecond).UTC()
}
