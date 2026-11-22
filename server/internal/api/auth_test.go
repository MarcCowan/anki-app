package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"anki-app/server/internal/config"
)

const pass = "correct horse battery staple"

func withPassphrase(c *config.Config) { c.Passphrase = pass }

type authJSON struct {
	Enabled       bool `json:"enabled"`
	Authenticated bool `json:"authenticated"`
}

func loginBody(p string) string { return `{"passphrase":"` + p + `"}` }

// sessionCookie returns the session cookie set by a response.
func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatalf("no session cookie in %v", w.Header().Values("Set-Cookie"))
	return nil
}

func withCookie(c *http.Cookie) map[string]string {
	return map[string]string{"Cookie": c.Name + "=" + c.Value}
}

func TestAuthDisabled(t *testing.T) {
	s := newServer(t)
	expectStatus(t, s.do("GET", "/api/decks", ""), 200)
	w := s.do("GET", "/api/auth", "")
	expectStatus(t, w, 200)
	if got := decode[authJSON](t, w); got != (authJSON{Enabled: false, Authenticated: true}) {
		t.Fatalf("auth = %+v", got)
	}
	if h := w.Header().Get("Strict-Transport-Security"); h != "" {
		t.Fatalf("HSTS = %q", h)
	}
	expectError(t, s.do("POST", "/api/login", loginBody(pass)), 404, "not_found")
}

func TestAuthRequired(t *testing.T) {
	s := newServer(t, withPassphrase)
	expectError(t, s.do("GET", "/api/decks", ""), 401, "unauthorized")
	expectError(t, s.do("POST", "/api/decks", `{"name":"x"}`), 401, "unauthorized")
	expectError(t, s.do("GET", "/api/settings", ""), 401, "unauthorized")
	expectError(t, s.do("GET", "/api/decks", "", map[string]string{"Cookie": "session=garbage!!"}), 401, "unauthorized")
	expectError(t, s.do("GET", "/api/decks", "", map[string]string{"Cookie": "session=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32))}), 401, "unauthorized")
	w := s.do("GET", "/api/auth", "")
	expectStatus(t, w, 200)
	if got := decode[authJSON](t, w); got != (authJSON{Enabled: true, Authenticated: false}) {
		t.Fatalf("auth = %+v", got)
	}
	// Static SPA paths hold no data and stay public (here: 404 because no Static handler).
	if w := s.do("GET", "/", ""); w.Code == 401 {
		t.Fatal("static path requires auth")
	}
}

func TestLoginFlow(t *testing.T) {
	s := newServer(t, withPassphrase)
	expectError(t, s.do("POST", "/api/login", loginBody("wrong passphrase!!")), 401, "bad_passphrase")
	expectError(t, s.do("POST", "/api/login", `{}`), 401, "bad_passphrase")

	w := s.do("POST", "/api/login", loginBody(pass))
	expectStatus(t, w, 204)
	c := sessionCookie(t, w)
	raw := w.Header().Get("Set-Cookie")
	for _, attr := range []string{"HttpOnly", "Secure", "SameSite=Strict", "Path=/", "Max-Age=2592000"} {
		if !strings.Contains(raw, attr) {
			t.Errorf("Set-Cookie %q lacks %s", raw, attr)
		}
	}
	if b, err := base64.RawURLEncoding.DecodeString(c.Value); err != nil || len(b) != 32 {
		t.Fatalf("token = %q (%d bytes, %v)", c.Value, len(b), err)
	}

	expectStatus(t, s.do("GET", "/api/decks", "", withCookie(c)), 200)
	if got := decode[authJSON](t, s.do("GET", "/api/auth", "", withCookie(c))); !got.Authenticated {
		t.Fatal("not authenticated with cookie")
	}

	// Sessions last 30 days.
	s.clock.Advance(30*24*time.Hour - time.Second)
	expectStatus(t, s.do("GET", "/api/decks", "", withCookie(c)), 200)
	s.clock.Advance(2 * time.Second)
	expectError(t, s.do("GET", "/api/decks", "", withCookie(c)), 401, "unauthorized")
}

func TestLogout(t *testing.T) {
	s := newServer(t, withPassphrase)
	c := sessionCookie(t, s.do("POST", "/api/login", loginBody(pass)))
	w := s.do("POST", "/api/logout", `{}`, withCookie(c))
	expectStatus(t, w, 204)
	if out := sessionCookie(t, w); out.MaxAge >= 0 || out.Value != "" {
		t.Fatalf("logout cookie = %+v", out)
	}
	expectError(t, s.do("GET", "/api/decks", "", withCookie(c)), 401, "unauthorized")
}

func TestLoginRateLimit(t *testing.T) {
	s := newServer(t, withPassphrase)
	for range 5 {
		expectError(t, s.do("POST", "/api/login", loginBody("nope nope nope nope")), 401, "bad_passphrase")
	}
	expectError(t, s.do("POST", "/api/login", loginBody(pass)), 429, "rate_limited")
	s.clock.Advance(61 * time.Second)
	expectStatus(t, s.do("POST", "/api/login", loginBody(pass)), 204)
}

func TestLoginRateLimitCountsSuccesses(t *testing.T) {
	s := newServer(t, withPassphrase)
	for range 5 {
		expectStatus(t, s.do("POST", "/api/login", loginBody(pass)), 204)
	}
	expectError(t, s.do("POST", "/api/login", loginBody(pass)), 429, "rate_limited")
}

func TestHSTSWhenEnabled(t *testing.T) {
	s := newServer(t, withPassphrase)
	for _, path := range []string{"/api/auth", "/api/decks", "/"} {
		if h := s.do("GET", path, "").Header().Get("Strict-Transport-Security"); h != "max-age=31536000" {
			t.Fatalf("%s: HSTS = %q", path, h)
		}
	}
}

func TestCookieIsHashedAtRest(t *testing.T) {
	s := newServer(t, withPassphrase)
	c := sessionCookie(t, s.do("POST", "/api/login", loginBody(pass)))
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	ctx := context.Background()
	if ok, err := s.store.SessionValid(ctx, sum[:], s.clock.Now()); err != nil || !ok {
		t.Fatalf("hash lookup = %v, %v", ok, err)
	}
	if ok, err := s.store.SessionValid(ctx, raw, s.clock.Now()); err != nil || ok {
		t.Fatalf("raw token stored: %v, %v", ok, err)
	}
}

func TestLoginExpiredSessionsSwept(t *testing.T) {
	s := newServer(t, withPassphrase)
	c := sessionCookie(t, s.do("POST", "/api/login", loginBody(pass)))
	raw, _ := base64.RawURLEncoding.DecodeString(c.Value)
	sum := sha256.Sum256(raw)
	s.clock.Advance(31 * 24 * time.Hour)
	expectStatus(t, s.do("POST", "/api/login", loginBody(pass)), 204)
	// The first session is gone even when asked about a time it was valid.
	if ok, _ := s.store.SessionValid(context.Background(), sum[:], t0); ok {
		t.Fatal("expired session not swept on login")
	}
}
