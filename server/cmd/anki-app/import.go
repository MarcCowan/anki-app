package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"anki-app/server/internal/store"
	"anki-app/server/internal/validate"
)

// seedFile is the cleanup task's output format (spec §10).
type seedFile struct {
	Deck  *string `json:"deck"`
	Cards *[]struct {
		Front string `json:"front"`
		Back  string `json:"back"`
	} `json:"cards"`
}

// importFile creates a deck and all its cards from a seed JSON file, in one
// transaction. Any invalid card aborts the whole import.
func importFile(ctx context.Context, st *store.Store, path string, now time.Time) (store.Deck, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return store.Deck{}, 0, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var seed seedFile
	if err := dec.Decode(&seed); err != nil {
		return store.Deck{}, 0, fmt.Errorf("%s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return store.Deck{}, 0, fmt.Errorf("%s: trailing data after the JSON object", path)
	}
	if seed.Deck == nil || seed.Cards == nil {
		return store.Deck{}, 0, fmt.Errorf("%s: \"deck\" and \"cards\" are required", path)
	}
	name, err := validate.DeckName(*seed.Deck)
	if err != nil {
		return store.Deck{}, 0, err
	}
	cards := make([]store.NewCard, 0, len(*seed.Cards))
	for i, c := range *seed.Cards {
		front, err := validate.Side("front", c.Front)
		if err != nil {
			return store.Deck{}, 0, fmt.Errorf("card %d: %w", i+1, err)
		}
		back, err := validate.Side("back", c.Back)
		if err != nil {
			return store.Deck{}, 0, fmt.Errorf("card %d: %w", i+1, err)
		}
		cards = append(cards, store.NewCard{Front: front, Back: back})
	}
	d, err := st.ImportDeck(ctx, name, cards, now)
	if err != nil {
		return store.Deck{}, 0, err
	}
	return d, len(cards), nil
}
