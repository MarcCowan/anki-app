package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"anki-app/server/internal/srs"
)

// Card is a flashcard's content. Its schedules live in review_states: one
// forward state always, and a reverse state exactly when TwoSided is set
// (two-sided spec §2). Sched is the forward direction's schedule.
type Card struct {
	ID, DeckID           int64
	Front, Back          string
	TwoSided             bool
	CreatedAt, UpdatedAt time.Time
	Sched                srs.CardState
}

// NewCard is the content of a card to create. TwoSided nil takes the deck's default.
type NewCard struct {
	Front, Back string
	TwoSided    *bool
}

// CardPatch changes a card; nil fields are left as they are.
type CardPatch struct {
	Front, Back *string
	TwoSided    *bool
}

// cardSelect joins each card to its forward state (served by the
// UNIQUE(card_id, reverse) index).
const cardSelect = `SELECT c.id, c.deck_id, c.front_md, c.back_md, c.two_sided, c.created_at, c.updated_at,
	` + stateSchedCols + ` FROM cards c JOIN review_states s ON s.card_id = c.id AND s.reverse = 0`

func scanCard(row scanner) (Card, error) {
	var c Card
	var created, updated int64
	sp := newSchedScan()
	dest := append([]any{&c.ID, &c.DeckID, &c.Front, &c.Back, &c.TwoSided, &created, &updated}, sp.dest()...)
	if err := row.Scan(dest...); err != nil {
		return Card{}, err
	}
	c.CreatedAt, c.UpdatedAt = fromMs(created), fromMs(updated)
	c.Sched = sp.sched()
	return c, nil
}

// nullableMs stores the zero time as NULL.
func nullableMs(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: toMs(t), Valid: true}
}

