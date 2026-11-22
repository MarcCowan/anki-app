package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"mime"
	"net/http"

	"anki-app/server/internal/store"
)

// parseForm requires an application/x-www-form-urlencoded body (the format
// RFC 6749 and RFC 7009 define) and parses it.
func parseForm(w http.ResponseWriter, r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Content-Type must be application/x-www-form-urlencoded")
		return false
	}
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeOAuthError(w, http.StatusRequestEntityTooLarge, "invalid_request", "request body is too large")
			return false
		}
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return false
	}
	return true
}

// token is the token endpoint (spec §4.4).
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.exchangeCode(w, r)
	case "refresh_token":
		s.refresh(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (s *Server) exchangeCode(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	if res := f.Get("resource"); res != "" && res != s.ResourceURL() {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "unknown resource")
		return
	}
	// The code is consumed first, so it can only ever be used once, even when
	// the checks below fail.
	code, err := s.Store.TakeCode(r.Context(), hashToken(f.Get("code")))
	if errors.Is(err, store.ErrNotFound) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown or used code")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	now := s.Clock.Now()
	verifier := f.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	switch {
	case !now.Before(code.ExpiresAt),
		f.Get("client_id") != code.ClientID,
		f.Get("redirect_uri") != code.RedirectURI,
		!validPKCEString(verifier),
		subtle.ConstantTimeCompare([]byte(computed), []byte(code.CodeChallenge)) != 1:
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the code is expired or doesn't match this request")
		return
	}
	access, refresh, at, rt, err := s.newPair()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err := s.Store.DeleteExpiredOAuth(r.Context(), now); err != nil {
		s.internal(w, r, err)
		return
	}
	id, err := s.Store.CreateGrant(r.Context(), code.ClientID, now, at, rt)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.Logger.Info("oauth grant created", slog.Int64("grant", id))
	writeTokens(w, access, refresh)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	if res := f.Get("resource"); res != "" && res != s.ResourceURL() {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "unknown resource")
		return
	}
	access, refresh, at, rt, err := s.newPair()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	now := s.Clock.Now()
	id, err := s.Store.RotateRefresh(r.Context(), hashToken(f.Get("refresh_token")), f.Get("client_id"), now, at, rt)
	switch {
	case errors.Is(err, store.ErrTokenReused):
		s.Logger.Warn("oauth refresh token reused; grant revoked")
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token already used; the connection was revoked")
		return
	case errors.Is(err, store.ErrNotFound):
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown or expired refresh token")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	if err := s.Store.DeleteExpiredOAuth(r.Context(), now); err != nil {
		s.internal(w, r, err)
		return
	}
	s.Logger.Info("oauth tokens refreshed", slog.Int64("grant", id))
	writeTokens(w, access, refresh)
}

// newPair returns a new access and refresh token and the hashes to store.
func (s *Server) newPair() (access, refresh string, at, rt store.NewToken, err error) {
	if access, err = newToken(); err != nil {
		return
	}
	if refresh, err = newToken(); err != nil {
		return
	}
	now := s.Clock.Now()
	at = store.NewToken{Hash: hashToken(access), ExpiresAt: now.Add(accessTTL)}
	rt = store.NewToken{Hash: hashToken(refresh), ExpiresAt: now.Add(refreshTTL)}
	return
}

func writeTokens(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessTTL.Seconds()),
		"refresh_token": refresh,
	})
}

// revoke is RFC 7009: revoking either token ends its whole grant. Unknown
// tokens get 200 too.
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	if err := s.Store.RevokeToken(r.Context(), hashToken(r.PostForm.Get("token"))); err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
