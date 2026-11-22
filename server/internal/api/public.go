package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"anki-app/server/internal/mcpserver"
	"anki-app/server/internal/oauth"
)

// The public handler (MCP spec §3.1, §7.1) serves only the Claude connector's
// paths, on its own loopback listener that Tailscale Funnel points at. The web
// UI and /api don't exist here.

// PublicCSP is the policy for every public response except the consent page,
// which sets its own (MCP spec §7.3).
const PublicCSP = "default-src 'none'; frame-ancestors 'none'"

const (
	mcpBodyBytes   = 1 << 20
	oauthBodyBytes = 64 << 10
)

// NewPublic returns the public handler. It uses d.Store, d.Clock, d.Logger,
// d.Version and d.Config (Passphrase, MCPPublicURL).
func NewPublic(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = discardLogger()
	}
	o := oauth.New(oauth.Deps{Store: d.Store, Clock: d.Clock, Passphrase: d.Config.Passphrase,
		PublicURL: d.Config.MCPPublicURL, Logger: d.Logger})
	verify := func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		grant, err := o.Authenticate(ctx, token)
		if errors.Is(err, oauth.ErrInvalidToken) {
			return nil, fmt.Errorf("%w: unknown or expired token", auth.ErrInvalidToken)
		}
		if err != nil {
			return nil, err
		}
		// Expiry is checked by Authenticate against the app's clock.
		return &auth.TokenInfo{Extra: map[string]any{mcpserver.GrantKey: grant}}, nil
	}
	bearer := auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL:    o.ResourceMetadataURL(),
		AllowMissingExpiration: true,
	})

	mux := http.NewServeMux()
	o.Routes(mux)
	mux.Handle("/mcp", bearer(mcpserver.New(mcpserver.Deps{Store: d.Store, Clock: d.Clock, Logger: d.Logger, Version: d.Version})))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})

	host := d.Config.MCPHost()
	var h http.Handler = mux
	h = rejectBrowserHeaders(h)
	h = publicBodyCap(h)
	h = publicHost(host, h)
	h = publicHeaders(h)
	h = recoverMiddleware(d.Logger, h)
	return h
}

func publicHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", PublicCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Strict-Transport-Security", "max-age=31536000")
		next.ServeHTTP(w, r)
	})
}

// publicHost accepts only the host in MCP_PUBLIC_URL (any port).
func publicHost(host string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Host
		if h, _, err := net.SplitHostPort(got); err == nil {
			got = h
		}
		if host == "" || strings.ToLower(strings.TrimSuffix(got, ".")) != host {
			writeError(w, http.StatusForbidden, "forbidden_host", "host not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func publicBodyCap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(oauthBodyBytes)
		if r.URL.Path == "/mcp" {
			limit = mcpBodyBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// serverToServer are the routes only Claude's servers call. Browsers always
// send Origin or Sec-Fetch-Site on these requests and Claude's servers don't,
// so either header means a browser and gets a 403 (MCP spec §7.1, exception 1).
var serverToServer = map[string]bool{"/mcp": true, "/oauth/register": true, "/oauth/token": true, "/oauth/revoke": true}

func rejectBrowserHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverToServer[r.URL.Path] && (r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "") {
			writeError(w, http.StatusForbidden, "forbidden_origin", "browser requests are not accepted here")
			return
		}
		next.ServeHTTP(w, r)
	})
}
