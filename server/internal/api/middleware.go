package api

import (
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// CSP is the exact Content-Security-Policy (spec §7.4). Never loosen it.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

func recoverMiddleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Any("panic", v))
				writeError(w, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", CSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if s.Config.Passphrase != "" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed strips any port, lower-cases, and checks the allowlist.
func (s *server) hostAllowed(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host != "" && slices.Contains(s.Config.AllowedHosts, host)
}

// hostMiddleware rejects any Host not in ALLOWED_HOSTS (defeats DNS rebinding).
func (s *server) hostMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			writeError(w, http.StatusForbidden, "forbidden_host", "host not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isMutation(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func isAPIMutation(r *http.Request) bool {
	return isMutation(r) && strings.HasPrefix(r.URL.Path, "/api/")
}

// contentTypeMiddleware requires application/json on API mutations (415 otherwise).
func contentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIMutation(r) {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originMiddleware blocks cross-site API mutations: Sec-Fetch-Site must be
// same-origin or none; without it, Origin (if any) must be an allowed host.
func (s *server) originMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIMutation(r) && !s.originAllowed(r) {
			writeError(w, http.StatusForbidden, "forbidden_origin", "cross-site request rejected")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) originAllowed(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return s.hostAllowed(u.Host)
}
