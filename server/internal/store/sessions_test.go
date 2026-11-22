package store

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestSessionsLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	a := bytes.Repeat([]byte{1}, 32)
	b := bytes.Repeat([]byte{2}, 32)
	if err := s.CreateSession(ctx, a, t0, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, b, t0, t0.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	valid := func(h []byte, at time.Time) bool {
		t.Helper()
		ok, err := s.SessionValid(ctx, h, at)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !valid(a, t0.Add(59*time.Minute)) {
		t.Fatal("valid before expiry")
	}
	if valid(a, t0.Add(time.Hour)) {
		t.Fatal("valid at expiry")
	}
	if valid(bytes.Repeat([]byte{3}, 32), t0) {
		t.Fatal("unknown token valid")
	}
	if err := s.DeleteExpiredSessions(ctx, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "sessions"); n != 1 {
		t.Fatalf("sessions after sweep = %d", n)
	}
	if !valid(b, t0.Add(2*time.Hour)) {
		t.Fatal("unexpired session removed")
	}
	if err := s.DeleteSession(ctx, b); err != nil {
		t.Fatal(err)
	}
	if valid(b, t0) {
		t.Fatal("valid after delete")
	}
	if err := s.DeleteSession(ctx, b); err != nil {
		t.Fatalf("deleting a missing session: %v", err)
	}
}

func TestSessionLookupUsesPrimaryKey(t *testing.T) {
	s := openTest(t)
	plan := explain(t, s, qSessionValid, []byte{1}, int64(0))
	if !strings.Contains(plan, "sqlite_autoindex_sessions_1") && !strings.Contains(plan, "PRIMARY KEY") {
		t.Fatalf("plan = %s", plan)
	}
}
