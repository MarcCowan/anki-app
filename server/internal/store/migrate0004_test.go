package store

import (
	"context"
	"path/filepath"
	"testing"
)

// seedV3 builds a version-3 database with two cards, one with two review logs
// and an MCP batch, written with the old schema's SQL.
func seedV3(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3.db")
	s, err := openUnmigrated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.migrateTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO decks (id, name, new_per_day, created_at) VALUES (7, 'cpp', 15, 100)`,
		`INSERT INTO mcp_batches (id, grant_id, deck_id, created_at, card_count) VALUES (3, NULL, 7, 150, 1)`,
		`INSERT INTO cards (id, deck_id, front_md, back_md, created_at, updated_at, state, due, stability,
			difficulty, elapsed_days, scheduled_days, reps, lapses, remaining_steps, last_review, batch_id)
			VALUES (11, 7, 'f11', 'b11', 200, 300, 2, 9000, 4.5, 6.25, 3, 8, 2, 1, 0, 800, NULL)`,
		`INSERT INTO cards (id, deck_id, front_md, back_md, created_at, updated_at, state, due, stability,
			difficulty, elapsed_days, scheduled_days, reps, lapses, remaining_steps, last_review, batch_id)
			VALUES (12, 7, 'f12', 'b12', 210, 210, 0, 210, 0, 0, 0, 0, 0, 0, 0, NULL, 3)`,
		`INSERT INTO review_logs (id, card_id, rating, reviewed_at, duration_ms, state_before, due_before,
			stability_before, difficulty_before, elapsed_days_before, scheduled_days_before, reps_before,
			lapses_before, remaining_steps_before, last_review_before)
			VALUES (21, 11, 3, 500, 1000, 0, 200, 0, 0, 0, 0, 0, 0, 0, NULL),
			       (22, 11, 1, 800, 2000, 1, 600, 1.5, 5, 0, 0, 1, 0, 1, 500)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	return s, path
}

func TestMigration0004KeepsSchedulesAndHistory(t *testing.T) {
	ctx := context.Background()
	s, _ := seedV3(t)
	defer s.Close()
	if err := s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v := pragma(t, s, "user_version"); v != "4" {
		t.Fatalf("user_version = %s", v)
	}

	// Each card keeps its content and gets one forward state with the same id.
	type stateRow struct {
		id, card, deck, reverse, state, due   int64
		stability, difficulty                 float64
		elapsed, scheduled, reps, lapses, rem int64
		last                                  *int64
	}
	rows, err := s.db.Query(`SELECT id, card_id, deck_id, reverse, state, due, stability, difficulty,
		elapsed_days, scheduled_days, reps, lapses, remaining_steps, last_review FROM review_states ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var got []stateRow
	for rows.Next() {
		var r stateRow
		if err := rows.Scan(&r.id, &r.card, &r.deck, &r.reverse, &r.state, &r.due, &r.stability, &r.difficulty,
			&r.elapsed, &r.scheduled, &r.reps, &r.lapses, &r.rem, &r.last); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	rows.Close()
	if len(got) != 2 {
		t.Fatalf("got %d states, want 2", len(got))
	}
	a := got[0]
	if a.id != 11 || a.card != 11 || a.deck != 7 || a.reverse != 0 || a.state != 2 || a.due != 9000 ||
		a.stability != 4.5 || a.difficulty != 6.25 || a.elapsed != 3 || a.scheduled != 8 || a.reps != 2 ||
		a.lapses != 1 || a.rem != 0 || a.last == nil || *a.last != 800 {
		t.Errorf("card 11 state = %+v", a)
	}
	if b := got[1]; b.id != 12 || b.card != 12 || b.state != 0 || b.due != 210 || b.last != nil {
		t.Errorf("card 12 state = %+v", b)
	}

	// Both logs survive, now pointing at card 11's state.
	var logs, logsFor11 int
	if err := s.db.QueryRow("SELECT COUNT(*), SUM(review_state_id = 11) FROM review_logs").Scan(&logs, &logsFor11); err != nil {
		t.Fatal(err)
	}
	if logs != 2 || logsFor11 != 2 {
		t.Errorf("logs = %d (for state 11: %d), want 2", logs, logsFor11)
	}
	var last int64
	if err := s.db.QueryRow("SELECT last_review_before FROM review_logs WHERE id = 22").Scan(&last); err != nil || last != 500 {
		t.Errorf("log 22 last_review_before = %d, %v", last, err)
	}

	// Content, batch link and the new flags.
	var front, back string
	var created, updated, two int64
	var batch *int64
	if err := s.db.QueryRow("SELECT front_md, back_md, created_at, updated_at, two_sided, batch_id FROM cards WHERE id = 12").
		Scan(&front, &back, &created, &updated, &two, &batch); err != nil {
		t.Fatal(err)
	}
	if front != "f12" || back != "b12" || created != 210 || updated != 210 || two != 0 || batch == nil || *batch != 3 {
		t.Errorf("card 12 = %q %q %d %d two=%d batch=%v", front, back, created, updated, two, batch)
	}
	var deckTwo, perDay int
	if err := s.db.QueryRow("SELECT two_sided, new_per_day FROM decks WHERE id = 7").Scan(&deckTwo, &perDay); err != nil {
		t.Fatal(err)
	}
	if deckTwo != 0 || perDay != 15 {
		t.Errorf("deck two_sided=%d new_per_day=%d", deckTwo, perDay)
	}

	// The schedule columns are gone from cards.
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('cards') WHERE name IN ('state', 'due', 'reps')").Scan(&n); err != nil || n != 0 {
		t.Errorf("cards still has %d schedule columns (%v)", n, err)
	}

	// Foreign keys are consistent and point at the renamed tables.
	if r, err := s.db.Query("PRAGMA foreign_key_check"); err != nil {
		t.Fatal(err)
	} else {
		if r.Next() {
			t.Error("foreign_key_check reports violations")
		}
		r.Close()
	}
	for tbl, want := range map[string]string{"review_states": "cards", "review_logs": "review_states"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list(?) WHERE "table" = ?`, tbl, want).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s has %d foreign keys to %s (%v)", tbl, n, want, err)
		}
	}
	if v := pragma(t, s, "integrity_check"); v != "ok" {
		t.Errorf("integrity_check = %s", v)
	}

	// Deleting a card now cascades to its state and that state's logs.
	if _, err := s.db.Exec("DELETE FROM cards WHERE id = 11"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT (SELECT COUNT(*) FROM review_states WHERE card_id = 11) + (SELECT COUNT(*) FROM review_logs)").Scan(&n); err != nil || n != 0 {
		t.Errorf("after delete: %d rows left (%v)", n, err)
	}
}
