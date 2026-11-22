// Package api is the HTTP layer: middleware, routing and thin JSON handlers
// (decode → validate → call → encode).
package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/config"
	"anki-app/server/internal/ratelimit"
	"anki-app/server/internal/store"
	"anki-app/server/internal/study"
	"anki-app/server/internal/validate"
)

// Deps are the handler's collaborators.
type Deps struct {
	Store  *store.Store
	Study  *study.Service
	Clock  clock.Clock
	Config config.Config
	Static http.Handler // SPA handler; nil → 404 for non-API paths
	Logger *slog.Logger

	Version    string                          // build version shown by /api/health
	LastBackup func() (time.Time, bool, error) // newest daily backup; nil means none
}

type server struct {
	Deps
	limiter *ratelimit.Window // login: loginLimit attempts per loginWindow in total (single user, so not per IP)
}

// New returns the full HTTP handler with all middleware applied.
func New(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = discardLogger()
	}
	s := &server{Deps: d, limiter: &ratelimit.Window{Limit: loginLimit, Period: loginWindow}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/decks", s.listDecks)
	mux.HandleFunc("POST /api/decks", s.createDeck)
	mux.HandleFunc("GET /api/decks/{id}", s.getDeck)
	mux.HandleFunc("PATCH /api/decks/{id}", s.updateDeck)
	mux.HandleFunc("DELETE /api/decks/{id}", s.deleteDeck)
	mux.HandleFunc("GET /api/decks/{id}/cards", s.listCards)
	mux.HandleFunc("POST /api/decks/{id}/cards", s.createCard)
	mux.HandleFunc("GET /api/cards/{id}", s.getCard)
	mux.HandleFunc("PATCH /api/cards/{id}", s.updateCard)
	mux.HandleFunc("DELETE /api/cards/{id}", s.deleteCard)
	s.routesStudy(mux)
	s.routesMCPAdmin(mux)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/auth", s.authStatus)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	static := d.Static
	if static == nil {
		static = http.NotFoundHandler()
	}
	mux.Handle("/", static)

	var h http.Handler = mux
	h = s.authMiddleware(h)
	h = s.originMiddleware(h)
	h = contentTypeMiddleware(h)
	h = s.hostMiddleware(h)
	h = s.securityHeaders(h)
	h = recoverMiddleware(d.Logger, h)
	return h
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// pathID parses the {id} path value as a positive integer.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, invalidf("id must be a positive integer")
	}
	return id, nil
}

func invalidf(msg string) error {
	return &invalidErr{msg: msg}
}

type invalidErr struct{ msg string }

func (e *invalidErr) Error() string { return "invalid: " + e.msg }
func (e *invalidErr) Unwrap() error { return validate.ErrInvalid }
