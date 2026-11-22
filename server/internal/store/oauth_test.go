package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func h(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func tok(b byte, exp time.Time) NewToken { return NewToken{Hash: h(b), ExpiresAt: exp} }

func mustClient(t *testing.T, s *Store, id string, at time.Time, maxClients int) {
	t.Helper()
	c := OAuthClient{ID: id, RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"}, CreatedAt: at}
	if err := s.CreateClient(context.Background(), c, maxClients); err != nil {
		t.Fatal(err)
	}
}

func mustGrant(t *testing.T, s *Store, clientID string, access, refresh byte) int64 {
	t.Helper()
	id, err := s.CreateGrant(context.Background(), clientID, t0, tok(access, t0.Add(time.Hour)), tok(refresh, t0.Add(30*24*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGetClientRoundTrip(t *testing.T) {
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	c, err := s.GetClient(context.Background(), "A")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "A" || len(c.RedirectURIs) != 1 || c.RedirectURIs[0] != "https://claude.ai/api/mcp/auth_callback" || !c.CreatedAt.Equal(t0) {
		t.Errorf("client = %+v", c)
	}
	if _, err := s.GetClient(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown client err = %v", err)
	}
}

func TestCreateClientEvictsOldestUnused(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 2)
	mustClient(t, s, "B", t0.Add(time.Second), 2)
	mustGrant(t, s, "A", 1, 2)
	mustClient(t, s, "C", t0.Add(2*time.Second), 2)
	if _, err := s.GetClient(ctx, "B"); !errors.Is(err, ErrNotFound) {
		t.Errorf("B should be evicted, err = %v", err)
	}
	for _, id := range []string{"A", "C"} {
		if _, err := s.GetClient(ctx, id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestCreateClientFullReturnsErrFull(t *testing.T) {
	s := openTest(t)
	mustClient(t, s, "A", t0, 1)
	mustGrant(t, s, "A", 1, 2)
	c := OAuthClient{ID: "B", RedirectURIs: []string{"x"}, CreatedAt: t0}
	if err := s.CreateClient(context.Background(), c, 1); !errors.Is(err, ErrFull) {
		t.Fatalf("err = %v, want ErrFull", err)
	}
}

func TestTakeCodeIsSingleUse(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	code := OAuthCode{Hash: h(9), ClientID: "A", RedirectURI: "r", CodeChallenge: "ch", ExpiresAt: t0.Add(time.Minute)}
	if err := s.CreateCode(ctx, code); err != nil {
		t.Fatal(err)
	}
	got, err := s.TakeCode(ctx, h(9))
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientID != "A" || got.RedirectURI != "r" || got.CodeChallenge != "ch" || !got.ExpiresAt.Equal(code.ExpiresAt) {
		t.Errorf("code = %+v", got)
	}
	if _, err := s.TakeCode(ctx, h(9)); !errors.Is(err, ErrNotFound) {
		t.Errorf("second take err = %v", err)
	}
}

func TestCreateGrantThenAccessGrant(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	id := mustGrant(t, s, "A", 1, 2)
	got, err := s.AccessGrant(ctx, h(1), t0.Add(59*time.Minute))
	if err != nil || got != id {
		t.Fatalf("AccessGrant = %d, %v; want %d", got, err, id)
	}
	if _, err := s.AccessGrant(ctx, h(1), t0.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired access err = %v", err)
	}
	if _, err := s.AccessGrant(ctx, h(2), t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("refresh token as access err = %v", err)
	}
}

func TestRotateRefresh(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	id := mustGrant(t, s, "A", 1, 2)
	now := t0.Add(10 * time.Minute)
	got, err := s.RotateRefresh(ctx, h(2), "A", now, tok(3, now.Add(time.Hour)), tok(4, now.Add(30*24*time.Hour)))
	if err != nil || got != id {
		t.Fatalf("RotateRefresh = %d, %v; want %d", got, err, id)
	}
	for _, b := range []byte{1, 3} {
		if g, err := s.AccessGrant(ctx, h(b), now); err != nil || g != id {
			t.Errorf("access %d: %d, %v", b, g, err)
		}
	}
}

func TestRotateRefreshReuseDeletesGrant(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	mustGrant(t, s, "A", 1, 2)
	now := t0.Add(10 * time.Minute)
	if _, err := s.RotateRefresh(ctx, h(2), "A", now, tok(3, now.Add(time.Hour)), tok(4, now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RotateRefresh(ctx, h(2), "A", now, tok(5, now.Add(time.Hour)), tok(6, now.Add(time.Hour))); !errors.Is(err, ErrTokenReused) {
		t.Fatalf("reuse err = %v, want ErrTokenReused", err)
	}
	if _, err := s.AccessGrant(ctx, h(3), now); !errors.Is(err, ErrNotFound) {
		t.Errorf("newest access token still valid: %v", err)
	}
	if _, err := s.AccessGrant(ctx, h(5), now); !errors.Is(err, ErrNotFound) {
		t.Errorf("token from the reused refresh was issued: %v", err)
	}
	gs, err := s.ListGrants(ctx)
	if err != nil || len(gs) != 0 {
		t.Errorf("grants = %v, %v", gs, err)
	}
}

func TestRotateRefreshWrongClientOrExpired(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	mustClient(t, s, "B", t0, 20)
	mustGrant(t, s, "A", 1, 2)
	n := tok(3, t0.Add(time.Hour))
	if _, err := s.RotateRefresh(ctx, h(2), "B", t0, n, tok(4, t0.Add(time.Hour))); !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong client err = %v", err)
	}
	late := t0.Add(31 * 24 * time.Hour)
	if _, err := s.RotateRefresh(ctx, h(2), "A", late, tok(5, late), tok(6, late)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired err = %v", err)
	}
	if _, err := s.RotateRefresh(ctx, h(1), "A", t0, tok(7, t0), tok(8, t0)); !errors.Is(err, ErrNotFound) {
		t.Errorf("access token as refresh err = %v", err)
	}
	if gs, _ := s.ListGrants(ctx); len(gs) != 1 {
		t.Errorf("grant should survive; grants = %v", gs)
	}
}

func TestTouchGrantAtMostOncePerMinute(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	id := mustGrant(t, s, "A", 1, 2)
	if err := s.TouchGrant(ctx, id, t0.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	gs, _ := s.ListGrants(ctx)
	if !gs[0].LastUsedAt.Equal(t0) {
		t.Errorf("touched within a minute: %v", gs[0].LastUsedAt)
	}
	if err := s.TouchGrant(ctx, id, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	gs, _ = s.ListGrants(ctx)
	if !gs[0].LastUsedAt.Equal(t0.Add(2 * time.Minute)) {
		t.Errorf("LastUsedAt = %v", gs[0].LastUsedAt)
	}
}

func TestRevokeToken(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	mustGrant(t, s, "A", 1, 2)
	if err := s.RevokeToken(ctx, h(99)); err != nil {
		t.Fatalf("unknown token: %v", err)
	}
	if err := s.RevokeToken(ctx, h(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccessGrant(ctx, h(1), t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("access after revoke err = %v", err)
	}
}

func TestListGrantsNewestFirstAndDeleteAll(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	g1 := mustGrant(t, s, "A", 1, 2)
	g2, err := s.CreateGrant(ctx, "A", t0.Add(time.Minute), tok(3, t0.Add(time.Hour)), tok(4, t0.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	gs, err := s.ListGrants(ctx)
	if err != nil || len(gs) != 2 || gs[0].ID != g2 || gs[1].ID != g1 || gs[0].ClientID != "A" {
		t.Fatalf("grants = %+v, %v", gs, err)
	}
	if err := s.DeleteAllGrants(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccessGrant(ctx, h(3), t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("access after disconnect err = %v", err)
	}
	if gs, _ := s.ListGrants(ctx); len(gs) != 0 {
		t.Errorf("grants = %v", gs)
	}
}

func TestDeleteExpiredOAuth(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	mustClient(t, s, "A", t0, 20)
	mustGrant(t, s, "A", 1, 2) // access expires t0+1h, refresh t0+30d
	if err := s.CreateCode(ctx, OAuthCode{Hash: h(9), ClientID: "A", RedirectURI: "r", CodeChallenge: "c", ExpiresAt: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteExpiredOAuth(ctx, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var tokens, codes int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM oauth_tokens").Scan(&tokens)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM oauth_codes").Scan(&codes)
	if tokens != 1 || codes != 0 {
		t.Errorf("tokens = %d (want 1, the refresh), codes = %d", tokens, codes)
	}
}
