// Package study builds the study queue (spec §5.1, two-sided spec §3) and
// records answers and undos. The queue is made of review states: one per
// direction of a card.
package study

import (
	"context"
	"fmt"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/srs"
	"anki-app/server/internal/store"
)

// LearnAhead is how far ahead a learning card may be shown when nothing else is due.
const LearnAhead = 20 * time.Minute

// MaxDurationMs caps the recorded time spent on one card (10 minutes).
const MaxDurationMs = 10 * 60 * 1000

var (
	learningStates = []srs.State{srs.Learning, srs.Relearning}
	reviewStates   = []srs.State{srs.Review}
	scheduled      = []srs.State{srs.Learning, srs.Review, srs.Relearning}
)

// Service is the study queue and answer logic.
type Service struct {
	Store *store.Store
	Clock clock.Clock
	Loc   *time.Location
}

// Counts are a deck's due items, not counting buried ones. NewToday is how many
// new items were introduced today, shown against the deck's daily target.
type Counts struct{ New, Learning, Review, NewToday int }

// Preview is the outcome of one rating.
type Preview struct {
	Due           time.Time
	IntervalLabel string
}

// NextResult is the next item to study, or Done.
type NextResult struct {
	Done      bool
	State     store.ReviewState      // valid when !Done; State.Reverse: show the back first
	Card      store.Card             // valid when !Done: the state's card
	Previews  map[srs.Rating]Preview // valid when !Done
	Remaining Counts
	NextDueAt *time.Time // set when Done and a future item exists
}

func (s *Service) scheduler(ctx context.Context) (*srs.Scheduler, store.Settings, error) {
	st, err := s.Store.GetSettings(ctx)
	if err != nil {
		return nil, store.Settings{}, err
	}
	return srs.NewScheduler(st.DesiredRetention), st, nil
}

// counts returns the deck's counts; dayStart is also the bury cut-off.
func (s *Service) counts(ctx context.Context, deckID int64, now, dayStart time.Time) (Counts, error) {
	var c Counts
	var err error
	if c.Learning, err = s.Store.CountDue(ctx, deckID, learningStates, now, dayStart); err != nil {
		return Counts{}, err
	}
	if c.Review, err = s.Store.CountDue(ctx, deckID, reviewStates, now, dayStart); err != nil {
		return Counts{}, err
	}
	if c.New, err = s.Store.CountState(ctx, deckID, srs.New, dayStart); err != nil {
		return Counts{}, err
	}
	if c.NewToday, err = s.Store.CountNewIntroducedSince(ctx, deckID, dayStart); err != nil {
		return Counts{}, err
	}
	return c, nil
}

// Counts returns a deck's due counts at the current time.
func (s *Service) Counts(ctx context.Context, deckID int64) (Counts, error) {
	if _, err := s.Store.GetDeck(ctx, deckID); err != nil {
		return Counts{}, err
	}
	st, err := s.Store.GetSettings(ctx)
	if err != nil {
		return Counts{}, err
	}
	now := s.now()
	return s.counts(ctx, deckID, now, DayStart(now, s.Loc, st.DayRolloverHour))
}

// Next returns the next item to study. store.ErrNotFound if no deck.
func (s *Service) Next(ctx context.Context, deckID int64) (NextResult, error) {
	now := s.now()
	if _, err := s.Store.GetDeck(ctx, deckID); err != nil {
		return NextResult{}, err
	}
	sched, st, err := s.scheduler(ctx)
	if err != nil {
		return NextResult{}, err
	}
	dayStart := DayStart(now, s.Loc, st.DayRolloverHour)
	remaining, err := s.counts(ctx, deckID, now, dayStart)
	if err != nil {
		return NextResult{}, err
	}

	state, ok, err := s.pick(ctx, deckID, now, dayStart)
	if err != nil {
		return NextResult{}, err
	}
	if !ok {
		res := NextResult{Done: true, Remaining: remaining}
		next, found, err := s.Store.MinDue(ctx, deckID, scheduled, dayStart)
		if err != nil {
			return NextResult{}, err
		}
		if found {
			res.NextDueAt = &next
		}
		return res, nil
	}
	card, err := s.Store.GetCard(ctx, state.CardID)
	if err != nil {
		return NextResult{}, fmt.Errorf("next: %w", err)
	}

	outcomes, err := sched.Preview(state.Sched, now)
	if err != nil {
		return NextResult{}, fmt.Errorf("next: %w", err)
	}
	previews := make(map[srs.Rating]Preview, len(outcomes))
	for r, o := range outcomes {
		previews[r] = Preview{Due: o.Due, IntervalLabel: srs.FormatInterval(o.Due.Sub(now))}
	}
	return NextResult{State: state, Card: card, Previews: previews, Remaining: remaining}, nil
}

// pick applies the queue order: due learning, due review, new, learn-ahead.
// Every step skips states whose sibling was answered since dayStart.
func (s *Service) pick(ctx context.Context, deckID int64, now, dayStart time.Time) (store.ReviewState, bool, error) {
	if r, ok, err := s.Store.FirstDue(ctx, deckID, learningStates, now, dayStart); err != nil || ok {
		return r, ok, err
	}
	if r, ok, err := s.Store.FirstDue(ctx, deckID, reviewStates, now, dayStart); err != nil || ok {
		return r, ok, err
	}
	if r, ok, err := s.Store.FirstNew(ctx, deckID, dayStart); err != nil || ok {
		return r, ok, err
	}
	return s.Store.FirstDue(ctx, deckID, learningStates, now.Add(LearnAhead), dayStart)
}

func clampDuration(ms int64) int64 {
	return min(max(ms, 0), MaxDurationMs)
}

// Answer records rating r for a review state. store.ErrStale if expectedReps
// does not match; store.ErrNotFound if the state is gone.
func (s *Service) Answer(ctx context.Context, stateID int64, r srs.Rating, expectedReps, durationMs int64) (store.ReviewState, error) {
	now := s.now()
	state, err := s.Store.GetState(ctx, stateID)
	if err != nil {
		return store.ReviewState{}, err
	}
	if state.Sched.Reps != expectedReps {
		return store.ReviewState{}, fmt.Errorf("answer state %d: %w", stateID, store.ErrStale)
	}
	sched, _, err := s.scheduler(ctx)
	if err != nil {
		return store.ReviewState{}, err
	}
	after, err := sched.Next(state.Sched, now, r)
	if err != nil {
		return store.ReviewState{}, fmt.Errorf("answer state %d: %w", stateID, err)
	}
	return s.Store.ApplyReview(ctx, store.ReviewInput{
		StateID:      stateID,
		ExpectedReps: expectedReps,
		Rating:       r,
		Before:       state.Sched,
		After:        after,
		ReviewedAt:   now,
		DurationMs:   clampDuration(durationMs),
	}, now)
}

// Undo reverts the state's latest review.
func (s *Service) Undo(ctx context.Context, stateID, expectedReps int64) (store.ReviewState, error) {
	return s.Store.UndoLatestReview(ctx, stateID, expectedReps, s.now())
}

// now is the current time at the millisecond precision the database stores,
// so intervals computed against stored due times are exact (1m, not 59.999s).
func (s *Service) now() time.Time {
	return s.Clock.Now().Truncate(time.Millisecond)
}
