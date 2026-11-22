package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/store"
)

const (
	testPass   = "correct horse battery staple"
	testPublic = "https://anki.test:8443"
	claudeCB   = "https://claude.ai/api/mcp/auth_callback"
	verifier   = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk" // 43 characters
)

var t0 = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)

type env struct {
	t     *testing.T
	srv   *Server
	mux   *http.ServeMux
	store *store.Store
	clock *clock.Fake
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := &clock.Fake{T: t0}
	srv := New(Deps{Store: st, Clock: clk, Passphrase: testPass, PublicURL: testPublic})
	mux := http.NewServeMux()
	srv.Routes(mux)
	return &env{t: t, srv: srv, mux: mux, store: st, clock: clk}
}

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (e *env) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

func (e *env) get(path string) *httptest.ResponseRecorder {
	return e.serve(httptest.NewRequest("GET", path, nil))
}

func (e *env) postJSON(path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return e.serve(r)
}

// postForm posts a form; hdr adds headers ("" deletes one).
func (e *env) postForm(path string, form url.Values, hdr ...map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, h := range hdr {
		for k, v := range h {
			if v == "" {
				r.Header.Del(k)
			} else {
				r.Header.Set(k, v)
			}
		}
	}
	return e.serve(r)
}

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return v
}

func expectCode(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d; body %s", w.Code, want, w.Body.String())
	}
}

type oauthErr struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func expectOAuthError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	expectCode(t, w, status)
	if got := decodeJSON[oauthErr](t, w).Error; got != code {
		t.Fatalf("error = %q, want %q; body %s", got, code, w.Body.String())
	}
}

// register registers a client with the given redirect URIs and returns its id.
func (e *env) register(uris ...string) string {
	e.t.Helper()
	b, _ := json.Marshal(map[string]any{"redirect_uris": uris, "client_name": "<b>Evil</b> Corp"})
	w := e.postJSON("/oauth/register", string(b))
	expectCode(e.t, w, http.StatusCreated)
	return decodeJSON[struct {
		ClientID string `json:"client_id"`
	}](e.t, w).ClientID
}

// authParams are valid authorize parameters for a client.
func authParams(clientID string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {claudeCB},
		"state":                 {"st"},
		"code_challenge":        {challenge(verifier)},
		"code_challenge_method": {"S256"},
		"resource":              {testPublic + "/mcp"},
	}
}

var sameOrigin = map[string]string{"Sec-Fetch-Site": "same-origin"}

// approve posts the consent form with the passphrase and returns the code.
func (e *env) approve(p url.Values) string {
	e.t.Helper()
	form := url.Values{}
	for k, v := range p {
		form[k] = v
	}
	form.Set("passphrase", testPass)
	form.Set("action", "allow")
	w := e.postForm("/oauth/authorize", form, sameOrigin)
	expectCode(e.t, w, http.StatusFound)
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		e.t.Fatal(err)
	}
	return loc.Query().Get("code")
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func codeForm(clientID, code string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {claudeCB},
		"code_verifier": {verifier},
		"resource":      {testPublic + "/mcp"},
	}
}

// connect runs the whole flow and returns the client id and tokens.
func (e *env) connect() (string, tokenResp) {
	e.t.Helper()
	id := e.register(claudeCB)
	code := e.approve(authParams(id))
	w := e.postForm("/oauth/token", codeForm(id, code))
	expectCode(e.t, w, http.StatusOK)
	return id, decodeJSON[tokenResp](e.t, w)
}

func jsonPost(path, body string) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
