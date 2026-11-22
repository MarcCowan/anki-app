package store

import (
	"context"
	"fmt"
	"strings"
)

// Card reads for the MCP tools (MCP spec §5).

// DeckCount is a deck with its total number of cards.
type DeckCount struct {
	ID        int64
	Name      string
	CardCount int
}

// Front is a card's id and front side.
type Front struct {
	ID    int64
	Front string
}

const (
	// The count subquery is served by cards_deck.
	qDeckCardCounts = `SELECT d.id, d.name, (SELECT COUNT(*) FROM cards c WHERE c.deck_id = d.id)
		FROM decks d ORDER BY d.name COLLATE NOCASE, d.id`
	// Served by cards_deck (deck_id) or the primary key (id range).
	qListFronts = "SELECT id, front_md FROM cards WHERE deck_id = ? AND id > ? ORDER BY id LIMIT ?"
	// No index can serve '%term%'. All decks: an accepted full scan (MCP spec
	// §5.3). One deck: narrowed by cards_deck.
	searchWhere      = `(c.front_md LIKE ? ESCAPE '\' OR c.back_md LIKE ? ESCAPE '\')`
	qSearchCards     = cardSelect + " WHERE " + searchWhere + " ORDER BY c.id LIMIT ?"
	qSearchDeckCards = cardSelect + " WHERE c.deck_id = ? AND " + searchWhere + " ORDER BY c.id LIMIT ?"
)

func qGetCards(n int) string {
	return cardSelect + " WHERE c.id IN (" + placeholders(n) + ") ORDER BY c.id"
}

// escapeLike makes q match literally inside a LIKE pattern with ESCAPE '\'.
func escapeLike(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}

// DeckCardCounts returns every deck with its card count, ordered by name.
func (s *Store) DeckCardCounts(ctx context.Context) ([]DeckCount, error) {
	rows, err := s.db.QueryContext(ctx, qDeckCardCounts)
	if err != nil {
		return nil, fmt.Errorf("deck card counts: %w", err)
	}
	defer rows.Close()
	out := []DeckCount{}
	for rows.Next() {
		var d DeckCount
		if err := rows.Scan(&d.ID, &d.Name, &d.CardCount); err != nil {
			return nil, fmt.Errorf("deck card counts: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("deck card counts: %w", err)
	}
	return out, nil
}

// ListFronts returns up to limit fronts of a deck with id > afterID, in id
// order. ErrNotFound if the deck is missing.
func (s *Store) ListFronts(ctx context.Context, deckID, afterID int64, limit int) ([]Front, error) {
	if err := deckExists(ctx, s.db, deckID); err != nil {
		return nil, fmt.Errorf("list fronts: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, qListFronts, deckID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list fronts: %w", err)
	}
	defer rows.Close()
	out := []Front{}
	for rows.Next() {
		var f Front
		if err := rows.Scan(&f.ID, &f.Front); err != nil {
			return nil, fmt.Errorf("list fronts: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list fronts: %w", err)
	}
	return out, nil
}

// SearchCards returns up to limit cards whose front or back contains query
// (ASCII case-insensitive, matched literally), in id order. deckID 0 searches
// every deck.
func (s *Store) SearchCards(ctx context.Context, query string, deckID int64, limit int) ([]Card, error) {
	pat := "%" + escapeLike(query) + "%"
	q, args := qSearchCards, []any{pat, pat, limit}
	if deckID != 0 {
		q, args = qSearchDeckCards, []any{deckID, pat, pat, limit}
	}
	return s.queryCards(ctx, "search cards", q, args...)
}

// GetCards returns the cards with the given ids that exist, in id order.
func (s *Store) GetCards(ctx context.Context, ids []int64) ([]Card, error) {
	if len(ids) == 0 {
		return []Card{}, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return s.queryCards(ctx, "get cards", qGetCards(len(ids)), args...)
}

func (s *Store) queryCards(ctx context.Context, op, q string, args ...any) ([]Card, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()
	out := []Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return out, nil
}
