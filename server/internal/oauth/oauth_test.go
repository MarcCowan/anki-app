package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProtectedResourceMetadata(t *testing.T) {
	e := newEnv(t)
	w := e.get("/.well-known/oauth-protected-resource/mcp")
	expectCode(t, w, 200)
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	got := decodeJSON[map[string]any](t, w)
	want := map[string]any{
		"resource":                 testPublic + "/mcp",
		"authorization_servers":    []any{testPublic},
		"bearer_methods_supported": []any{"header"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("metadata = %v", got)
	}
}

func TestAuthorizationServerMetadata(t *testing.T) {
	e := newEnv(t)
	w := e.get("/.well-known/oauth-authorization-server")
	expectCode(t, w, 200)
	got := decodeJSON[map[string]any](t, w)
	want := map[string]any{
		"issuer":                                     testPublic,
		"authorization_endpoint":                     testPublic + "/oauth/authorize",
		"token_endpoint":                             testPublic + "/oauth/token",
		"registration_endpoint":                      testPublic + "/oauth/register",
		"revocation_endpoint":                        testPublic + "/oauth/revoke",
		"response_types_supported":                   []any{"code"},
		"grant_types_supported":                      []any{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":           []any{"S256"},
		"token_endpoint_auth_methods_supported":      []any{"none"},
		"revocation_endpoint_auth_methods_supported": []any{"none"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("metadata = %v", got)
	}
}

func TestRegisterAllowedURI(t *testing.T) {
	e := newEnv(t)
	w := e.postJSON("/oauth/register", `{"redirect_uris":["https://claude.com/api/mcp/auth_callback"],"client_name":"x","grant_types":["authorization_code"]}`)
	expectCode(t, w, http.StatusCreated)
	got := decodeJSON[struct {
		ClientID     string   `json:"client_id"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}](t, w)
	if len(got.ClientID) != 22 || got.AuthMethod != "none" || len(got.RedirectURIs) != 1 {
		t.Errorf("registration = %+v", got)
	}
	if _, err := e.store.GetClient(context.Background(), got.ClientID); err != nil {
		t.Errorf("client not stored: %v", err)
	}
}

func TestRegisterRejectsOtherURIs(t *testing.T) {
	e := newEnv(t)
	for _, uri := range []string{
		"https://claude.ai/api/mcp/auth_callback/x",
		"http://claude.ai/api/mcp/auth_callback",
		"https://evil.example/api/mcp/auth_callback",
		"https://claude.ai/api/mcp/auth_callback?x=1",
	} {
		body := fmt.Sprintf(`{"redirect_uris":[%q, %q]}`, claudeCB, uri)
		expectOAuthError(t, e.postJSON("/oauth/register", body), 400, "invalid_redirect_uri")
	}
	expectOAuthError(t, e.postJSON("/oauth/register", `{"redirect_uris":[]}`), 400, "invalid_redirect_uri")
	expectOAuthError(t, e.postJSON("/oauth/register", `not json`), 400, "invalid_client_metadata")
}

func TestRegisterRateLimit(t *testing.T) {
	e := newEnv(t)
	for range 10 {
		e.register(claudeCB)
	}
	w := e.postJSON("/oauth/register", fmt.Sprintf(`{"redirect_uris":[%q]}`, claudeCB))
	expectCode(t, w, http.StatusTooManyRequests)
	e.clock.Advance(time.Hour)
	e.register(claudeCB)
}

func TestRegisterFull(t *testing.T) {
	e := newEnv(t)
	for range maxClients {
		e.connect()                // each client gets a grant, so none is evictable
		e.clock.Advance(time.Hour) // stay under the registration and authorize rate limits
	}
	expectOAuthError(t, e.postJSON("/oauth/register", fmt.Sprintf(`{"redirect_uris":[%q]}`, claudeCB)), 400, "invalid_client_metadata")
}

func TestRevoke(t *testing.T) {
	e := newEnv(t)
	_, tok := e.connect()
	expectCode(t, e.postForm("/oauth/revoke", url.Values{"token": {"unknown"}}), 200)
	if _, err := e.srv.Authenticate(context.Background(), tok.AccessToken); err != nil {
		t.Fatalf("before revoke: %v", err)
	}
	expectCode(t, e.postForm("/oauth/revoke", url.Values{"token": {tok.RefreshToken}}), 200)
	if _, err := e.srv.Authenticate(context.Background(), tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("after revoke err = %v", err)
	}
}

func TestAuthenticateTouchesGrant(t *testing.T) {
	e := newEnv(t)
	_, tok := e.connect()
	e.clock.Advance(10 * time.Minute)
	id, err := e.srv.Authenticate(context.Background(), tok.AccessToken)
	if err != nil || id == 0 {
		t.Fatalf("Authenticate = %d, %v", id, err)
	}
	gs, _ := e.store.ListGrants(context.Background())
	if !gs[0].LastUsedAt.Equal(t0.Add(10 * time.Minute)) {
		t.Errorf("LastUsedAt = %v", gs[0].LastUsedAt)
	}
	if _, err := e.srv.Authenticate(context.Background(), "garbage"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("garbage err = %v", err)
	}
}

func TestResourceURLs(t *testing.T) {
	e := newEnv(t)
	if e.srv.ResourceURL() != testPublic+"/mcp" ||
		e.srv.ResourceMetadataURL() != testPublic+"/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("%s %s", e.srv.ResourceURL(), e.srv.ResourceMetadataURL())
	}
}
