package api

import (
	"testing"
	"time"
)

type previewJSON struct {
	Due           int64  `json:"due"`
	IntervalLabel string `json:"intervalLabel"`
}

type nextJSON struct {
	Done      bool                   `json:"done"`
	Card      *cardJSON              `json:"card"`
	StateID   int64                  `json:"stateId"`
	Reverse   bool                   `json:"reverse"`
	Reps      int64                  `json:"reps"`
	Previews  map[string]previewJSON `json:"previews"`
	Remaining countsJSON             `json:"remaining"`
	NextDueAt *int64                 `json:"nextDueAt"`
}

type stateJSON struct {
	ID      int64 `json:"id"`
	CardID  int64 `json:"cardId"`
	Reverse bool  `json:"reverse"`
	State   int   `json:"state"`
	Due     int64 `json:"due"`
	Reps    int64 `json:"reps"`
}

func seedCard(t *testing.T, s *testServer) {
	t.Helper()
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"Cpp"}`), 201)
	expectStatus(t, s.do("POST", "/api/decks/1/cards", cardBody("Q", "A")), 201)
}

func TestNextAndReview(t *testing.T) {
	s := newServer(t)
	seedCard(t, s)
	w := s.do("GET", "/api/decks/1/next", "")
	expectStatus(t, w, 200)
	n := decode[nextJSON](t, w)
	if n.Done || n.Card == nil || n.Card.ID != 1 || n.StateID != 1 || n.Reverse || n.Reps != 0 || n.Remaining != (countsJSON{New: 1}) {
		t.Fatalf("next = %+v", n)
	}
	for _, k := range []string{"again", "hard", "good", "easy"} {
		p, ok := n.Previews[k]
		if !ok || p.IntervalLabel == "" || p.Due < t0.UnixMilli() {
			t.Fatalf("preview %s = %+v (present %v)", k, p, ok)
		}
	}
	if len(n.Previews) != 4 {
		t.Fatalf("previews = %v", n.Previews)
	}

	w = s.do("POST", "/api/states/1/review", `{"rating":3,"expectedReps":0,"durationMs":1200}`)
	expectStatus(t, w, 200)
	got := decode[struct {
		State stateJSON `json:"state"`
	}](t, w)
	if got.State != (stateJSON{ID: 1, CardID: 1, Reverse: false, State: got.State.State, Due: got.State.Due, Reps: 1}) || got.State.State == 0 {
		t.Fatalf("reviewed state = %+v", got.State)
	}
	expectError(t, s.do("POST", "/api/states/1/review", `{"rating":3,"expectedReps":0,"durationMs":1200}`), 409, "stale")
}

func TestReviewValidation(t *testing.T) {
	s := newServer(t)
	seedCard(t, s)
	expectError(t, s.do("POST", "/api/states/1/review", `{"rating":0,"expectedReps":0,"durationMs":0}`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/states/1/review", `{"rating":5,"expectedReps":0,"durationMs":0}`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/states/1/review", `{"expectedReps":0,"durationMs":0}`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/states/1/review", `{"rating":3,"durationMs":0}`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/states/1/undo", `{}`), 400, "invalid_input")
	// durationMs is optional and clamped server-side.
	expectStatus(t, s.do("POST", "/api/states/1/review", `{"rating":3,"expectedReps":0,"durationMs":-50}`), 200)
}

func TestReviewDeletedCard(t *testing.T) {
	s := newServer(t)
	seedCard(t, s)
	expectStatus(t, s.do("DELETE", "/api/cards/1", ""), 204)
	expectError(t, s.do("POST", "/api/states/1/review", `{"rating":3,"expectedReps":0,"durationMs":0}`), 404, "not_found")
	expectError(t, s.do("POST", "/api/states/1/undo", `{"expectedReps":1}`), 404, "not_found")
}

func TestUndoEndpoint(t *testing.T) {
	s := newServer(t)
	seedCard(t, s)
	expectStatus(t, s.do("POST", "/api/states/1/review", `{"rating":3,"expectedReps":0,"durationMs":0}`), 200)
	expectError(t, s.do("POST", "/api/states/1/undo", `{"expectedReps":0}`), 409, "stale")
	w := s.do("POST", "/api/states/1/undo", `{"expectedReps":1}`)
	expectStatus(t, w, 200)
	got := decode[struct {
		State stateJSON `json:"state"`
	}](t, w)
	if got.State.Reps != 0 || got.State.State != 0 {
		t.Fatalf("after undo = %+v", got.State)
	}
	expectError(t, s.do("POST", "/api/states/1/undo", `{"expectedReps":0}`), 404, "not_found")
}

func TestNextDone(t *testing.T) {
	s := newServer(t)
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"Cpp"}`), 201)
	w := s.do("GET", "/api/decks/1/next", "")
	expectStatus(t, w, 200)
	raw := decode[map[string]any](t, w)
	if raw["done"] != true {
		t.Fatalf("next = %v", raw)
	}
	if v, ok := raw["nextDueAt"]; !ok || v != nil {
		t.Fatalf("nextDueAt = %v (present %v)", v, ok)
	}
	expectError(t, s.do("GET", "/api/decks/9/next", ""), 404, "not_found")

	// After answering Easy the deck is done, with the card's due time reported.
	expectStatus(t, s.do("POST", "/api/decks/1/cards", cardBody("Q", "A")), 201)
	expectStatus(t, s.do("POST", "/api/states/1/review", `{"rating":4,"expectedReps":0,"durationMs":0}`), 200)
	n := decode[nextJSON](t, s.do("GET", "/api/decks/1/next", ""))
	if !n.Done || n.NextDueAt == nil || *n.NextDueAt <= t0.Add(24*time.Hour).UnixMilli() {
		t.Fatalf("next after easy = %+v", n)
	}
}

