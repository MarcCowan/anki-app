package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type cardJSON struct {
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

func cardBody(front, back string) string {
	b, _ := json.Marshal(map[string]string{"front": front, "back": back})
	return string(b)
}

func TestCardEndpoints(t *testing.T) {
	s := newServer(t)
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"Cpp"}`), 201)

	w := s.do("POST", "/api/decks/1/cards", cardBody("\r\n  Q\r\n", "A  "))
	expectStatus(t, w, 201)
	c := decode[cardJSON](t, w)
	if c.ID <= 0 || c.DeckID != 1 || c.Front != "  Q" || c.Back != "A" || c.State != 0 || c.Reps != 0 ||
		c.Due != t0.UnixMilli() || c.CreatedAt != t0.UnixMilli() || c.UpdatedAt != t0.UnixMilli() {
		t.Fatalf("created %+v", c)
	}
	expectError(t, s.do("POST", "/api/decks/9/cards", cardBody("Q", "A")), 404, "not_found")

	w = s.do("GET", "/api/decks/1/cards", "")
	expectStatus(t, w, 200)
	if list := decode[[]cardJSON](t, w); len(list) != 1 || list[0] != c {
		t.Fatalf("list = %+v", list)
	}
	expectError(t, s.do("GET", "/api/decks/9/cards", ""), 404, "not_found")

	w = s.do("GET", "/api/cards/1", "")
	expectStatus(t, w, 200)
	if got := decode[cardJSON](t, w); got != c {
		t.Fatalf("get = %+v", got)
	}

	s.clock.Advance(time.Minute)
	w = s.do("PATCH", "/api/cards/1", `{"front":"Q2"}`)
	expectStatus(t, w, 200)
	if got := decode[cardJSON](t, w); got.Front != "Q2" || got.Back != "A" || got.UpdatedAt != t0.Add(time.Minute).UnixMilli() {
		t.Fatalf("patched %+v", got)
	}

	ok6000 := strings.Repeat("→", 6000)
	expectStatus(t, s.do("PATCH", "/api/cards/1", cardBody(ok6000, ok6000)), 200)
	expectStatus(t, s.do("POST", "/api/decks/1/cards", cardBody(ok6000, "A")), 201)
	expectError(t, s.do("PATCH", "/api/cards/1", `{"back":"`+strings.Repeat("→", 6001)+`"}`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/decks/1/cards", cardBody("Q", strings.Repeat("→", 6001))), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/decks/1/cards", cardBody("Q", "")), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/decks/1/cards", `{"front":"Q"}`), 400, "invalid_input")
	expectError(t, s.do("PATCH", "/api/cards/1", `{"front":" \n "}`), 400, "invalid_input")
	w = s.do("POST", "/api/decks/1/cards", cardBody("", "A"))
	if msg := decode[errBody](t, w).Error.Message; !strings.Contains(msg, "front") {
		t.Fatalf("message %q should name the field", msg)
	}

	expectStatus(t, s.do("DELETE", "/api/cards/1", ""), 204)
	expectError(t, s.do("GET", "/api/cards/1", ""), 404, "not_found")
	expectError(t, s.do("PATCH", "/api/cards/1", `{"front":"x"}`), 404, "not_found")
	expectError(t, s.do("DELETE", "/api/cards/1", ""), 404, "not_found")
}

func TestCardTwoSidedEndpoints(t *testing.T) {
	s := newServer(t)
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"Cpp","twoSided":true}`), 201)
	w := s.do("POST", "/api/decks/1/cards", cardBody("Q", "A"))
	expectStatus(t, w, 201)
	if c := decode[cardJSON](t, w); !c.TwoSided {
		t.Fatalf("default = %+v", c)
	}
	w = s.do("POST", "/api/decks/1/cards", `{"front":"Q","back":"A","twoSided":false}`)
	expectStatus(t, w, 201)
	if c := decode[cardJSON](t, w); c.TwoSided {
		t.Fatalf("override = %+v", c)
	}
	w = s.do("PATCH", "/api/cards/1", `{"twoSided":false}`)
	expectStatus(t, w, 200)
	if c := decode[cardJSON](t, w); c.TwoSided || c.Front != "Q" {
		t.Fatalf("turned off = %+v", c)
	}
	expectError(t, s.do("PATCH", "/api/cards/1", `{"twoSided":1}`), 400, "invalid_input")
}
