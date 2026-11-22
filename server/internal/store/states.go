package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"anki-app/server/internal/srs"
)

// ReviewState is one direction of a card and its FSRS schedule (two-sided spec §2).
type ReviewState struct {
	ID, CardID, DeckID int64
	Reverse            bool // false: front→back; true: back→front
	Sched              srs.CardState
}

// stateSchedCols are the schedule columns of review_states, aliased s.
const stateSchedCols = `s.state, s.due, s.stability, s.difficulty, s.elapsed_days, s.scheduled_days,
	s.reps, s.lapses, s.remaining_steps, s.last_review`

const stateCols = "s.id, s.card_id, s.deck_id, s.reverse, " + stateSchedCols

// schedScan holds the scan targets for stateSchedCols.
type schedScan struct {
	st   srs.CardState
	fsrs int
	due  int64
	last sql.NullInt64
}

func newSchedScan() *schedScan { return &schedScan{} }

func (p *schedScan) dest() []any {
	return []any{&p.fsrs, &p.due, &p.st.Stability, &p.st.Difficulty, &p.st.ElapsedDays,
		&p.st.ScheduledDays, &p.st.Reps, &p.st.Lapses, &p.st.RemainingSteps, &p.last}
}

func (p *schedScan) sched() srs.CardState {
	st := p.st
	st.State, st.Due = srs.State(p.fsrs), fromMs(p.due)
	if p.last.Valid {
		st.LastReview = fromMs(p.last.Int64)
	}
	return st
}

func scanState(row scanner) (ReviewState, error) {
	var r ReviewState
	sp := newSchedScan()
	if err := row.Scan(append([]any{&r.ID, &r.CardID, &r.DeckID, &r.Reverse}, sp.dest()...)...); err != nil {
		return ReviewState{}, err
	}
	r.Sched = sp.sched()
	return r, nil
}

func schedArgs(st srs.CardState) []any {
	return []any{int(st.State), toMs(st.Due), st.Stability, st.Difficulty, st.ElapsedDays,
		st.ScheduledDays, st.Reps, st.Lapses, st.RemainingSteps, nullableMs(st.LastReview)}
}

// schedArgsNew is schedArgs for a New state created at now.
func schedArgsNew(now time.Time) []any { return schedArgs(srs.NewCardState(now)) }

