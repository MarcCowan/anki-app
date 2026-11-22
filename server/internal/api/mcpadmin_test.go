package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"anki-app/server/internal/config"
	"anki-app/server/internal/store"
)

type mcpStatusBody struct {
	Enabled      bool   `json:"enabled"`
	ConnectorURL string `json:"connectorUrl"`
	Grants       []struct {
		ID         int64 `json:"id"`
		CreatedAt  int64 `json:"createdAt"`
		LastUsedAt int64 `json:"lastUsedAt"`
	} `json:"grants"`
	Batches []struct {
		ID        int64  `json:"id"`
		DeckID    int64  `json:"deckId"`
		DeckName  string `json:"deckName"`
		CreatedAt int64  `json:"createdAt"`
		CardCount int    `json:"cardCount"`
		Remaining int    `json:"remaining"`
	} `json:"batches"`
}

func seedConnector(t *testing.T, st *store.Store) (deckID, batchID int64) {
	t.Helper()
	ctx := context.Background()
	c := store.OAuthClient{ID: "c1", RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"}, CreatedAt: t0}
	if err := st.CreateClient(ctx, c, 20); err != nil {
		t.Fatal(err)
	}
	g, err := st.CreateGrant(ctx, "c1", t0,
		store.NewToken{Hash: make([]byte, 32), ExpiresAt: t0.Add(time.Hour)},
		store.NewToken{Hash: append(make([]byte, 31), 1), ExpiresAt: t0.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.CreateDeck(ctx, "Go", false, t0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.AddBatch(ctx, g, d.ID, []store.NewCard{{Front: "a", Back: "b"}, {Front: "c", Back: "d"}}, t0.Add(time.Minute), 200)
	if err != nil {
		t.Fatal(err)
	}
	return d.ID, b.ID
}

func TestMCPStatusOff(t *testing.T) {
	s := newServer(t)
	w := s.do("GET", "/api/mcp", "")
	expectStatus(t, w, 200)
	got := decode[mcpStatusBody](t, w)
	if got.Enabled || got.ConnectorURL != "" || got.Grants == nil || got.Batches == nil || len(got.Grants)+len(got.Batches) != 0 {
		t.Errorf("status = %+v (want enabled=false and empty, non-null lists)", got)
	}
}

func TestMCPStatusOn(t *testing.T) {
	s := newServer(t, func(c *config.Config) { c.MCPPublicURL = "https://anki.test:8443" })
	deckID, batchID := seedConnector(t, s.store)
	got := decode[mcpStatusBody](t, s.do("GET", "/api/mcp", ""))
	if !got.Enabled || got.ConnectorURL != "https://anki.test:8443/mcp" {
		t.Errorf("status = %+v", got)
	}
	if len(got.Grants) != 1 || got.Grants[0].CreatedAt != t0.UnixMilli() || got.Grants[0].LastUsedAt != t0.UnixMilli() {
		t.Errorf("grants = %+v", got.Grants)
	}
	b := got.Batches
	if len(b) != 1 || b[0].ID != batchID || b[0].DeckID != deckID || b[0].DeckName != "Go" ||
		b[0].CreatedAt != t0.Add(time.Minute).UnixMilli() || b[0].CardCount != 2 || b[0].Remaining != 2 {
		t.Errorf("batches = %+v", b)
	}
}

func TestDeleteMCPBatch(t *testing.T) {
	s := newServer(t)
	deckID, batchID := seedConnector(t, s.store)
	path := fmt.Sprintf("/api/mcp/batches/%d", batchID)
	expectStatus(t, s.do("DELETE", path, ""), 204)
	if cs, _ := s.store.ListCards(context.Background(), deckID); len(cs) != 0 {
		t.Errorf("cards left: %d", len(cs))
	}
	expectError(t, s.do("DELETE", path, ""), 404, "not_found")
	expectError(t, s.do("DELETE", "/api/mcp/batches/x", ""), 400, "invalid_input")
}

func TestDisconnectMCP(t *testing.T) {
	s := newServer(t)
	seedConnector(t, s.store)
	expectStatus(t, s.do("DELETE", "/api/mcp/grants", ""), 204)
	if _, err := s.store.AccessGrant(context.Background(), make([]byte, 32), t0); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("access token survived disconnect: %v", err)
	}
	if gs, _ := s.store.ListGrants(context.Background()); len(gs) != 0 {
		t.Errorf("grants = %v", gs)
	}
}

func TestMCPAdminNeedsSession(t *testing.T) {
	s := newServer(t, withPassphrase)
	expectError(t, s.do("GET", "/api/mcp", ""), 401, "unauthorized")
	expectError(t, s.do("DELETE", "/api/mcp/batches/1", ""), 401, "unauthorized")
	expectError(t, s.do("DELETE", "/api/mcp/grants", ""), 401, "unauthorized")
}
