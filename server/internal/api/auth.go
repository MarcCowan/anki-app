package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"time"
)

// Passphrase mode (spec §7.3). Off unless config.Passphrase is set.

const (
	sessionCookieName = "session"
	sessionTTL        = 30 * 24 * time.Hour
	loginLimit        = 5
	loginWindow       = time.Minute
)

func (s *server) authEnabled() bool { return s.Config.Passphrase != "" }

func hashToken(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}

// sessionHash returns the SHA-256 of the session cookie's token, if present and well-formed.
func sessionHash(r *http.Request) ([]byte, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	return hashToken(raw), true
}

func (s *server) authenticated(r *http.Request) (bool, error) {
	h, ok := sessionHash(r)
	if !ok {
		return false, nil
	}
	return s.Store.SessionValid(r.Context(), h, s.Clock.Now())
}

// authMiddleware requires a valid session for every /api/ route except
// GET /api/auth, GET /api/health (version and a timestamp only) and
// POST /api/login. Static SPA paths hold no data and stay public.
func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authEnabled() || !strings.HasPrefix(r.URL.Path, "/api/") ||
			(r.Method == http.MethodGet && (r.URL.Path == "/api/auth" || r.URL.Path == "/api/health")) ||
			(r.Method == http.MethodPost && r.URL.Path == "/api/login") {
			next.ServeHTTP(w, r)
			return
		}
		ok, err := s.authenticated(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "log in first")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) authStatus(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Enabled       bool `json:"enabled"`
		Authenticated bool `json:"authenticated"`
	}{Enabled: s.authEnabled(), Authenticated: true}
	if out.Enabled {
		ok, err := s.authenticated(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out.Authenticated = ok
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		writeError(w, http.StatusNotFound, "not_found", "passphrase mode is off")
		return
	}
	now := s.Clock.Now()
	if !s.limiter.Allow("", now) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; wait a minute")
		return
	}
	var in struct {
		Passphrase string `json:"passphrase"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	got := sha256.Sum256([]byte(in.Passphrase))
	want := sha256.Sum256([]byte(s.Config.Passphrase))
	if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		writeError(w, http.StatusUnauthorized, "bad_passphrase", "wrong passphrase")
		return
	}
	if err := s.Store.DeleteExpiredSessions(r.Context(), now); err != nil {
		s.fail(w, r, err)
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Store.CreateSession(r.Context(), hashToken(raw), now, now.Add(sessionTTL)); err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		writeError(w, http.StatusNotFound, "not_found", "passphrase mode is off")
		return
	}
	if h, ok := sessionHash(r); ok {
		if err := s.Store.DeleteSession(r.Context(), h); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}
