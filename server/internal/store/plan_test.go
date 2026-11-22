package store

import (
	"strings"
	"testing"
)

// explain returns the joined detail column of EXPLAIN QUERY PLAN.
func explain(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(parts, " | ")
}

func usesIndex(plan, index string) bool {
	return strings.Contains(plan, "USING INDEX "+index) || strings.Contains(plan, "USING COVERING INDEX "+index)
}

func TestQueuePlansUseIndex(t *testing.T) {
	s := openTest(t)
	two := []any{int64(1), 1, 3, int64(0), int64(0)} // deck, two states, time, bury cut-off
	queries := map[string]struct {
		q    string
		args []any
	}{
		"FirstDue":   {qFirstDue(2), two},
		"FirstNew":   {qFirstNew, []any{int64(1), int64(0)}},
		"CountDue":   {qCountDue(2), two},
		"CountState": {qCountState, []any{int64(1), 0, int64(0)}},
		"MinDue":     {qMinDue(2), []any{int64(1), 1, 3, int64(0)}},
	}
	for name, c := range queries {
		plan := explain(t, s, c.q, c.args...)
		if !usesIndex(plan, "review_states_deck_state_due") {
			t.Errorf("%s plan does not use review_states_deck_state_due: %s", name, plan)
		}
		// The burying check: the sibling by (card_id, reverse), its logs by (state, time).
		if !usesIndex(plan, "sqlite_autoindex_review_states_1") || !usesIndex(plan, "review_logs_state_time") {
			t.Errorf("%s burying check does not use the sibling and log indexes: %s", name, plan)
		}
	}
}

func TestCardPlans(t *testing.T) {
	s := openTest(t)
	for name, c := range map[string]struct {
		q     string
		args  []any
		index string
	}{
		"ListCards":            {qListCards, []any{int64(1)}, "cards_deck"},
		"CardStates":           {qCardStates, []any{int64(1)}, "sqlite_autoindex_review_states_1"},
		"ListCardsForward":     {qListCards, []any{int64(1)}, "sqlite_autoindex_review_states_1"},
		"AddDeckReverses":      {qAddDeckReverses, append(schedArgsNew(t0), int64(1)), "cards_deck"},
		"AddDeckReversesCheck": {qAddDeckReverses, append(schedArgsNew(t0), int64(1)), "sqlite_autoindex_review_states_1"},
		"DeleteDeckReverses":   {qDeleteDeckReverses, []any{int64(1)}, "review_states_deck_state_due"},
		"SetDeckCardsTwoSided": {qSetDeckCardsTwoSided, []any{true, int64(1)}, "cards_deck"},
	} {
		if plan := explain(t, s, c.q, c.args...); !usesIndex(plan, c.index) {
			t.Errorf("%s plan does not use %s: %s", name, c.index, plan)
		}
	}
	// Cascades from a card delete must not scan: the child lookups SQLite runs.
	if plan := explain(t, s, "SELECT 1 FROM review_states WHERE card_id = ?", int64(1)); !usesIndex(plan, "sqlite_autoindex_review_states_1") {
		t.Errorf("card cascade to review_states scans: %s", plan)
	}
	if plan := explain(t, s, "SELECT 1 FROM review_logs WHERE review_state_id = ?", int64(1)); !usesIndex(plan, "review_logs_state_time") {
		t.Errorf("state cascade to review_logs scans: %s", plan)
	}
}

func TestNewTodayPlanUsesLogIndex(t *testing.T) {
	s := openTest(t)
	plan := explain(t, s, qCountNewIntroducedSince, int64(1), int64(0))
	if !usesIndex(plan, "review_states_deck_state_due") || !usesIndex(plan, "review_logs_state_time") {
		t.Fatalf("plan does not use review_states_deck_state_due and review_logs_state_time: %s", plan)
	}
}

func TestUndoPlanUsesLogIndex(t *testing.T) {
	s := openTest(t)
	plan := explain(t, s, qLatestLog, int64(1))
	if !usesIndex(plan, "review_logs_state_time") {
		t.Fatalf("plan does not use review_logs_state_time: %s", plan)
	}
}

func TestOAuthPlans(t *testing.T) {
	s := openTest(t)
	for name, c := range map[string]struct {
		q     string
		args  []any
		index string
	}{
		"TokenByHash": {qTokenByHash, []any{[]byte{1}}, "sqlite_autoindex_oauth_tokens_1"},
		"AccessGrant": {qAccessGrant, []any{[]byte{1}, int64(0)}, "sqlite_autoindex_oauth_tokens_1"},
		"Evictable":   {qEvictableClient, nil, "oauth_grants_client"},
	} {
		if plan := explain(t, s, c.q, c.args...); !usesIndex(plan, c.index) {
			t.Errorf("%s plan does not use %s: %s", name, c.index, plan)
		}
	}
	// The grant delete's cascade to oauth_tokens must not scan: check the
	// child lookup SQLite runs for the foreign key.
	if plan := explain(t, s, "SELECT 1 FROM oauth_tokens WHERE grant_id = ?", int64(1)); !usesIndex(plan, "oauth_tokens_grant") {
		t.Errorf("grant cascade lookup does not use oauth_tokens_grant: %s", plan)
	}
}

func TestBatchAndCardReadPlans(t *testing.T) {
	s := openTest(t)
	for name, c := range map[string]struct {
		q     string
		args  []any
		index string
	}{
		"BatchWindow":      {qBatchWindow, []any{int64(0)}, "mcp_batches_created"},
		"ListBatches":      {qListBatches, []any{20}, "mcp_batches_created"},
		"ListBatchesCount": {qListBatches, []any{20}, "cards_batch"},
		"DeleteBatchCards": {qDeleteBatchCards, []any{int64(1)}, "cards_batch"},
		"DeckCardCounts":   {qDeckCardCounts, nil, "cards_deck"},
		"ListFronts":       {qListFronts, []any{int64(1), int64(0), 10}, "cards_deck"},
		"SearchDeckCards":  {qSearchDeckCards, []any{int64(1), "%a%", "%a%", 50}, "cards_deck"},
	} {
		if plan := explain(t, s, c.q, c.args...); !usesIndex(plan, c.index) {
			t.Errorf("%s plan does not use %s: %s", name, c.index, plan)
		}
	}
	// '%term%' can't use an index: searching every deck is an accepted full
	// scan (MCP spec §5.3). Assert it so a change to this is noticed.
	if plan := explain(t, s, qSearchCards, "%a%", "%a%", 50); !strings.Contains(plan, "SCAN c") {
		t.Errorf("SearchCards plan: %s", plan)
	}
	if plan := explain(t, s, qGetCards(3), int64(1), int64(2), int64(3)); !strings.Contains(plan, "USING INTEGER PRIMARY KEY") {
		t.Errorf("GetCards plan does not use the primary key: %s", plan)
	}
}
