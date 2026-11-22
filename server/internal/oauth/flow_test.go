package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func authorizeURL(p url.Values) string { return "/oauth/authorize?" + p.Encode() }

func TestAuthorizeBadClientOrRedirectNeverRedirects(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	unknown := authParams("nope")
	wrongURI := authParams(id)
	wrongURI.Set("redirect_uri", "https://claude.com/api/mcp/auth_callback") // allowed, but not registered
	for name, p := range map[string]url.Values{"unknown client": unknown, "unregistered redirect": wrongURI} {
		w := e.get(authorizeURL(p))
		if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
			t.Errorf("%s: status %d, Location %q", name, w.Code, w.Header().Get("Location"))
		}
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: Content-Type %q", name, w.Header().Get("Content-Type"))
		}
	}
}

func TestAuthorizeBadParamsRedirectWithError(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	cases := map[string]func(url.Values){
		"response_type": func(p url.Values) { p.Set("response_type", "token") },
		"no challenge":  func(p url.Values) { p.Del("code_challenge") },
		"short challenge": func(p url.Values) {
			p.Set("code_challenge", "abc")
		},
		"plain":          func(p url.Values) { p.Set("code_challenge_method", "plain") },
		"wrong resource": func(p url.Values) { p.Set("resource", "https://evil.example/mcp") },
	}
	for name, mutate := range cases {
		p := authParams(id)
		mutate(p)
		w := e.get(authorizeURL(p))
		expectCode(t, w, http.StatusFound)
		loc, _ := url.Parse(w.Header().Get("Location"))
		if loc.Scheme+"://"+loc.Host+loc.Path != claudeCB || loc.Query().Get("error") == "" || loc.Query().Get("state") != "st" {
			t.Errorf("%s: Location %s", name, loc)
		}
	}
	// Without state there is nothing to echo, but the redirect still carries the error.
	p := authParams(id)
	p.Del("state")
	w := e.get(authorizeURL(p))
	expectCode(t, w, http.StatusFound)
	if loc, _ := url.Parse(w.Header().Get("Location")); loc.Query().Get("error") != "invalid_request" {
		t.Errorf("no state: Location %s", loc)
	}
}

func TestAuthorizeConsentPage(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	p := authParams(id)
	p.Del("resource") // optional
	w := e.get(authorizeURL(p))
	expectCode(t, w, http.StatusOK)
	body := w.Body.String()
	for _, want := range []string{"Claude (claude.ai)", "Approve only if you just clicked Connect in your own Claude chat"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "Evil") {
		t.Error("page shows the registered client_name")
	}
	wantCSP := "default-src 'none'; style-src 'self'; form-action 'self' https://claude.ai; frame-ancestors 'none'; base-uri 'none'"
	if got := w.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("CSP = %q", got)
	}
}

func TestAuthorizeConsentPageClaudeCom(t *testing.T) {
	e := newEnv(t)
	com := "https://claude.com/api/mcp/auth_callback"
	id := e.register(com)
	p := authParams(id)
	p.Set("redirect_uri", com)
	w := e.get(authorizeURL(p))
	expectCode(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Claude (claude.com)") ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "form-action 'self' https://claude.com;") {
		t.Errorf("claude.com page wrong; CSP %q", w.Header().Get("Content-Security-Policy"))
	}
}

func consentForm(p url.Values, action, pass string) url.Values {
	f := url.Values{}
	for k, v := range p {
		f[k] = v
	}
	f.Set("action", action)
	f.Set("passphrase", pass)
	return f
}

func TestAuthorizePostCrossSiteRejected(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	f := consentForm(authParams(id), "allow", testPass)
	expectCode(t, e.postForm("/oauth/authorize", f, map[string]string{"Sec-Fetch-Site": "cross-site"}), http.StatusForbidden)
	expectCode(t, e.postForm("/oauth/authorize", f, map[string]string{"Origin": "https://evil.example"}), http.StatusForbidden)
	expectCode(t, e.postForm("/oauth/authorize", f), http.StatusForbidden) // neither header
	w := e.postForm("/oauth/authorize", f, map[string]string{"Origin": testPublic})
	expectCode(t, w, http.StatusFound)
}

func TestAuthorizeDeny(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	w := e.postForm("/oauth/authorize", consentForm(authParams(id), "deny", ""), sameOrigin)
	expectCode(t, w, http.StatusFound)
	loc, _ := url.Parse(w.Header().Get("Location"))
	if loc.Query().Get("error") != "access_denied" || loc.Query().Get("code") != "" {
		t.Errorf("Location %s", loc)
	}
}

func TestAuthorizeWrongPassphraseAndLimit(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	for range 5 {
		w := e.postForm("/oauth/authorize", consentForm(authParams(id), "allow", "wrong"), sameOrigin)
		expectCode(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), "Wrong passphrase") || w.Header().Get("Location") != "" {
			t.Fatalf("wrong passphrase page: %s", w.Body.String())
		}
	}
	w := e.postForm("/oauth/authorize", consentForm(authParams(id), "allow", testPass), sameOrigin)
	expectCode(t, w, http.StatusTooManyRequests)
	e.clock.Advance(time.Minute)
	e.approve(authParams(id))
}

