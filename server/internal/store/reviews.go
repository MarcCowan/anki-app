package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"anki-app/server/internal/srs"
)

// ReviewInput is one answered review state: its schedule before and after.
type ReviewInput struct {
	StateID      int64
	ExpectedReps int64
	Rating       srs.Rating
	Before       srs.CardState
	After        srs.CardState
	ReviewedAt   time.Time
	DurationMs   int64
}

const (
	// Served by review_logs_state_time (review_state_id, reviewed_at).
	qLatestLog = `SELECT id, state_before, due_before, stability_before, difficulty_before, elapsed_days_before,
		scheduled_days_before, reps_before, lapses_before, remaining_steps_before, last_review_before
		FROM review_logs WHERE review_state_id = ? ORDER BY reviewed_at DESC, id DESC LIMIT 1`

	// The deck's states (review_states_deck_state_due) joined to their logs since day
	// start (review_logs_state_time). Each direction counts as one new item.
	qCountNewIntroducedSince = `SELECT COUNT(*) FROM review_states s JOIN review_logs l ON l.review_state_id = s.id
		WHERE s.deck_id = ? AND l.reviewed_at >= ? AND l.state_before = 0`

	// The schedule lives on review_states; the card's updated_at records the change.
	updateSched = `UPDATE review_states SET state = ?, due = ?, stability = ?, difficulty = ?, elapsed_days = ?,
		scheduled_days = ?, reps = ?, lapses = ?, remaining_steps = ?, last_review = ?
		WHERE id = ? AND reps = ? RETURNING card_id`
	touchCard = "UPDATE cards SET updated_at = ? WHERE id = ?"
)

// updateSchedTx writes a state's schedule if its reps still equal expectedReps,
// and bumps the card's updated_at. ErrStale or ErrNotFound when no row matched.
func updateSchedTx(ctx context.Context, tx *sql.Tx, stateID, expectedReps int64, st srs.CardState, now time.Time) error {
	var cardID int64
	args := append(schedArgs(st), stateID, expectedReps)
	err := tx.QueryRowContext(ctx, updateSched, args...).Scan(&cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return staleOrMissing(ctx, tx, stateID)
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, touchCard, toMs(now), cardID)
	return err
}

// staleOrMissing explains why a guarded UPDATE matched no row.
func staleOrMissing(ctx context.Context, tx *sql.Tx, stateID int64) error {
	var one int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM review_states WHERE id = ?", stateID).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return err
	}
	return ErrStale
}

// ApplyReview stores the state's new schedule and a log of its previous one,
// in one transaction. ErrStale if the state's reps != ExpectedReps;
// ErrNotFound if the state is gone.
func (s *Store) ApplyReview(ctx context.Context, in ReviewInput, now time.Time) (ReviewState, error) {
	var out ReviewState
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := updateSchedTx(ctx, tx, in.StateID, in.ExpectedReps, in.After, now); err != nil {
			return err
		}
		b := in.Before
		_, err := tx.ExecContext(ctx, `INSERT INTO review_logs (review_state_id, rating, reviewed_at, duration_ms,
			state_before, due_before, stability_before, difficulty_before, elapsed_days_before,
			scheduled_days_before, reps_before, lapses_before, remaining_steps_before, last_review_before)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.StateID, int(in.Rating), toMs(in.ReviewedAt), in.DurationMs,
			int(b.State), toMs(b.Due), b.Stability, b.Difficulty, b.ElapsedDays,
			b.ScheduledDays, b.Reps, b.Lapses, b.RemainingSteps, nullableMs(b.LastReview))
		if err != nil {
			return err
		}
		out, err = getStateTx(ctx, tx, in.StateID)
		return err
	})
	if err != nil {
		return ReviewState{}, fmt.Errorf("apply review to state %d: %w", in.StateID, err)
	}
	return out, nil
}

// UndoLatestReview restores the state's schedule from its latest log and deletes
// that log, in one transaction. ErrNotFound if the state or a log is missing;
// ErrStale if the state's reps != expectedReps.
func (s *Store) UndoLatestReview(ctx context.Context, stateID, expectedReps int64, now time.Time) (ReviewState, error) {
	var out ReviewState
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var logID int64
		sp := newSchedScan()
		err := tx.QueryRowContext(ctx, qLatestLog, stateID).Scan(append([]any{&logID}, sp.dest()...)...)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := updateSchedTx(ctx, tx, stateID, expectedReps, sp.sched(), now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM review_logs WHERE id = ?", logID); err != nil {
			return err
		}
		out, err = getStateTx(ctx, tx, stateID)
		return err
	})
	if err != nil {
		return ReviewState{}, fmt.Errorf("undo review of state %d: %w", stateID, err)
	}
	return out, nil
}

// CountNewIntroducedSince counts the deck's New states answered at or after since.
func (s *Store) CountNewIntroducedSince(ctx context.Context, deckID int64, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, qCountNewIntroducedSince, deckID, toMs(since)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count new introduced: %w", err)
	}
	return n, nil
}
