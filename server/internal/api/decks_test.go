package api

import (
	"testing"
)

type countsJSON struct {
	New      int `json:"new"`
	Learning int `json:"learning"`
	Review   int `json:"review"`
	NewToday int `json:"newToday"`
}

type deckJSON struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	NewPerDay int        `json:"newPerDay"`
	TwoSided  bool       `json:"twoSided"`
	Counts    countsJSON `json:"counts"`
}

func TestDeckEndpoints(t *testing.T) {
	s := newServer(t)
	w := s.do("POST", "/api/decks", `{"name":"  Cpp "}`)
	expectStatus(t, w, 201)
	d := decode[deckJSON](t, w)
	if d.ID <= 0 || d.Name != "Cpp" || d.NewPerDay != 20 || d.TwoSided || d.Counts != (countsJSON{}) {
		t.Fatalf("created %+v", d)
	}
	expectError(t, s.do("POST", "/api/decks", `{"name":"Cpp"}`), 409, "conflict")
	expectError(t, s.do("POST", "/api/decks", `{"name":"   "}`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/decks", `{}`), 400, "invalid_input")

	w = s.do("GET", "/api/decks", "")
	expectStatus(t, w, 200)
	list := decode[[]deckJSON](t, w)
	if len(list) != 1 || list[0] != d {
		t.Fatalf("list = %+v", list)
	}

	w = s.do("GET", "/api/decks/1", "")
	expectStatus(t, w, 200)
	if got := decode[deckJSON](t, w); got != d {
		t.Fatalf("get = %+v", got)
	}

	w = s.do("PATCH", "/api/decks/1", `{"newPerDay":5}`)
	expectStatus(t, w, 200)
	if got := decode[deckJSON](t, w); got.NewPerDay != 5 || got.Name != "Cpp" {
		t.Fatalf("patched %+v", got)
	}
	w = s.do("PATCH", "/api/decks/1", `{"name":" C++ "}`)
	expectStatus(t, w, 200)
	if got := decode[deckJSON](t, w); got.Name != "C++" || got.NewPerDay != 5 {
		t.Fatalf("renamed %+v", got)
	}
	expectError(t, s.do("PATCH", "/api/decks/1", `{"newPerDay":10000}`), 400, "invalid_input")
	expectError(t, s.do("PATCH", "/api/decks/1", `{"name":""}`), 400, "invalid_input")
	expectError(t, s.do("PATCH", "/api/decks/9", `{"newPerDay":1}`), 404, "not_found")

	expectStatus(t, s.do("DELETE", "/api/decks/1", ""), 204)
	expectError(t, s.do("GET", "/api/decks/1", ""), 404, "not_found")
	expectError(t, s.do("DELETE", "/api/decks/1", ""), 404, "not_found")
	expectError(t, s.do("GET", "/api/decks/abc", ""), 400, "invalid_input")
	expectError(t, s.do("GET", "/api/decks/0", ""), 400, "invalid_input")
	expectError(t, s.do("GET", "/api/decks/-1", ""), 400, "invalid_input")
}

func TestDeckTwoSidedEndpoints(t *testing.T) {
	s := newServer(t)
	w := s.do("POST", "/api/decks", `{"name":"Cpp","twoSided":true}`)
	expectStatus(t, w, 201)
	if d := decode[deckJSON](t, w); !d.TwoSided {
		t.Fatalf("created %+v", d)
	}
	expectError(t, s.do("POST", "/api/decks", `{"name":"x","twoSided":"yes"}`), 400, "invalid_input")

	expectStatus(t, s.do("PATCH", "/api/decks/1", `{"twoSided":false}`), 200)
	expectStatus(t, s.do("POST", "/api/decks/1/cards", cardBody("Q", "A")), 201)
	if c := decode[cardJSON](t, s.do("GET", "/api/cards/1", "")); c.TwoSided {
		t.Fatalf("card in a one-sided deck = %+v", c)
	}
	w = s.do("PATCH", "/api/decks/1", `{"twoSided":true}`)
	expectStatus(t, w, 200)
	if d := decode[deckJSON](t, w); !d.TwoSided || d.Counts.New != 2 {
		t.Fatalf("after turning on = %+v", d)
	}
	if c := decode[cardJSON](t, s.do("GET", "/api/cards/1", "")); !c.TwoSided {
		t.Fatalf("card after deck on = %+v", c)
	}
	expectError(t, s.do("PATCH", "/api/decks/1", `{"removeReverses":true}`), 400, "invalid_input")
	expectError(t, s.do("PATCH", "/api/decks/1", `{"twoSided":true,"removeReverses":true}`), 400, "invalid_input")
	w = s.do("PATCH", "/api/decks/1", `{"twoSided":false,"removeReverses":true}`)
	expectStatus(t, w, 200)
	if d := decode[deckJSON](t, w); d.TwoSided || d.Counts.New != 1 {
		t.Fatalf("after removing reverses = %+v", d)
	}
}
