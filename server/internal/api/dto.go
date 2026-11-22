package api

import (
	"anki-app/server/internal/store"
	"anki-app/server/internal/study"
)

// JSON shapes (spec §6, two-sided spec §4): camelCase, times as UTC Unix milliseconds.

type countsDTO struct {
	New      int `json:"new"`
	Learning int `json:"learning"`
	Review   int `json:"review"`
	NewToday int `json:"newToday"`
}

type deckDTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	NewPerDay int       `json:"newPerDay"`
	TwoSided  bool      `json:"twoSided"`
	Counts    countsDTO `json:"counts"`
}

// cardDTO's state, due and reps are the forward direction's.
type cardDTO struct {
	ID        int64  `json:"id"`
	DeckID    int64  `json:"deckId"`
	Front     string `json:"front"`
	Back      string `json:"back"`
	State     int    `json:"state"`
	Due       int64  `json:"due"`
	Reps      int64  `json:"reps"`
	TwoSided  bool   `json:"twoSided"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type stateDTO struct {
	ID      int64 `json:"id"`
	CardID  int64 `json:"cardId"`
	Reverse bool  `json:"reverse"`
	State   int   `json:"state"`
	Due     int64 `json:"due"`
	Reps    int64 `json:"reps"`
}

func toCounts(c study.Counts) countsDTO {
	return countsDTO{New: c.New, Learning: c.Learning, Review: c.Review, NewToday: c.NewToday}
}

func toDeckDTO(d store.Deck, c study.Counts) deckDTO {
	return deckDTO{ID: d.ID, Name: d.Name, NewPerDay: d.NewPerDay, TwoSided: d.TwoSided, Counts: toCounts(c)}
}

func toCardDTO(c store.Card) cardDTO {
	return cardDTO{
		ID:        c.ID,
		DeckID:    c.DeckID,
		Front:     c.Front,
		Back:      c.Back,
		State:     int(c.Sched.State),
		Due:       c.Sched.Due.UnixMilli(),
		Reps:      c.Sched.Reps,
		TwoSided:  c.TwoSided,
		CreatedAt: c.CreatedAt.UnixMilli(),
		UpdatedAt: c.UpdatedAt.UnixMilli(),
	}
}

func toStateDTO(r store.ReviewState) stateDTO {
	return stateDTO{
		ID:      r.ID,
		CardID:  r.CardID,
		Reverse: r.Reverse,
		State:   int(r.Sched.State),
		Due:     r.Sched.Due.UnixMilli(),
		Reps:    r.Sched.Reps,
	}
}
