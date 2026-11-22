package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/config"
	"anki-app/server/internal/store"
	"anki-app/server/internal/study"
)

var t0 = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

type testServer struct {
	t     *testing.T
	h     http.Handler
	store *store.Store
	clock *clock.Fake
}

func newServer(t *testing.T, mutate ...func(*config.Config)) *testServer {
	t.Helper()
	return newServerWith(t, nil, mutate...)
}

// newServerWith is newServer with a LastBackup provider.
func newServerWith(t *testing.T, last func() (time.Time, bool, error), mutate ...func(*config.Config)) *testServer {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := &clock.Fake{T: t0}
	cfg := config.Config{AllowedHosts: []string{"localhost"}, Location: time.UTC}
	for _, m := range mutate {
		m(&cfg)
	}
	h := New(Deps{
		Store:  st,
		Study:  &study.Service{Store: st, Clock: clk, Loc: cfg.Location},
		Clock:  clk,
		Config: cfg,

		Version:    "abc123",
		LastBackup: last,
	})
	return &testServer{t: t, h: h, store: st, clock: clk}
}

// do sends a request with Host localhost, JSON content type and
// Sec-Fetch-Site same-origin, unless hdr overrides them ("" deletes a header).
func (s *testServer) do(method, path, body string, hdr ...map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "localhost"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, h := range hdr {
		for k, v := range h {
			if k == "Host" {
				r.Host = v
				continue
			}
			if v == "" {
				r.Header.Del(k)
			} else {
				r.Header.Set(k, v)
			}
		}
	}
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return v
}

type errBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func expectStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d; body %s", w.Code, want, w.Body.String())
	}
}

func expectError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	expectStatus(t, w, status)
	if got := decode[errBody](t, w).Error.Code; got != code {
		t.Fatalf("error code = %q, want %q; body %s", got, code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
}
