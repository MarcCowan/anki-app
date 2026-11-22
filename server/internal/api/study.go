package api

import (
	"net/http"

	"anki-app/server/internal/srs"
)

func (s *server) routesStudy(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/decks/{id}/next", s.next)
	mux.HandleFunc("POST /api/states/{id}/review", s.review)
	mux.HandleFunc("POST /api/states/{id}/undo", s.undo)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PATCH /api/settings", s.updateSettings)
}

var ratingKeys = map[srs.Rating]string{srs.Again: "again", srs.Hard: "hard", srs.Good: "good", srs.Easy: "easy"}

type previewDTO struct {
	Due           int64  `json:"due"`
	IntervalLabel string `json:"intervalLabel"`
}

type nextCardDTO struct {
	Done      bool                  `json:"done"`
	Card      cardDTO               `json:"card"`
	StateID   int64                 `json:"stateId"`
	Reverse   bool                  `json:"reverse"` // show the back first
	Reps      int64                 `json:"reps"`
	Previews  map[string]previewDTO `json:"previews"`
	Remaining countsDTO             `json:"remaining"`
}

type nextDoneDTO struct {
	Done      bool      `json:"done"`
	NextDueAt *int64    `json:"nextDueAt"`
	Remaining countsDTO `json:"remaining"`
}

type stateEnvelope struct {
	State stateDTO `json:"state"`
}

func (s *server) next(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Study.Next(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if res.Done {
		out := nextDoneDTO{Done: true, Remaining: toCounts(res.Remaining)}
		if res.NextDueAt != nil {
			ms := res.NextDueAt.UnixMilli()
			out.NextDueAt = &ms
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	previews := make(map[string]previewDTO, len(res.Previews))
	for rating, p := range res.Previews {
		previews[ratingKeys[rating]] = previewDTO{Due: p.Due.UnixMilli(), IntervalLabel: p.IntervalLabel}
	}
	writeJSON(w, http.StatusOK, nextCardDTO{
		Card:      toCardDTO(res.Card),
		StateID:   res.State.ID,
		Reverse:   res.State.Reverse,
		Reps:      res.State.Sched.Reps,
		Previews:  previews,
		Remaining: toCounts(res.Remaining),
	})
}

func (s *server) review(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Rating       *int   `json:"rating"`
		ExpectedReps *int64 `json:"expectedReps"`
		DurationMs   int64  `json:"durationMs"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Rating == nil || *in.Rating < 1 || *in.Rating > 4 {
		s.fail(w, r, invalidf("rating must be 1, 2, 3 or 4"))
		return
	}
	if in.ExpectedReps == nil {
		s.fail(w, r, invalidf("expectedReps is required"))
		return
	}
	st, err := s.Study.Answer(r.Context(), id, srs.Rating(*in.Rating), *in.ExpectedReps, in.DurationMs)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stateEnvelope{State: toStateDTO(st)})
}

func (s *server) undo(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		ExpectedReps *int64 `json:"expectedReps"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.ExpectedReps == nil {
		s.fail(w, r, invalidf("expectedReps is required"))
		return
	}
	st, err := s.Study.Undo(r.Context(), id, *in.ExpectedReps)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stateEnvelope{State: toStateDTO(st)})
}
