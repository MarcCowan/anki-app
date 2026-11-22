package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Batches group the cards added by one MCP add_cards call, so a bad batch
// can be deleted in one go (MCP spec §5.5, §6.1).

// ErrDailyLimit is wrapped by *DailyLimitError.
var ErrDailyLimit = errors.New("daily limit")

// DailyLimitError says how many cards are left in the rolling 24-hour window
// and when the oldest batch in it leaves the window.
type DailyLimitError struct {
	Remaining int
	FreesAt   time.Time
}

func (e *DailyLimitError) Error() string { return "daily limit" }
func (e *DailyLimitError) Unwrap() error { return ErrDailyLimit }

// DailyWindow is the rolling window the daily card limit counts over.
const DailyWindow = 24 * time.Hour

// Batch is the result of AddBatch.
type Batch struct {
	ID, DeckID int64
	Added      int
}

// BatchSummary is one row of the Settings batch list.
type BatchSummary struct {
	ID, DeckID           int64
	DeckName             string
	CreatedAt            time.Time
	CardCount, Remaining int
}

const (
	// Served by mcp_batches_created.
	qBatchWindow = "SELECT COALESCE(SUM(card_count), 0), MIN(created_at) FROM mcp_batches WHERE created_at > ?"
	// Served by mcp_batches_created (order) and cards_batch (remaining count).
	qListBatches = `SELECT b.id, b.deck_id, d.name, b.created_at, b.card_count,
		(SELECT COUNT(*) FROM cards c WHERE c.batch_id = b.id)
		FROM mcp_batches b JOIN decks d ON d.id = b.deck_id
		ORDER BY b.created_at DESC, b.id DESC LIMIT ?`
	// Served by cards_batch.
	qDeleteBatchCards = "DELETE FROM cards WHERE batch_id = ?"
)

// AddBatch inserts cards as one batch, in one transaction, unless that would
// take the cards added in the last DailyWindow (all clients together) over
// dailyLimit; then nothing is written and a *DailyLimitError is returned.
// grantID 0 records no grant. ErrNotFound if the deck is missing.
func (s *Store) AddBatch(ctx context.Context, grantID, deckID int64, cards []NewCard, now time.Time, dailyLimit int) (Batch, error) {
	out := Batch{DeckID: deckID, Added: len(cards)}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := deckExists(ctx, tx, deckID); err != nil {
			return err
		}
		var used int
		var oldest sql.NullInt64
		if err := tx.QueryRowContext(ctx, qBatchWindow, toMs(now.Add(-DailyWindow))).Scan(&used, &oldest); err != nil {
			return err
		}
		if used+len(cards) > dailyLimit {
			e := &DailyLimitError{Remaining: max(dailyLimit-used, 0), FreesAt: now}
			if oldest.Valid {
				e.FreesAt = fromMs(oldest.Int64).Add(DailyWindow)
			}
			return e
		}
		grant := sql.NullInt64{Int64: grantID, Valid: grantID != 0}
		if err := tx.QueryRowContext(ctx, "INSERT INTO mcp_batches (grant_id, deck_id, created_at, card_count) VALUES (?, ?, ?, ?) RETURNING id",
			grant, deckID, toMs(now), len(cards)).Scan(&out.ID); err != nil {
			return err
		}
		for i, c := range cards {
			if _, err := insertCardTx(ctx, tx, deckID, c, now, sql.NullInt64{Int64: out.ID, Valid: true}); err != nil {
				return fmt.Errorf("card %d: %w", i+1, err)
			}
		}
		return nil
	})
	if err != nil {
		return Batch{}, fmt.Errorf("add batch: %w", err)
	}
	return out, nil
}

// ListBatches returns the newest batches first, with how many of each
// batch's cards still exist.
func (s *Store) ListBatches(ctx context.Context, limit int) ([]BatchSummary, error) {
	rows, err := s.db.QueryContext(ctx, qListBatches, limit)
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	defer rows.Close()
	out := []BatchSummary{}
	for rows.Next() {
		var b BatchSummary
		var created int64
		if err := rows.Scan(&b.ID, &b.DeckID, &b.DeckName, &created, &b.CardCount, &b.Remaining); err != nil {
			return nil, fmt.Errorf("list batches: %w", err)
		}
		b.CreatedAt = fromMs(created)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	return out, nil
}

// DeleteBatch deletes a batch's cards (their review logs cascade) and the
// batch itself, which also returns its cards to the daily limit.
// ErrNotFound if the batch does not exist.
func (s *Store) DeleteBatch(ctx context.Context, id int64) error {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, qDeleteBatchCards, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM mcp_batches WHERE id = ?", id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete batch %d: %w", id, err)
	}
	return nil
}
