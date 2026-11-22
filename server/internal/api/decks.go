package api

import (
	"fmt"
	"net/http"

	"anki-app/server/internal/store"
	"anki-app/server/internal/validate"
)

func (s *server) deckWithCounts(r *http.Request, d store.Deck) (deckDTO, error) {
	c, err := s.Study.Counts(r.Context(), d.ID)
	if err != nil {
		return deckDTO{}, err
	}
	return toDeckDTO(d, c), nil
}

func (s *server) listDecks(w http.ResponseWriter, r *http.Request) {
	decks, err := s.Store.ListDecks(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]deckDTO, 0, len(decks))
	for _, d := range decks {
		dto, err := s.deckWithCounts(r, d)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createDeck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     *string `json:"name"`
		TwoSided bool    `json:"twoSided"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Name == nil {
		s.fail(w, r, invalidf("name is required"))
		return
	}
	name, err := validate.DeckName(*in.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.Store.CreateDeck(r.Context(), name, in.TwoSided, s.Clock.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	dto, err := s.deckWithCounts(r, d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto)
}

func (s *server) getDeck(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.Store.GetDeck(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	dto, err := s.deckWithCounts(r, d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *server) updateDeck(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Name           *string `json:"name"`
		NewPerDay      *int    `json:"newPerDay"`
		TwoSided       *bool   `json:"twoSided"`
		RemoveReverses bool    `json:"removeReverses"` // only with twoSided: false
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Name != nil {
		name, err := validate.DeckName(*in.Name)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		in.Name = &name
	}
	if in.NewPerDay != nil {
		if err := validate.NewPerDay(*in.NewPerDay); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.RemoveReverses && (in.TwoSided == nil || *in.TwoSided) {
		s.fail(w, r, invalidf("removeReverses needs twoSided: false"))
		return
	}
	p := store.DeckPatch{Name: in.Name, NewPerDay: in.NewPerDay, TwoSided: in.TwoSided, RemoveReverses: in.RemoveReverses}
	d, err := s.Store.UpdateDeck(r.Context(), id, p, s.Clock.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	dto, err := s.deckWithCounts(r, d)
	if err != nil {
		s.fail(w, r, fmt.Errorf("update deck: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *server) deleteDeck(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Store.DeleteDeck(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
