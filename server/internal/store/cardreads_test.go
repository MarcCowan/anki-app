package store

import (
	"context"
	"errors"
	"testing"
)

func mustCardFB(t *testing.T, s *Store, deckID int64, front, back string) Card {
	t.Helper()
	c, err := s.CreateCard(context.Background(), deckID, NewCard{Front: front, Back: back}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDeckCardCounts(t *testing.T) {
	s := openTest(t)
	b := mustDeck(t, s, "beta")
	a := mustDeck(t, s, "Alpha")
	mustCardFB(t, s, b.ID, "x", "y")
	mustCardFB(t, s, b.ID, "x", "y")
	got, err := s.DeckCardCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []DeckCount{{ID: a.ID, Name: "Alpha", CardCount: 0}, {ID: b.ID, Name: "beta", CardCount: 2}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("counts = %+v", got)
	}
}

func TestListFrontsPaging(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	other := mustDeck(t, s, "o")
	c1 := mustCardFB(t, s, d.ID, "one", "1")
	mustCardFB(t, s, other.ID, "elsewhere", "x")
	c2 := mustCardFB(t, s, d.ID, "two", "2")
	c3 := mustCardFB(t, s, d.ID, "three", "3")
	page, err := s.ListFronts(ctx, d.ID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0] != (Front{c1.ID, "one"}) || page[1] != (Front{c2.ID, "two"}) {
		t.Errorf("page 1 = %+v", page)
	}
	page, _ = s.ListFronts(ctx, d.ID, c2.ID, 2)
	if len(page) != 1 || page[0].ID != c3.ID {
		t.Errorf("page 2 = %+v", page)
	}
	if _, err := s.ListFronts(ctx, 999, 0, 2); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown deck err = %v", err)
	}
}

func ids(cs []Card) []int64 {
	out := make([]int64, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearchCardsEscapesLike(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	pct := mustCardFB(t, s, d.ID, "100%", "x")
	und := mustCardFB(t, s, d.ID, "a_b", "x")
	bsl := mustCardFB(t, s, d.ID, `x\y`, "x")
	abc := mustCardFB(t, s, d.ID, "q", "abc")
	cases := map[string][]int64{
		"%":   {pct.ID},
		"_":   {und.ID},
		`\`:   {bsl.ID},
		"ABC": {abc.ID},
		"0%":  {pct.ID},
	}
	for q, want := range cases {
		got, err := s.SearchCards(ctx, q, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if !sameIDs(ids(got), want) {
			t.Errorf("query %q matched %v, want %v", q, ids(got), want)
		}
	}
}

func TestSearchCardsDeckFilterAndLimit(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d := mustDeck(t, s, "d")
	o := mustDeck(t, s, "o")
	a := mustCardFB(t, s, d.ID, "std::move", "x")
	b := mustCardFB(t, s, o.ID, "std::move too", "x")
	c := mustCardFB(t, s, d.ID, "y", "uses std::move")
	got, _ := s.SearchCards(ctx, "std::move", d.ID, 50)
	if !sameIDs(ids(got), []int64{a.ID, c.ID}) {
		t.Errorf("deck filter: %v", ids(got))
	}
	got, _ = s.SearchCards(ctx, "std::move", 0, 2)
	if !sameIDs(ids(got), []int64{a.ID, b.ID}) {
		t.Errorf("limit: %v", ids(got))
	}
}

func TestGetCardsMixed(t *testing.T) {
	s := openTest(t)
	d := mustDeck(t, s, "d")
	c1 := mustCardFB(t, s, d.ID, "a", "b")
	mustCardFB(t, s, d.ID, "c", "d")
	c3 := mustCardFB(t, s, d.ID, "e", "f")
	got, err := s.GetCards(context.Background(), []int64{c3.ID, 999, c1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(ids(got), []int64{c1.ID, c3.ID}) || got[1].Back != "f" {
		t.Errorf("cards = %+v", got)
	}
}
