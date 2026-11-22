// Package mcpserver is the Claude connector's MCP endpoint: five read or
// add-only tools over the official SDK's stateless Streamable HTTP handler
// (spec docs/specs/2026-11-08-mcp-connector-design.md §5). It expects the
// caller to have authenticated the request and put the grant id in the
// SDK's auth.TokenInfo (see api.NewPublic).
package mcpserver

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"anki-app/server/internal/clock"
	"anki-app/server/internal/ratelimit"
	"anki-app/server/internal/store"
)

// GrantKey is the auth.TokenInfo.Extra key holding the grant id (int64).
const GrantKey = "grant"

const (
	requestsPerMinute = 60
	maxBodyBytes      = 1 << 20
)

// Deps are the server's collaborators.
type Deps struct {
	Store   *store.Store
	Clock   clock.Clock
	Logger  *slog.Logger
	Version string
}

// New returns the /mcp handler: a per-grant request limit in front of the
// SDK's handler.
func New(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	t := &tools{Deps: d}
	srv := mcp.NewServer(&mcp.Implementation{Name: "anki-app", Version: d.Version}, nil)
	t.register(srv)
	sdk := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// Funnel traffic arrives on a loopback listener with the public Host,
		// which this check would reject. api.NewPublic's Host allowlist
		// replaces it.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        maxBodyBytes,
	})
	limit := &ratelimit.Window{Limit: requestsPerMinute, Period: time.Minute}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grant, ok := grantFrom(auth.TokenInfoFromContext(r.Context()))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !limit.Allow(strconv.FormatInt(grant, 10), d.Clock.Now()) {
			http.Error(w, "too many requests; slow down", http.StatusTooManyRequests)
			return
		}
		sdk.ServeHTTP(w, r)
	})
}

func grantFrom(ti *auth.TokenInfo) (int64, bool) {
	if ti == nil {
		return 0, false
	}
	g, ok := ti.Extra[GrantKey].(int64)
	return g, ok
}
