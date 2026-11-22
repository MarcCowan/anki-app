package store

import (
	"context"
	"fmt"
	"time"
)

// Sessions are used only in passphrase mode. Only the SHA-256 of a token is
// stored, never the token itself.

// Served by the sessions primary key (token_hash).
const qSessionValid = "SELECT 1 FROM sessions WHERE token_hash = ? AND expires_at > ?"

// CreateSession stores a session token hash.
func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, now, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO sessions (token_hash, created_at, expires_at) VALUES (?, ?, ?)",
		tokenHash, toMs(now), toMs(expires))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// SessionValid reports whether a token hash exists and has not expired.
func (s *Store) SessionValid(ctx context.Context, tokenHash []byte, now time.Time) (bool, error) {
	rows, err := s.db.QueryContext(ctx, qSessionValid, tokenHash, toMs(now))
	if err != nil {
		return false, fmt.Errorf("check session: %w", err)
	}
	defer rows.Close()
	found := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check session: %w", err)
	}
	return found, nil
}

// DeleteSession removes a session; deleting a missing one is not an error.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions sweeps expired sessions (a full scan; the table is tiny).
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", toMs(now)); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
