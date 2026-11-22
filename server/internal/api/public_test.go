package api

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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/config"
	"anki-app/server/internal/store"
)

const (
	pubPass   = "correct horse battery staple"
	pubURL    = "https://anki.test:8443"
	pubHost   = "anki.test:8443"
	claudeCB  = "https://claude.ai/api/mcp/auth_callback"
	pkceVerif = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

func publicConfig() config.Config {
	return config.Config{AllowedHosts: []string{"localhost"}, Location: time.UTC, Passphrase: pubPass, MCPPublicURL: pubURL}
}

type publicEnv struct {
	h     http.Handler
	store *store.Store
	clock *clock.Fake
}

func newPublic(t *testing.T) *publicEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "pub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := &clock.Fake{T: t0}
	h := NewPublic(Deps{Store: st, Clock: clk, Config: publicConfig(), Version: "test"})
	return &publicEnv{h: h, store: st, clock: clk}
}

// req sends a request with the public Host; hdr overrides headers ("" deletes).
func (p *publicEnv) req(method, path, contentType, body string, hdr ...map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = pubHost
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for _, h := range hdr {
		for k, v := range h {
			if k == "Host" {
				r.Host = v
			} else if v == "" {
				r.Header.Del(k)
			} else {
				r.Header.Set(k, v)
			}
		}
	}
	w := httptest.NewRecorder()
	p.h.ServeHTTP(w, r)
	return w
}

const form = "application/x-www-form-urlencoded"

func TestPublicRejectsOtherHosts(t *testing.T) {
	p := newPublic(t)
	for _, host := range []string{"localhost", "evil.example", "anki.test.evil.example"} {
		expectStatus(t, p.req("GET", "/.well-known/oauth-authorization-server", "", "", map[string]string{"Host": host}), 403)
	}
	expectStatus(t, p.req("GET", "/.well-known/oauth-authorization-server", "", ""), 200)
}

func TestPublicUnmappedPathsAre404(t *testing.T) {
	p := newPublic(t)
	for _, path := range []string{"/", "/api/health", "/api/decks", "/index.html"} {
		expectError(t, p.req("GET", path, "", ""), 404, "not_found")
	}
	expectError(t, p.req("POST", "/api/login", "application/json", `{"passphrase":"x"}`), 404, "not_found")
}

func TestPublicRejectsBrowserHeadersOnServerRoutes(t *testing.T) {
	p := newPublic(t)
	for _, path := range []string{"/mcp", "/oauth/token", "/oauth/register", "/oauth/revoke"} {
		for _, h := range []map[string]string{{"Origin": "https://evil.example"}, {"Sec-Fetch-Site": "same-origin"}, {"Origin": pubURL}} {
			expectError(t, p.req("POST", path, "application/json", "{}", h), 403, "forbidden_origin")
		}
	}
}

func TestPublicMCPWithoutTokenIs401(t *testing.T) {
	p := newPublic(t)
	w := p.req("POST", "/mcp", "application/json", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	expectStatus(t, w, 401)
	want := `Bearer resource_metadata="https://anki.test:8443/.well-known/oauth-protected-resource/mcp"`
	if got := w.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q", got)
	}
	expectStatus(t, p.req("POST", "/mcp", "application/json", "{}", map[string]string{"Authorization": "Bearer nope"}), 401)
}

func TestPublicContentTypes(t *testing.T) {
	p := newPublic(t)
	if w := p.req("POST", "/oauth/token", "application/json", `{"grant_type":"refresh_token"}`); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "invalid_request") {
		t.Errorf("token with JSON: %d %s", w.Code, w.Body.String())
	}
	if w := p.req("POST", "/oauth/register", form, "redirect_uris="+claudeCB); w.Code != 400 {
		t.Errorf("register with a form: %d", w.Code)
	}
}

func TestPublicBodyCaps(t *testing.T) {
	p := newPublic(t)
	big := "grant_type=refresh_token&x=" + strings.Repeat("a", 65<<10)
	expectStatus(t, p.req("POST", "/oauth/token", form, big), 413)
}

func TestPublicSecurityHeaders(t *testing.T) {
	p := newPublic(t)
	w := p.req("GET", "/.well-known/oauth-authorization-server", "", "")
	for k, v := range map[string]string{
		"Content-Security-Policy":   "default-src 'none'; frame-ancestors 'none'",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "no-referrer",
		"Strict-Transport-Security": "max-age=31536000",
	} {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q", k, got)
		}
	}
}

func TestMainHandlerHasNoPublicRoutes(t *testing.T) {
	s := newServer(t, func(c *config.Config) { *c = publicConfig() })
	for _, c := range []struct{ method, path string }{
		{"POST", "/mcp"}, {"GET", "/oauth/authorize"}, {"POST", "/oauth/token"},
		{"GET", "/.well-known/oauth-authorization-server"},
	} {
		if w := s.do(c.method, c.path, ""); w.Code != 404 {
			t.Errorf("%s %s on the main handler: %d", c.method, c.path, w.Code)
		}
	}
}

// hostTransport sends every request with the public Host (and an optional
// bearer token) to a loopback test server, the way Funnel does.
type hostTransport struct{ token string }

func (h hostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = pubHost
	if h.token != "" {
		r.Header.Set("Authorization", "Bearer "+h.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestPublicMCPAcceptsPublicHostOnLoopback(t *testing.T) {
	p := newPublic(t)
	ts := httptest.NewServer(p.h)
	defer ts.Close()
	hc := &http.Client{Transport: hostTransport{}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	post := func(path, ct, body string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", ct)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}

	res := post("/oauth/register", "application/json", `{"redirect_uris":["`+claudeCB+`"]}`, nil)
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if res.StatusCode != 201 || json.NewDecoder(res.Body).Decode(&reg) != nil {
		t.Fatalf("register: %d", res.StatusCode)
	}

	sum := sha256.Sum256([]byte(pkceVerif))
	consent := url.Values{
		"response_type": {"code"}, "client_id": {reg.ClientID}, "redirect_uri": {claudeCB}, "state": {"s"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"resource": {pubURL + "/mcp"}, "passphrase": {pubPass}, "action": {"allow"},
	}
	res = post("/oauth/authorize", form, consent.Encode(), map[string]string{"Sec-Fetch-Site": "same-origin"})
	loc, _ := url.Parse(res.Header.Get("Location"))
	if res.StatusCode != 302 || loc.Query().Get("code") == "" {
		t.Fatalf("authorize: %d %s", res.StatusCode, loc)
	}

	exchange := url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
		"client_id": {reg.ClientID}, "redirect_uri": {claudeCB}, "code_verifier": {pkceVerif}}
	res = post("/oauth/token", form, exchange.Encode(), nil)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&tok) != nil {
		t.Fatalf("token: %d", res.StatusCode)
	}

	d, err := p.store.CreateDeck(context.Background(), "Go", false, t0)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	sess, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp", HTTPClient: &http.Client{Transport: hostTransport{token: tok.AccessToken}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	tools, err := sess.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil || len(tools.Tools) != 5 {
		t.Fatalf("tools/list: %v, %v", tools, err)
	}
	r, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "add_cards", Arguments: map[string]any{
		"deckId": d.ID, "cards": []map[string]string{{"front": "Q", "back": "A"}},
	}})
	if err != nil || r.IsError {
		t.Fatalf("add_cards: %+v, %v", r, err)
	}
	bs, _ := p.store.ListBatches(context.Background(), 20)
	gs, _ := p.store.ListGrants(context.Background())
	if len(bs) != 1 || len(gs) != 1 {
		t.Fatalf("batches %v, grants %v", bs, gs)
	}
}