func TestAuthorizePostRechecksParams(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	p := authParams(id)
	p.Set("code_challenge_method", "plain")
	w := e.postForm("/oauth/authorize", consentForm(p, "allow", testPass), sameOrigin)
	expectCode(t, w, http.StatusFound)
	if loc, _ := url.Parse(w.Header().Get("Location")); loc.Query().Get("code") != "" {
		t.Errorf("issued a code for bad params: %s", loc)
	}
	bad := authParams(id)
	bad.Set("redirect_uri", "https://evil.example/cb")
	w = e.postForm("/oauth/authorize", consentForm(bad, "allow", testPass), sameOrigin)
	if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
		t.Errorf("bad redirect on POST: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestAuthorizeApproveIssuesCodeAndEchoesState(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	p := authParams(id)
	p.Set("state", "a b&c=d")
	w := e.postForm("/oauth/authorize", consentForm(p, "allow", testPass), sameOrigin)
	expectCode(t, w, http.StatusFound)
	loc, _ := url.Parse(w.Header().Get("Location"))
	if loc.Host != "claude.ai" || len(loc.Query().Get("code")) != 43 || loc.Query().Get("state") != "a b&c=d" {
		t.Errorf("Location %s", loc)
	}
}

func TestTokenHappyPath(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	code := e.approve(authParams(id))
	w := e.postForm("/oauth/token", codeForm(id, code))
	expectCode(t, w, http.StatusOK)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
	tok := decodeJSON[tokenResp](t, w)
	if tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || len(tok.AccessToken) != 43 || len(tok.RefreshToken) != 43 {
		t.Errorf("token = %+v", tok)
	}
	if _, err := e.srv.Authenticate(context.Background(), tok.AccessToken); err != nil {
		t.Errorf("access token: %v", err)
	}
	if _, err := e.srv.Authenticate(context.Background(), tok.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("refresh token accepted as access: %v", err)
	}
}

func TestTokenCodeFailures(t *testing.T) {
	cases := map[string]func(e *env, f url.Values){
		"reused": func(e *env, f url.Values) {
			expectCode(e.t, e.postForm("/oauth/token", f), http.StatusOK)
		},
		"expired":        func(e *env, f url.Values) { e.clock.Advance(61 * time.Second) },
		"wrong verifier": func(e *env, f url.Values) { f.Set("code_verifier", strings.Repeat("x", 43)) },
		"short verifier": func(e *env, f url.Values) { f.Set("code_verifier", verifier[:42]) },
		"wrong redirect": func(e *env, f url.Values) { f.Set("redirect_uri", "https://claude.com/api/mcp/auth_callback") },
		"wrong client":   func(e *env, f url.Values) { f.Set("client_id", e.register(claudeCB)) },
		"unknown code":   func(e *env, f url.Values) { f.Set("code", "nope") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			id := e.register(claudeCB)
			f := codeForm(id, e.approve(authParams(id)))
			mutate(e, f)
			expectOAuthError(t, e.postForm("/oauth/token", f), 400, "invalid_grant")
		})
	}
}

func TestTokenFailedCheckStillConsumesCode(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	f := codeForm(id, e.approve(authParams(id)))
	bad := url.Values{}
	for k, v := range f {
		bad[k] = v
	}
	bad.Set("code_verifier", strings.Repeat("x", 43))
	expectOAuthError(t, e.postForm("/oauth/token", bad), 400, "invalid_grant")
	expectOAuthError(t, e.postForm("/oauth/token", f), 400, "invalid_grant")
}

func TestTokenOtherErrors(t *testing.T) {
	e := newEnv(t)
	id := e.register(claudeCB)
	f := codeForm(id, e.approve(authParams(id)))
	f.Set("resource", "https://evil.example/mcp")
	expectOAuthError(t, e.postForm("/oauth/token", f), 400, "invalid_target")
	expectOAuthError(t, e.postForm("/oauth/token", url.Values{"grant_type": {"password"}}), 400, "unsupported_grant_type")
	w := e.serve(jsonPost("/oauth/token", `{"grant_type":"authorization_code"}`))
	expectOAuthError(t, w, 400, "invalid_request")
}

func TestTokenRefreshRotationAndReuse(t *testing.T) {
	e := newEnv(t)
	id, tok := e.connect()
	refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}, "client_id": {id}}
	w := e.postForm("/oauth/token", refresh)
	expectCode(t, w, http.StatusOK)
	next := decodeJSON[tokenResp](t, w)
	if next.AccessToken == tok.AccessToken || next.RefreshToken == tok.RefreshToken {
		t.Fatal("tokens not rotated")
	}
	expectOAuthError(t, e.postForm("/oauth/token", refresh), 400, "invalid_grant")
	if _, err := e.srv.Authenticate(context.Background(), next.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("grant survived refresh reuse: %v", err)
	}
}

func TestAccessTokenExpires(t *testing.T) {
	e := newEnv(t)
	_, tok := e.connect()
	e.clock.Advance(time.Hour)
	if _, err := e.srv.Authenticate(context.Background(), tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired access token: %v", err)
	}
}

func TestAuthorizeCSS(t *testing.T) {
	e := newEnv(t)
	w := e.get("/oauth/authorize.css")
	expectCode(t, w, 200)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}
}
