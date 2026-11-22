package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// OAuth storage for the Claude connector (MCP spec §4, §6.1). Codes and
// tokens are stored only as SHA-256 hashes.

// OAuthClient is a dynamically registered public client.
type OAuthClient struct {
	ID           string
	RedirectURIs []string
	CreatedAt    time.Time
}

// OAuthCode is a single-use authorization code.
type OAuthCode struct {
	Hash                                 []byte
	ClientID, RedirectURI, CodeChallenge string
	ExpiresAt                            time.Time
}

// NewToken is a token hash to store with its expiry.
type NewToken struct {
	Hash      []byte
	ExpiresAt time.Time
}

// Grant is one connection (one completed authorization).
type Grant struct {
	ID                    int64
	ClientID              string
	CreatedAt, LastUsedAt time.Time
}

const (
	// Served by oauth_grants_client (NOT EXISTS probe); the outer scan is over a table capped at maxClients.
	qEvictableClient = `SELECT c.id FROM oauth_clients c
		WHERE NOT EXISTS (SELECT 1 FROM oauth_grants g WHERE g.client_id = c.id)
		ORDER BY c.created_at LIMIT 1`
	// Served by the oauth_tokens primary key.
	qTokenByHash = `SELECT t.grant_id, t.kind, t.expires_at, t.used_at, g.client_id
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id WHERE t.token_hash = ?`
	// Served by the oauth_tokens primary key.
	qAccessGrant = "SELECT grant_id FROM oauth_tokens WHERE token_hash = ? AND kind = 'access' AND expires_at > ?"
	// The cascade from this delete is served by oauth_tokens_grant.
	qDeleteGrant = "DELETE FROM oauth_grants WHERE id = ?"
)

// CreateClient stores a client. When maxClients already exist, the oldest
// client with no grant is removed first; if every client has a grant, ErrFull.
func (s *Store) CreateClient(ctx context.Context, c OAuthClient, maxClients int) error {
	uris, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM oauth_clients").Scan(&n); err != nil {
			return err
		}
		if n >= maxClients {
			var victim string
			err := tx.QueryRowContext(ctx, qEvictableClient).Scan(&victim)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrFull
			}
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM oauth_clients WHERE id = ?", victim); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO oauth_clients (id, redirect_uris, created_at) VALUES (?, ?, ?)",
			c.ID, string(uris), toMs(c.CreatedAt))
		return err
	})
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	return nil
}

// GetClient returns one client. ErrNotFound if it does not exist.
func (s *Store) GetClient(ctx context.Context, id string) (OAuthClient, error) {
	var uris string
	var created int64
	err := s.db.QueryRowContext(ctx, "SELECT redirect_uris, created_at FROM oauth_clients WHERE id = ?", id).Scan(&uris, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthClient{}, fmt.Errorf("get client: %w", ErrNotFound)
	}
	if err != nil {
		return OAuthClient{}, fmt.Errorf("get client: %w", err)
	}
	c := OAuthClient{ID: id, CreatedAt: fromMs(created)}
	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return OAuthClient{}, fmt.Errorf("get client: %w", err)
	}
	return c, nil
}