// insertNewStateTx adds a New state for one direction of a card.
func insertNewStateTx(ctx context.Context, tx *sql.Tx, cardID, deckID int64, reverse bool, now time.Time) error {
	args := append([]any{cardID, deckID, reverse}, schedArgsNew(now)...)
	_, err := tx.ExecContext(ctx, `INSERT INTO review_states (card_id, deck_id, reverse, state, due, stability,
		difficulty, elapsed_days, scheduled_days, reps, lapses, remaining_steps, last_review)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
	return err
}

func getStateTx(ctx context.Context, q queryRower, id int64) (ReviewState, error) {
	r, err := scanState(q.QueryRowContext(ctx, "SELECT "+stateCols+" FROM review_states s WHERE s.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewState{}, ErrNotFound
	}
	return r, err
}

// GetState returns one review state. ErrNotFound if it does not exist.
func (s *Store) GetState(ctx context.Context, id int64) (ReviewState, error) {
	r, err := getStateTx(ctx, s.db, id)
	if err != nil {
		return ReviewState{}, fmt.Errorf("get state %d: %w", id, err)
	}
	return r, nil
}

// Served by UNIQUE(card_id, reverse).
const qCardStates = "SELECT " + stateCols + " FROM review_states s WHERE s.card_id = ? ORDER BY s.reverse"

// CardStates returns a card's review states, forward first. Empty if no card.
func (s *Store) CardStates(ctx context.Context, cardID int64) ([]ReviewState, error) {
	rows, err := s.db.QueryContext(ctx, qCardStates, cardID)
	if err != nil {
		return nil, fmt.Errorf("card states: %w", err)
	}
	defer rows.Close()
	out := []ReviewState{}
	for rows.Next() {
		r, err := scanState(rows)
		if err != nil {
			return nil, fmt.Errorf("card states: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("card states: %w", err)
	}
	return out, nil
}

// Queue queries (two-sided spec §3). All are served by review_states_deck_state_due;
// the burying check by UNIQUE(card_id, reverse) and review_logs_state_time
// (asserted in plan_test.go). Every query takes the bury cut-off as its last argument.

// notBuried excludes a state whose sibling direction was answered at or after ?.
const notBuried = ` AND NOT EXISTS (SELECT 1 FROM review_states o JOIN review_logs l ON l.review_state_id = o.id
	WHERE o.card_id = s.card_id AND o.reverse <> s.reverse AND l.reviewed_at >= ?)`

const (
	qFirstNew   = "SELECT " + stateCols + " FROM review_states s WHERE s.deck_id = ? AND s.state = 0" + notBuried + " ORDER BY s.due, s.id LIMIT 1"
	qCountState = "SELECT COUNT(*) FROM review_states s WHERE s.deck_id = ? AND s.state = ?" + notBuried
)

func qFirstDue(n int) string {
	return "SELECT " + stateCols + " FROM review_states s WHERE s.deck_id = ? AND s.state IN (" + placeholders(n) +
		") AND s.due <= ?" + notBuried + " ORDER BY s.due, s.id LIMIT 1"
}

func qCountDue(n int) string {
	return "SELECT COUNT(*) FROM review_states s WHERE s.deck_id = ? AND s.state IN (" + placeholders(n) + ") AND s.due <= ?" + notBuried
}

func qMinDue(n int) string {
	return "SELECT MIN(s.due) FROM review_states s WHERE s.deck_id = ? AND s.state IN (" + placeholders(n) + ")" + notBuried
}

// stateArgs builds [deckID, states..., extra...].
func stateArgs(deckID int64, states []srs.State, extra ...any) []any {
	args := make([]any, 0, 1+len(states)+len(extra))
	args = append(args, deckID)
	for _, st := range states {
		args = append(args, int(st))
	}
	return append(args, extra...)
}

func (s *Store) firstState(ctx context.Context, q string, args ...any) (ReviewState, bool, error) {
	r, err := scanState(s.db.QueryRowContext(ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewState{}, false, nil
	}
	if err != nil {
		return ReviewState{}, false, err
	}
	return r, true, nil
}

// FirstDue returns the unburied state in states with the earliest due <= dueAtOrBefore.
// A state is buried if its sibling was answered at or after buryFrom.
func (s *Store) FirstDue(ctx context.Context, deckID int64, states []srs.State, dueAtOrBefore, buryFrom time.Time) (ReviewState, bool, error) {
	r, ok, err := s.firstState(ctx, qFirstDue(len(states)), stateArgs(deckID, states, toMs(dueAtOrBefore), toMs(buryFrom))...)
	if err != nil {
		return ReviewState{}, false, fmt.Errorf("first due: %w", err)
	}
	return r, ok, nil
}

// FirstNew returns the oldest unburied New state (a new state's due is its creation time).
func (s *Store) FirstNew(ctx context.Context, deckID int64, buryFrom time.Time) (ReviewState, bool, error) {
	r, ok, err := s.firstState(ctx, qFirstNew, deckID, toMs(buryFrom))
	if err != nil {
		return ReviewState{}, false, fmt.Errorf("first new: %w", err)
	}
	return r, ok, nil
}

// CountDue counts unburied states in states with due <= dueAtOrBefore.
func (s *Store) CountDue(ctx context.Context, deckID int64, states []srs.State, dueAtOrBefore, buryFrom time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, qCountDue(len(states)), stateArgs(deckID, states, toMs(dueAtOrBefore), toMs(buryFrom))...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count due: %w", err)
	}
	return n, nil
}

// CountState counts a deck's unburied states in one FSRS state.
func (s *Store) CountState(ctx context.Context, deckID int64, st srs.State, buryFrom time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, qCountState, deckID, int(st), toMs(buryFrom)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count state: %w", err)
	}
	return n, nil
}

// MinDue returns the earliest due time among unburied states in states, if any.
func (s *Store) MinDue(ctx context.Context, deckID int64, states []srs.State, buryFrom time.Time) (time.Time, bool, error) {
	var v sql.NullInt64
	if err := s.db.QueryRowContext(ctx, qMinDue(len(states)), stateArgs(deckID, states, toMs(buryFrom))...).Scan(&v); err != nil {
		return time.Time{}, false, fmt.Errorf("min due: %w", err)
	}
	if !v.Valid {
		return time.Time{}, false, nil
	}
	return fromMs(v.Int64), true, nil
}
