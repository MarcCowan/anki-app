package api

import (
	"net/http"

	"anki-app/server/internal/store"
	"anki-app/server/internal/validate"
)

func (s *server) listCards(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cards, err := s.Store.ListCards(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]cardDTO, 0, len(cards))
	for _, c := range cards {
		out = append(out, toCardDTO(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// optionalSide validates a side if present.
func optionalSide(field string, v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s, err := validate.Side(field, *v)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *server) createCard(w http.ResponseWriter, r *http.Request) {
	deckID, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Front    *string `json:"front"`
		Back     *string `json:"back"`
		TwoSided *bool   `json:"twoSided"` // omitted: the deck's default
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Front == nil || in.Back == nil {
		s.fail(w, r, invalidf("front and back are required"))
		return
	}
	front, err := validate.Side("front", *in.Front)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	back, err := validate.Side("back", *in.Back)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Store.CreateCard(r.Context(), deckID, store.NewCard{Front: front, Back: back, TwoSided: in.TwoSided}, s.Clock.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCardDTO(c))
}

func (s *server) getCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Store.GetCard(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toCardDTO(c))
}

func (s *server) updateCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Front    *string `json:"front"`
		Back     *string `json:"back"`
		TwoSided *bool   `json:"twoSided"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	front, err := optionalSide("front", in.Front)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	back, err := optionalSide("back", in.Back)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := store.CardPatch{Front: front, Back: back, TwoSided: in.TwoSided}
	c, err := s.Store.UpdateCard(r.Context(), id, p, s.Clock.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toCardDTO(c))
}

func (s *server) deleteCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Store.DeleteCard(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
