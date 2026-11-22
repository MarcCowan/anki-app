package api

import (
	"net/http"
	"strings"
	"testing"
)

const wantCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

func TestSecurityHeaders(t *testing.T) {
	s := newServer(t)
	for _, w := range []interface{ Header() http.Header }{
		s.do("GET", "/api/decks", ""),
		s.do("GET", "/api/decks", "", map[string]string{"Host": "evil.example"}), // also on rejections
		s.do("GET", "/", ""),
	} {
		h := w.Header()
		if got := h.Get("Content-Security-Policy"); got != wantCSP {
			t.Errorf("CSP = %q", got)
		}
		if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("nosniff = %q", got)
		}
		if got := h.Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("Referrer-Policy = %q", got)
		}
		if got := h.Get("Strict-Transport-Security"); got != "" {
			t.Errorf("HSTS set without passphrase mode: %q", got)
		}
	}
}

func TestHostRejected(t *testing.T) {
	s := newServer(t)
	expectError(t, s.do("GET", "/api/decks", "", map[string]string{"Host": "evil.example"}), 403, "forbidden_host")
	expectError(t, s.do("GET", "/", "", map[string]string{"Host": "evil.example:8080"}), 403, "forbidden_host")
	expectStatus(t, s.do("GET", "/api/decks", "", map[string]string{"Host": "LOCALHOST:8080"}), 200)
}

func TestCrossSiteRejected(t *testing.T) {
	s := newServer(t)
	expectError(t, s.do("POST", "/api/decks", `{"name":"a"}`, map[string]string{"Sec-Fetch-Site": "cross-site"}), 403, "forbidden_origin")
	expectError(t, s.do("POST", "/api/decks", `{"name":"b"}`, map[string]string{"Sec-Fetch-Site": "same-site"}), 403, "forbidden_origin")
	expectError(t, s.do("POST", "/api/decks", `{"name":"c"}`, map[string]string{"Sec-Fetch-Site": "", "Origin": "http://evil.example"}), 403, "forbidden_origin")
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"d"}`, map[string]string{"Sec-Fetch-Site": "", "Origin": "http://localhost:5173"}), 201)
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"e"}`, map[string]string{"Sec-Fetch-Site": ""}), 201)
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"f"}`, map[string]string{"Sec-Fetch-Site": "none"}), 201)
	// Reads are not blocked by the fetch-metadata check.
	expectStatus(t, s.do("GET", "/api/decks", "", map[string]string{"Sec-Fetch-Site": "cross-site"}), 200)
}

func TestMutationNeedsJSON(t *testing.T) {
	s := newServer(t)
	expectError(t, s.do("POST", "/api/decks", `{"name":"x"}`, map[string]string{"Content-Type": "text/plain"}), 415, "unsupported_media_type")
	expectError(t, s.do("POST", "/api/decks", `{"name":"x"}`, map[string]string{"Content-Type": ""}), 415, "unsupported_media_type")
	expectError(t, s.do("DELETE", "/api/decks/1", "", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}), 415, "unsupported_media_type")
	expectStatus(t, s.do("POST", "/api/decks", `{"name":"x"}`, map[string]string{"Content-Type": "application/json; charset=utf-8"}), 201)
}

func TestBodyTooLarge(t *testing.T) {
	s := newServer(t)
	body := `{"name":"` + strings.Repeat("a", 1<<20) + `"}`
	expectError(t, s.do("POST", "/api/decks", body), 413, "too_large")
}

func TestUnknownField(t *testing.T) {
	s := newServer(t)
	expectError(t, s.do("POST", "/api/decks", `{"name":"x","extra":1}`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/decks", `{"name":"x"} {"name":"y"}`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/decks", `not json`), 400, "invalid_input")
	expectError(t, s.do("POST", "/api/decks", `{"name":5}`), 400, "invalid_input")
}

func TestUnknownAPIPath(t *testing.T) {
	s := newServer(t)
	expectError(t, s.do("GET", "/api/nope", ""), 404, "not_found")
	expectError(t, s.do("GET", "/api/decks/1/nope", ""), 404, "not_found")
}

func TestInternalErrorHidden(t *testing.T) {
	s := newServer(t)
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	w := s.do("GET", "/api/decks", "")
	expectError(t, w, 500, "internal")
	if got := decode[errBody](t, w).Error.Message; got != "internal error" {
		t.Fatalf("message = %q", got)
	}
}

func TestPanicRecovered(t *testing.T) {
	h := recoverMiddleware(discardLogger(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	s := newServer(t)
	s.h = h
	expectError(t, s.do("GET", "/api/decks", ""), 500, "internal")
}
