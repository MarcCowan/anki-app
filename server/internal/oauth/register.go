package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"

	"anki-app/server/internal/store"
)

// register implements dynamic client registration (RFC 7591, §4.2). Only
// redirect_uris is used; every other field, client_name included, is ignored.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	now := s.Clock.Now()
	if !s.regLimit.Allow("", now) {
		writeOAuthError(w, http.StatusTooManyRequests, "too_many_requests", "too many registrations; try again later")
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "Content-Type must be application/json")
		return
	}
	var in struct {
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "body must be a JSON object")
		return
	}
	if len(in.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, uri := range in.RedirectURIs {
		if !allowedRedirect(uri) {
			// Not secret, and logging it makes a changed Claude callback easy to spot.
			s.Logger.Warn("oauth registration rejected redirect_uri", slog.String("redirect_uri", uri))
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri is not allowed")
			return
		}
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		s.internal(w, r, err)
		return
	}
	c := store.OAuthClient{ID: base64.RawURLEncoding.EncodeToString(idBytes), RedirectURIs: in.RedirectURIs, CreatedAt: now}
	if err := s.Store.CreateClient(r.Context(), c, maxClients); errors.Is(err, store.ErrFull) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "too many clients are registered; disconnect Claude in Settings first")
		return
	} else if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        now.Unix(),
		"redirect_uris":              c.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}