// placeholders returns "?,?,…" with n marks. Only the count comes from the
// caller; the values are always bound as parameters.
func placeholders(n int) string {
	if n <= 0 {
		panic("store: placeholders needs at least one value")
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// Served by cards_deck (deck_id), then the forward-state join.
const qListCards = cardSelect + " WHERE c.deck_id = ? ORDER BY c.id"

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func deckExists(ctx context.Context, q queryRower, deckID int64) error {
	_, err := deckTwoSided(ctx, q, deckID)
	return err
}

// deckTwoSided returns the deck's two-sided default. ErrNotFound if no deck.
func deckTwoSided(ctx context.Context, q queryRower, deckID int64) (bool, error) {
	var two bool
	err := q.QueryRowContext(ctx, "SELECT two_sided FROM decks WHERE id = ?", deckID).Scan(&two)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("deck %d: %w", deckID, ErrNotFound)
	}
	return two, err
}

func getCardTx(ctx context.Context, q queryRower, id int64) (Card, error) {
	c, err := scanCard(q.QueryRowContext(ctx, cardSelect+" WHERE c.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	return c, err
}

// insertCardTx adds a card with a New forward state, and a New reverse state
// when it is two-sided.
func insertCardTx(ctx context.Context, tx *sql.Tx, deckID int64, c NewCard, now time.Time, batchID sql.NullInt64) (Card, error) {
	twoSided := false
	if c.TwoSided != nil {
		twoSided = *c.TwoSided
	} else {
		var err error
		if twoSided, err = deckTwoSided(ctx, tx, deckID); err != nil {
			return Card{}, err
		}
	}
	var id int64
	err := tx.QueryRowContext(ctx, `INSERT INTO cards (deck_id, front_md, back_md, created_at, updated_at, batch_id, two_sided)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		deckID, c.Front, c.Back, toMs(now), toMs(now), batchID, twoSided).Scan(&id)
	if err != nil {
		return Card{}, err
	}
	if err := insertNewStateTx(ctx, tx, id, deckID, false, now); err != nil {
		return Card{}, err
	}
	if twoSided {
		if err := insertNewStateTx(ctx, tx, id, deckID, true, now); err != nil {
			return Card{}, err
		}
	}
	return getCardTx(ctx, tx, id)
}

// CreateCard adds a New card to a deck. ErrNotFound if the deck is missing.
func (s *Store) CreateCard(ctx context.Context, deckID int64, c NewCard, now time.Time) (Card, error) {
	var out Card
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := deckExists(ctx, tx, deckID); err != nil {
			return err
		}
		var err error
		out, err = insertCardTx(ctx, tx, deckID, c, now, sql.NullInt64{})
		return err
	})
	if err != nil {
		return Card{}, fmt.Errorf("create card: %w", err)
	}
	return out, nil
}

// GetCard returns one card. ErrNotFound if it does not exist.
func (s *Store) GetCard(ctx context.Context, id int64) (Card, error) {
	c, err := getCardTx(ctx, s.db, id)
	if err != nil {
		return Card{}, fmt.Errorf("get card %d: %w", id, err)
	}
	return c, nil
}

// ListCards returns a deck's cards in creation order. ErrNotFound if the deck is missing.
func (s *Store) ListCards(ctx context.Context, deckID int64) ([]Card, error) {
	if err := deckExists(ctx, s.db, deckID); err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	return s.queryCards(ctx, "list cards", qListCards, deckID)
}

// UpdateCard changes a card's content and/or two-sided flag. Changing the
// content never touches scheduling (a content fix is not a new card).
// Turning two-sided on adds a New reverse state; turning it off deletes the
// reverse state and (by cascade) its review history.
func (s *Store) UpdateCard(ctx context.Context, id int64, p CardPatch, now time.Time) (Card, error) {
	var out Card
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var deckID int64
		err := tx.QueryRowContext(ctx, `UPDATE cards SET front_md = COALESCE(?, front_md),
			back_md = COALESCE(?, back_md), two_sided = COALESCE(?, two_sided), updated_at = ?
			WHERE id = ? RETURNING deck_id`, p.Front, p.Back, p.TwoSided, toMs(now), id).Scan(&deckID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if p.TwoSided != nil {
			if err := setReverseTx(ctx, tx, id, deckID, *p.TwoSided, now); err != nil {
				return err
			}
		}
		out, err = getCardTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return Card{}, fmt.Errorf("update card %d: %w", id, err)
	}
	return out, nil
}

// setReverseTx makes the card's reverse state exist (on) or not (off). An
// existing reverse state is kept as it is when turning on.
func setReverseTx(ctx context.Context, tx *sql.Tx, cardID, deckID int64, on bool, now time.Time) error {
	if !on {
		_, err := tx.ExecContext(ctx, "DELETE FROM review_states WHERE card_id = ? AND reverse = 1", cardID)
		return err
	}
	var exists int
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM review_states WHERE card_id = ? AND reverse = 1", cardID).Scan(&exists)
	if err != nil || exists > 0 {
		return err
	}
	return insertNewStateTx(ctx, tx, cardID, deckID, true, now)
}

// DeleteCard deletes a card and (by cascade) its review states and their logs.
func (s *Store) DeleteCard(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM cards WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete card %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete card %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("delete card %d: %w", id, ErrNotFound)
	}
	return nil
}

// ImportDeck creates a deck (one-sided by default) and all its cards (as New)
// in one transaction. ErrConflict if the name exists; nothing is written on any error.
func (s *Store) ImportDeck(ctx context.Context, name string, cards []NewCard, now time.Time) (Deck, error) {
	var d Deck
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if d, err = createDeckTx(ctx, tx, name, false, now); err != nil {
			return err
		}
		for i, c := range cards {
			if _, err := insertCardTx(ctx, tx, d.ID, c, now, sql.NullInt64{}); err != nil {
				return fmt.Errorf("card %d: %w", i+1, err)
			}
		}
		return nil
	})
	if err != nil {
		return Deck{}, fmt.Errorf("import deck: %w", err)
	}
	return d, nil
}
