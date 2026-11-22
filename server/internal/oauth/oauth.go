// Package oauth is the Claude connector's authorization server: OAuth 2.1
// with PKCE, dynamic client registration and refresh-token rotation
// (spec docs/specs/2026-11-08-mcp-connector-design.md §4). The single
// sign-in step is the app's passphrase. It knows nothing about MCP.
//
// The Host check, the rule against browser headers on server-to-server
// routes, and the body caps are applied by the caller (api.NewPublic).
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/ratelimit"
	"anki-app/server/internal/store"
)

// AllowedRedirectURIs are the only redirect URIs a client may register.
var AllowedRedirectURIs = []string{
	"https://claude.ai/api/mcp/auth_callback",
	"https://claude.com/api/mcp/auth_callback",
}

// ErrInvalidToken is returned by Authenticate for any token that isn't a
// live access token.
var ErrInvalidToken = errors.New("invalid token")

const (
	maxClients    = 20
	codeTTL       = 60 * time.Second
	accessTTL     = time.Hour
	refreshTTL    = 30 * 24 * time.Hour
	regPerHour    = 10
	authPerMinute = 5
)

// Deps are the server's collaborators.
type Deps struct {
	Store      *store.Store
	Clock      clock.Clock
	Passphrase string
	PublicURL  string // https://host[:port], no trailing slash
	Logger     *slog.Logger
}

// Server is the authorization server.
type Server struct {
	Deps
	regLimit  *ratelimit.Window
	authLimit *ratelimit.Window // separate from the web login's limiter
}

// New returns a Server.
func New(d Deps) *Server {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	return &Server{
		Deps:      d,
		regLimit:  &ratelimit.Window{Limit: regPerHour, Period: time.Hour},
		authLimit: &ratelimit.Window{Limit: authPerMinute, Period: time.Minute},
	}
}

// ResourceURL is the protected resource (the MCP endpoint).
func (s *Server) ResourceURL() string { return s.PublicURL + "/mcp" }

// ResourceMetadataURL is where the protected-resource metadata lives (RFC 9728).
func (s *Server) ResourceMetadataURL() string {
	return s.PublicURL + "/.well-known/oauth-protected-resource/mcp"
}

// Routes registers the OAuth and metadata endpoints.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.resourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.serverMetadata)
	mux.HandleFunc("POST /oauth/register", s.register)
	mux.HandleFunc("GET /oauth/authorize", s.authorizeGet)
	mux.HandleFunc("POST /oauth/authorize", s.authorizePost)
	mux.HandleFunc("GET /oauth/authorize.css", serveCSS)
	mux.HandleFunc("POST /oauth/token", s.token)
	mux.HandleFunc("POST /oauth/revoke", s.revoke)
}

// Authenticate returns the grant of a live access token.
func (s *Server) Authenticate(ctx context.Context, token string) (int64, error) {
	now := s.Clock.Now()
	id, err := s.Store.AccessGrant(ctx, hashToken(token), now)
	if errors.Is(err, store.ErrNotFound) {
		return 0, ErrInvalidToken
	}
	if err != nil {
		return 0, err
	}
	if err := s.Store.TouchGrant(ctx, id, now); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.ResourceURL(),
		"authorization_servers":    []string{s.PublicURL},
		"bearer_methods_supported": []string{"header"},
	})
}

func (s *Server) serverMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                     s.PublicURL,
		"authorization_endpoint":                     s.PublicURL + "/oauth/authorize",
		"token_endpoint":                             s.PublicURL + "/oauth/token",
		"registration_endpoint":                      s.PublicURL + "/oauth/register",
		"revocation_endpoint":                        s.PublicURL + "/oauth/revoke",
		"response_types_supported":                   []string{"code"},
		"grant_types_supported":                      []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":           []string{"S256"},
		"token_endpoint_auth_methods_supported":      []string{"none"},
		"revocation_endpoint_auth_methods_supported": []string{"none"},
	})
}

// newToken returns 256 random bits, base64url (43 characters).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken is what the store keeps instead of a code or token.
func hashToken(t string) []byte {
	sum := sha256.Sum256([]byte(t))
	return sum[:]
}

func allowedRedirect(uri string) bool { return slices.Contains(AllowedRedirectURIs, uri) }

// redirectOrigin returns "https://host" of an allowed redirect URI.
func redirectOrigin(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeOAuthError writes an RFC 6749 error body.
func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.Logger.Error("oauth request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
	writeOAuthError(w, http.StatusInternalServerError, "server_error", "internal error")
}