// CreateCode stores an authorization code.
func (s *Store) CreateCode(ctx context.Context, c OAuthCode) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO oauth_codes (code_hash, client_id, redirect_uri, code_challenge, expires_at)
		VALUES (?, ?, ?, ?, ?)`, c.Hash, c.ClientID, c.RedirectURI, c.CodeChallenge, toMs(c.ExpiresAt))
	if err != nil {
		return fmt.Errorf("create code: %w", err)
	}
	return nil
}

// TakeCode deletes and returns a code, so it can be used only once, whether or
// not the caller's later checks pass. ErrNotFound if it does not exist.
func (s *Store) TakeCode(ctx context.Context, hash []byte) (OAuthCode, error) {
	c := OAuthCode{Hash: hash}
	var exp int64
	err := s.db.QueryRowContext(ctx, `DELETE FROM oauth_codes WHERE code_hash = ?
		RETURNING client_id, redirect_uri, code_challenge, expires_at`, hash).
		Scan(&c.ClientID, &c.RedirectURI, &c.CodeChallenge, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthCode{}, fmt.Errorf("take code: %w", ErrNotFound)
	}
	if err != nil {
		return OAuthCode{}, fmt.Errorf("take code: %w", err)
	}
	c.ExpiresAt = fromMs(exp)
	return c, nil
}

func insertTokensTx(ctx context.Context, tx *sql.Tx, grantID int64, access, refresh NewToken) error {
	for _, t := range []struct {
		kind string
		tok  NewToken
	}{{"access", access}, {"refresh", refresh}} {
		if _, err := tx.ExecContext(ctx, "INSERT INTO oauth_tokens (token_hash, grant_id, kind, expires_at) VALUES (?, ?, ?, ?)",
			t.tok.Hash, grantID, t.kind, toMs(t.tok.ExpiresAt)); err != nil {
			return err
		}
	}
	return nil
}

// CreateGrant records a new connection with its first token pair.
func (s *Store) CreateGrant(ctx context.Context, clientID string, now time.Time, access, refresh NewToken) (int64, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "INSERT INTO oauth_grants (client_id, created_at, last_used_at) VALUES (?, ?, ?) RETURNING id",
			clientID, toMs(now), toMs(now)).Scan(&id)
		if err != nil {
			return err
		}
		return insertTokensTx(ctx, tx, id, access, refresh)
	})
	if err != nil {
		return 0, fmt.Errorf("create grant: %w", err)
	}
	return id, nil
}

// RotateRefresh exchanges an unused, unexpired refresh token belonging to
// clientID for a new pair under the same grant. A refresh token that was
// already used deletes the whole grant and returns ErrTokenReused. Anything
// else invalid returns ErrNotFound and changes nothing.
func (s *Store) RotateRefresh(ctx context.Context, hash []byte, clientID string, now time.Time, access, refresh NewToken) (int64, error) {
	var grantID int64
	reused := false
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var kind, owner string
		var exp int64
		var used sql.NullInt64
		err := tx.QueryRowContext(ctx, qTokenByHash, hash).Scan(&grantID, &kind, &exp, &used, &owner)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if kind != "refresh" || owner != clientID || exp <= toMs(now) {
			return ErrNotFound
		}
		if used.Valid {
			// Commit the delete, then report the reuse.
			reused = true
			_, err := tx.ExecContext(ctx, qDeleteGrant, grantID)
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE oauth_tokens SET used_at = ? WHERE token_hash = ?", toMs(now), hash); err != nil {
			return err
		}
		return insertTokensTx(ctx, tx, grantID, access, refresh)
	})
	if err != nil {
		return 0, fmt.Errorf("rotate refresh: %w", err)
	}
	if reused {
		return 0, fmt.Errorf("rotate refresh: %w", ErrTokenReused)
	}
	return grantID, nil
}

// AccessGrant returns the grant of an unexpired access token. ErrNotFound otherwise.
func (s *Store) AccessGrant(ctx context.Context, hash []byte, now time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, qAccessGrant, hash, toMs(now)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("access grant: %w", ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("access grant: %w", err)
	}
	return id, nil
}

// TouchGrant records use of a grant, at most once a minute.
func (s *Store) TouchGrant(ctx context.Context, grantID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE oauth_grants SET last_used_at = ? WHERE id = ? AND last_used_at < ?",
		toMs(now), grantID, toMs(now.Add(-time.Minute)))
	if err != nil {
		return fmt.Errorf("touch grant: %w", err)
	}
	return nil
}

// RevokeToken deletes the grant a token belongs to. An unknown token is not an error.
func (s *Store) RevokeToken(ctx context.Context, hash []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM oauth_grants WHERE id = (SELECT grant_id FROM oauth_tokens WHERE token_hash = ?)", hash)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

// ListGrants returns every connection, newest first (a tiny table).
func (s *Store) ListGrants(ctx context.Context) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, client_id, created_at, last_used_at FROM oauth_grants ORDER BY created_at DESC, id DESC")
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	defer rows.Close()
	gs := []Grant{}
	for rows.Next() {
		var g Grant
		var created, used int64
		if err := rows.Scan(&g.ID, &g.ClientID, &created, &used); err != nil {
			return nil, fmt.Errorf("list grants: %w", err)
		}
		g.CreatedAt, g.LastUsedAt = fromMs(created), fromMs(used)
		gs = append(gs, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	return gs, nil
}

// DeleteAllGrants disconnects every client (their tokens cascade).
func (s *Store) DeleteAllGrants(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM oauth_grants"); err != nil {
		return fmt.Errorf("delete grants: %w", err)
	}
	return nil
}

// DeleteExpiredOAuth sweeps expired codes and tokens (full scans of small tables).
func (s *Store) DeleteExpiredOAuth(ctx context.Context, now time.Time) error {
	for _, q := range []string{"DELETE FROM oauth_codes WHERE expires_at <= ?", "DELETE FROM oauth_tokens WHERE expires_at <= ?"} {
		if _, err := s.db.ExecContext(ctx, q, toMs(now)); err != nil {
			return fmt.Errorf("delete expired oauth: %w", err)
		}
	}
	return nil
}
