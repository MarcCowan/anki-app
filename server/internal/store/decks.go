package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Deck is a named collection of cards. NewPerDay is a daily target, not a cap;
// TwoSided is the default for new cards (two-sided spec §1).
type Deck struct {
	ID        int64
	Name      string
	NewPerDay int
	TwoSided  bool
	CreatedAt time.Time
}

// DeckPatch changes a deck; nil fields are left as they are.
//
// TwoSided true also makes every card in the deck two-sided. TwoSided false
// changes only the default unless RemoveReverses is set, which also removes
// every card's reverse direction and its history. RemoveReverses without
// TwoSided false is a caller error.
type DeckPatch struct {
	Name           *string
	NewPerDay      *int
	TwoSided       *bool
	RemoveReverses bool
}

const deckCols = "id, name, new_per_day, two_sided, created_at"

type scanner interface{ Scan(...any) error }

func scanDeck(row scanner) (Deck, error) {
	var d Deck
	var created int64
	if err := row.Scan(&d.ID, &d.Name, &d.NewPerDay, &d.TwoSided, &created); err != nil {
		return Deck{}, err
	}
	d.CreatedAt = fromMs(created)
	return d, nil
}

// CreateDeck inserts a deck. ErrConflict if the name is taken.
func (s *Store) CreateDeck(ctx context.Context, name string, twoSided bool, now time.Time) (Deck, error) {
	var d Deck
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		d, err = createDeckTx(ctx, tx, name, twoSided, now)
		return err
	})
	return d, err
}

func createDeckTx(ctx context.Context, tx *sql.Tx, name string, twoSided bool, now time.Time) (Deck, error) {
	row := tx.QueryRowContext(ctx,
		"INSERT INTO decks (name, two_sided, created_at) VALUES (?, ?, ?) RETURNING "+deckCols, name, twoSided, toMs(now))
	d, err := scanDeck(row)
	if isUniqueViolation(err) {
		return Deck{}, fmt.Errorf("create deck: %w", ErrConflict)
	}
	if err != nil {
		return Deck{}, fmt.Errorf("create deck: %w", err)
	}
	return d, nil
}

// GetDeck returns one deck. ErrNotFound if it does not exist.
func (s *Store) GetDeck(ctx context.Context, id int64) (Deck, error) {
	d, err := scanDeck(s.db.QueryRowContext(ctx, "SELECT "+deckCols+" FROM decks WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Deck{}, fmt.Errorf("get deck %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Deck{}, fmt.Errorf("get deck %d: %w", id, err)
	}
	return d, nil
}

// ListDecks returns every deck ordered by name, case-insensitively.
func (s *Store) ListDecks(ctx context.Context) ([]Deck, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+deckCols+" FROM decks ORDER BY name COLLATE NOCASE, id")
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	defer rows.Close()
	decks := []Deck{}
	for rows.Next() {
		d, err := scanDeck(rows)
		if err != nil {
			return nil, fmt.Errorf("list decks: %w", err)
		}
		decks = append(decks, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	return decks, nil
}

const (
	// Served by cards_deck. A card with a reverse already keeps it (and its history).
	qAddDeckReverses = `INSERT INTO review_states (card_id, deck_id, reverse, state, due, stability,
		difficulty, elapsed_days, scheduled_days, reps, lapses, remaining_steps, last_review)
		SELECT c.id, c.deck_id, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM cards c
		WHERE c.deck_id = ? AND NOT EXISTS (SELECT 1 FROM review_states s WHERE s.card_id = c.id AND s.reverse = 1)`
	// Served by review_states_deck_state_due (deck_id prefix).
	qDeleteDeckReverses = "DELETE FROM review_states WHERE deck_id = ? AND reverse = 1"
	// Served by cards_deck.
	qSetDeckCardsTwoSided = "UPDATE cards SET two_sided = ? WHERE deck_id = ?"
)

// UpdateDeck applies a DeckPatch in one transaction.
func (s *Store) UpdateDeck(ctx context.Context, id int64, p DeckPatch, now time.Time) (Deck, error) {
	if p.RemoveReverses && (p.TwoSided == nil || *p.TwoSided) {
		return Deck{}, fmt.Errorf("update deck %d: RemoveReverses needs TwoSided false", id)
	}
	var d Deck
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx,
			`UPDATE decks SET name = COALESCE(?, name), new_per_day = COALESCE(?, new_per_day),
			 two_sided = COALESCE(?, two_sided) WHERE id = ? RETURNING `+deckCols, p.Name, p.NewPerDay, p.TwoSided, id)
		var err error
		if d, err = scanDeck(row); err != nil {
			return err
		}
		switch {
		case p.TwoSided != nil && *p.TwoSided:
			args := append(schedArgsNew(now), id)
			if _, err := tx.ExecContext(ctx, qAddDeckReverses, args...); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, qSetDeckCardsTwoSided, true, id)
		case p.RemoveReverses:
			if _, err := tx.ExecContext(ctx, qDeleteDeckReverses, id); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, qSetDeckCardsTwoSided, false, id)
		}
		return err
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Deck{}, fmt.Errorf("update deck %d: %w", id, ErrNotFound)
	case isUniqueViolation(err):
		return Deck{}, fmt.Errorf("update deck %d: %w", id, ErrConflict)
	case err != nil:
		return Deck{}, fmt.Errorf("update deck %d: %w", id, err)
	}
	return d, nil
}

// DeleteDeck deletes a deck and (by cascade) its cards, their states and logs.
func (s *Store) DeleteDeck(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM decks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete deck %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete deck %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("delete deck %d: %w", id, ErrNotFound)
	}
	return nil
}
