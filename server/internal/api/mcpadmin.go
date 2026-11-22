package api

import "net/http"

// Settings → Claude connector (MCP spec §6.2). Tailnet only, behind the
// normal session auth.

const recentBatches = 20

type mcpGrantDTO struct {
	ID         int64 `json:"id"`
	CreatedAt  int64 `json:"createdAt"`
	LastUsedAt int64 `json:"lastUsedAt"`
}

type mcpBatchDTO struct {
	ID        int64  `json:"id"`
	DeckID    int64  `json:"deckId"`
	DeckName  string `json:"deckName"`
	CreatedAt int64  `json:"createdAt"`
	CardCount int    `json:"cardCount"`
	Remaining int    `json:"remaining"`
}

type mcpStatusDTO struct {
	Enabled      bool          `json:"enabled"`
	ConnectorURL string        `json:"connectorUrl"`
	Grants       []mcpGrantDTO `json:"grants"`
	Batches      []mcpBatchDTO `json:"batches"`
}

func (s *server) routesMCPAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/mcp", s.mcpStatus)
	mux.HandleFunc("DELETE /api/mcp/batches/{id}", s.deleteMCPBatch)
	mux.HandleFunc("DELETE /api/mcp/grants", s.disconnectMCP)
}

// mcpStatus lists grants and batches even when the connector is off, so
// batches added before it was turned off can still be deleted.
func (s *server) mcpStatus(w http.ResponseWriter, r *http.Request) {
	out := mcpStatusDTO{Enabled: s.Config.MCPEnabled(), Grants: []mcpGrantDTO{}, Batches: []mcpBatchDTO{}}
	if out.Enabled {
		out.ConnectorURL = s.Config.MCPPublicURL + "/mcp"
	}
	gs, err := s.Store.ListGrants(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, g := range gs {
		out.Grants = append(out.Grants, mcpGrantDTO{ID: g.ID, CreatedAt: g.CreatedAt.UnixMilli(), LastUsedAt: g.LastUsedAt.UnixMilli()})
	}
	bs, err := s.Store.ListBatches(r.Context(), recentBatches)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, b := range bs {
		out.Batches = append(out.Batches, mcpBatchDTO{ID: b.ID, DeckID: b.DeckID, DeckName: b.DeckName,
			CreatedAt: b.CreatedAt.UnixMilli(), CardCount: b.CardCount, Remaining: b.Remaining})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) deleteMCPBatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Store.DeleteBatch(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) disconnectMCP(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteAllGrants(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