func TestSettingsEndpoints(t *testing.T) {
	s := newServer(t)
	type settingsJSON struct {
		DesiredRetention float64 `json:"desiredRetention"`
		DayRolloverHour  int     `json:"dayRolloverHour"`
	}
	w := s.do("GET", "/api/settings", "")
	expectStatus(t, w, 200)
	if got := decode[settingsJSON](t, w); got != (settingsJSON{0.9, 4}) {
		t.Fatalf("defaults = %+v", got)
	}
	w = s.do("PATCH", "/api/settings", `{"desiredRetention":0.85}`)
	expectStatus(t, w, 200)
	if got := decode[settingsJSON](t, w); got != (settingsJSON{0.85, 4}) {
		t.Fatalf("patched = %+v", got)
	}
	w = s.do("PATCH", "/api/settings", `{"dayRolloverHour":0}`)
	expectStatus(t, w, 200)
	if got := decode[settingsJSON](t, w); got != (settingsJSON{0.85, 0}) {
		t.Fatalf("patched hour = %+v", got)
	}
	expectError(t, s.do("PATCH", "/api/settings", `{"desiredRetention":0.5}`), 400, "invalid_input")
	expectError(t, s.do("PATCH", "/api/settings", `{"dayRolloverHour":24}`), 400, "invalid_input")
	if got := decode[settingsJSON](t, s.do("GET", "/api/settings", "")); got != (settingsJSON{0.85, 0}) {
		t.Fatalf("after rejected patches = %+v", got)
	}
}

func TestNextReverseAndOldRoutesGone(t *testing.T) {
	s := newServer(t)
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"Cpp","twoSided":true}`), 201)
	expectStatus(t, s.do("POST", "/api/decks/1/cards", cardBody("Q", "A")), 201)
	n := decode[nextJSON](t, s.do("GET", "/api/decks/1/next", ""))
	if n.Done || n.Reverse || n.Remaining.New != 2 {
		t.Fatalf("next = %+v", n)
	}
	expectStatus(t, s.do("POST", "/api/states/1/review", `{"rating":4,"expectedReps":0,"durationMs":0}`), 200)
	// The reverse (state 2) is buried today, but answering it directly still works:
	// burying only shapes the queue.
	n = decode[nextJSON](t, s.do("GET", "/api/decks/1/next", ""))
	if !n.Done || n.Remaining.NewToday != 1 {
		t.Fatalf("next after forward = %+v", n)
	}
	w := s.do("POST", "/api/states/2/review", `{"rating":3,"expectedReps":0,"durationMs":0}`)
	expectStatus(t, w, 200)
	if got := decode[struct {
		State stateJSON `json:"state"`
	}](t, w); !got.State.Reverse || got.State.CardID != 1 {
		t.Fatalf("reverse state = %+v", got.State)
	}
	expectError(t, s.do("POST", "/api/cards/1/review", `{"rating":3,"expectedReps":0,"durationMs":0}`), 404, "not_found")
}
