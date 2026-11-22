-- Claude connector (spec docs/specs/2026-11-08-mcp-connector-design.md §6.1).

CREATE TABLE oauth_clients (
  id            TEXT    PRIMARY KEY,           -- random client_id
  redirect_uris TEXT    NOT NULL,              -- JSON array, each from the fixed allowlist
  created_at    INTEGER NOT NULL
);

CREATE TABLE oauth_codes (
  code_hash      BLOB    PRIMARY KEY,
  client_id      TEXT    NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  redirect_uri   TEXT    NOT NULL,
  code_challenge TEXT    NOT NULL,
  expires_at     INTEGER NOT NULL
);

CREATE TABLE oauth_grants (                    -- one per connection; shown in Settings
  id           INTEGER PRIMARY KEY,
  client_id    TEXT    NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL
);

CREATE TABLE oauth_tokens (
  token_hash BLOB    PRIMARY KEY,
  grant_id   INTEGER NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
  kind       TEXT    NOT NULL CHECK (kind IN ('access', 'refresh')),
  expires_at INTEGER NOT NULL,
  used_at    INTEGER                            -- refresh tokens: set on rotation, for reuse detection
);

CREATE TABLE mcp_batches (
  id         INTEGER PRIMARY KEY,
  grant_id   INTEGER REFERENCES oauth_grants(id) ON DELETE SET NULL,
  deck_id    INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  card_count INTEGER NOT NULL CHECK (card_count BETWEEN 1 AND 50)
);

ALTER TABLE cards ADD COLUMN batch_id INTEGER REFERENCES mcp_batches(id) ON DELETE SET NULL;

-- Serves: deleting a batch's cards (DeleteBatch), each batch's remaining count (ListBatches),
-- and the cards.batch_id foreign key.
CREATE INDEX cards_batch ON cards(batch_id);

-- Serves: the rolling 24-hour card sum (AddBatch) and the newest-first list (ListBatches).
CREATE INDEX mcp_batches_created ON mcp_batches(created_at);

-- Serves: the oauth_tokens.grant_id cascade when a grant is deleted.
CREATE INDEX oauth_tokens_grant ON oauth_tokens(grant_id);

-- Serves: the oauth_grants.client_id cascade, and finding clients without a grant (eviction).
CREATE INDEX oauth_grants_client ON oauth_grants(client_id);

-- Serves: the oauth_codes.client_id cascade when a client is evicted.
CREATE INDEX oauth_codes_client ON oauth_codes(client_id);
