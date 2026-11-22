package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"anki-app/server/internal/store"
)

//go:embed authorize.html
var pageHTML string

//go:embed authorize.css
var pageCSS []byte

var page = template.Must(template.New("authorize").Parse(pageHTML))

const maxStateRunes = 500

// authRequest holds authorize parameters that passed every check.
type authRequest struct {
	ClientID, RedirectURI, State, Challenge, Resource string
}

// pageData feeds authorize.html.
type pageData struct {
	Invalid bool // the request can't be trusted to redirect: show an error only
	Label   string
	Error   string
	Req     authRequest
}

// checkAuthRequest validates authorize parameters (spec §4.3). It returns
// fatal=true when the client or redirect URI is bad (never redirect then),
// or a non-empty OAuth error code to send back to the redirect URI.
func (s *Server) checkAuthRequest(ctx context.Context, v url.Values) (req authRequest, fatal bool, errCode string, err error) {
	req = authRequest{
		ClientID: v.Get("client_id"), RedirectURI: v.Get("redirect_uri"), State: v.Get("state"),
		Challenge: v.Get("code_challenge"), Resource: v.Get("resource"),
	}
	c, err := s.Store.GetClient(ctx, req.ClientID)
	if errors.Is(err, store.ErrNotFound) {
		return req, true, "", nil
	}
	if err != nil {
		return req, true, "", err
	}
	if !allowedRedirect(req.RedirectURI) || !contains(c.RedirectURIs, req.RedirectURI) {
		return req, true, "", nil
	}
	switch {
	case utf8.RuneCountInString(req.State) > maxStateRunes:
		req.State = ""
		return req, false, "invalid_request", nil
	case req.State == "",
		v.Get("response_type") != "code",
		!validPKCEString(req.Challenge),
		v.Get("code_challenge_method") != "S256":
		return req, false, "invalid_request", nil
	case req.Resource != "" && req.Resource != s.ResourceURL():
		return req, false, "invalid_target", nil
	}
	return req, false, "", nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}

// validPKCEString checks the RFC 7636 shape shared by code_challenge and
// code_verifier: 43–128 characters of [A-Za-z0-9-._~].
func validPKCEString(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, r := range v {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)
		if !ok {
			return false
		}
	}
	return true
}

func label(redirectURI string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "Claude"
	}
	return "Claude (" + u.Host + ")"
}

func (s *Server) authorizeGet(w http.ResponseWriter, r *http.Request) {
	req, fatal, errCode, err := s.checkAuthRequest(r.Context(), r.URL.Query())
	s.respondToCheck(w, r, req, fatal, errCode, err, func() {
		s.renderPage(w, http.StatusOK, pageData{Label: label(req.RedirectURI), Req: req})
	})
}

func (s *Server) respondToCheck(w http.ResponseWriter, r *http.Request, req authRequest, fatal bool, errCode string, err error, ok func()) {
	switch {
	case err != nil:
		s.Logger.Error("oauth authorize failed", slog.Any("error", err))
		s.renderPage(w, http.StatusInternalServerError, pageData{Invalid: true})
	case fatal:
		s.renderPage(w, http.StatusBadRequest, pageData{Invalid: true})
	case errCode != "":
		redirectTo(w, r, req.RedirectURI, url.Values{"error": {errCode}}, req.State)
	default:
		ok()
	}
}

func (s *Server) authorizePost(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		writeOAuthError(w, http.StatusForbidden, "access_denied", "cross-site request rejected")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderPage(w, http.StatusBadRequest, pageData{Invalid: true})
		return
	}
	req, fatal, errCode, err := s.checkAuthRequest(r.Context(), r.PostForm)
	s.respondToCheck(w, r, req, fatal, errCode, err, func() {
		if r.PostForm.Get("action") != "allow" {
			redirectTo(w, r, req.RedirectURI, url.Values{"error": {"access_denied"}}, req.State)
			return
		}
		now := s.Clock.Now()
		if !s.authLimit.Allow("", now) {
			s.renderPage(w, http.StatusTooManyRequests, pageData{Label: label(req.RedirectURI), Req: req,
				Error: "Too many attempts. Wait a minute and try again."})
			return
		}
		got := sha256.Sum256([]byte(r.PostForm.Get("passphrase")))
		want := sha256.Sum256([]byte(s.Passphrase))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			s.renderPage(w, http.StatusOK, pageData{Label: label(req.RedirectURI), Req: req, Error: "Wrong passphrase."})
			return
		}
		code, err := newToken()
		if err != nil {
			s.internal(w, r, err)
			return
		}
		err = s.Store.CreateCode(r.Context(), store.OAuthCode{
			Hash: hashToken(code), ClientID: req.ClientID, RedirectURI: req.RedirectURI,
			CodeChallenge: req.Challenge, ExpiresAt: now.Add(codeTTL),
		})
		if err != nil {
			s.internal(w, r, err)
			return
		}
		redirectTo(w, r, req.RedirectURI, url.Values{"code": {code}}, req.State)
	})
}

// sameOrigin requires Sec-Fetch-Site: same-origin or, when it is absent,
// an Origin equal to the public origin. A form post always carries one.
func (s *Server) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	return r.Header.Get("Origin") == s.PublicURL
}

// redirectTo sends a 302 to an allowed redirect URI with q (and state) as its
// query, built with url.Values so nothing can be injected into it.
func redirectTo(w http.ResponseWriter, r *http.Request, redirectURI string, q url.Values, state string) {
	if state != "" {
		q.Set("state", state)
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "bad redirect", http.StatusInternalServerError)
		return
	}
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// renderPage writes the consent (or error) page with its own CSP (spec §7.3).
// The extra form-action origin comes only from a redirect URI that already
// matched the allowlist.
func (s *Server) renderPage(w http.ResponseWriter, status int, d pageData) {
	formAction := "'none'"
	if !d.Invalid {
		formAction = "'self' " + redirectOrigin(d.Req.RedirectURI)
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action "+formAction+
		"; frame-ancestors 'none'; base-uri 'none'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := page.Execute(w, d); err != nil {
		s.Logger.Error("render authorize page", slog.Any("error", err))
	}
}

func serveCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(pageCSS)
}
