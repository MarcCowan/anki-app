-- Initial schema (spec §4). All timestamps are UTC Unix milliseconds.

CREATE TABLE decks (
  id           INTEGER PRIMARY KEY,
  name         TEXT    NOT NULL UNIQUE,
  new_per_day  INTEGER NOT NULL DEFAULT 20 CHECK (new_per_day BETWEEN 0 AND 9999),
  created_at   INTEGER NOT NULL
);

CREATE TABLE cards (
  id             INTEGER PRIMARY KEY,
  deck_id        INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  front_md       TEXT    NOT NULL,
  back_md        TEXT    NOT NULL,
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  state          INTEGER NOT NULL DEFAULT 0,
  due            INTEGER NOT NULL,
  stability      REAL    NOT NULL DEFAULT 0,
  difficulty     REAL    NOT NULL DEFAULT 0,
  elapsed_days   INTEGER NOT NULL DEFAULT 0,
  scheduled_days INTEGER NOT NULL DEFAULT 0,
  reps           INTEGER NOT NULL DEFAULT 0,
  lapses         INTEGER NOT NULL DEFAULT 0,
  remaining_steps INTEGER NOT NULL DEFAULT 0,
  last_review    INTEGER
);

CREATE TABLE review_logs (
  id                    INTEGER PRIMARY KEY,
  card_id               INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  rating                INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 4),
  reviewed_at           INTEGER NOT NULL,
  duration_ms           INTEGER NOT NULL CHECK (duration_ms >= 0),
  state_before          INTEGER NOT NULL,
  due_before            INTEGER NOT NULL,
  stability_before      REAL    NOT NULL,
  difficulty_before     REAL    NOT NULL,
  elapsed_days_before   INTEGER NOT NULL,
  scheduled_days_before INTEGER NOT NULL,
  reps_before           INTEGER NOT NULL,
  lapses_before         INTEGER NOT NULL,
  remaining_steps_before INTEGER NOT NULL,
  last_review_before    INTEGER
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE sessions (
  token_hash  BLOB    PRIMARY KEY,
  created_at  INTEGER NOT NULL,
  expires_at  INTEGER NOT NULL
);

-- Serves: the study-queue queries (FirstDue, FirstNew, CountDue, CountState, MinDue),
-- the per-deck counts, and the cards.deck_id foreign key (cascade deletes, ListCards).
CREATE INDEX cards_deck_state_due ON cards(deck_id, state, due);

-- Serves: the review_logs.card_id foreign key, undo (latest log for a card), and
-- CountNewIntroducedSince (the deck's cards joined to logs since day start).
CREATE INDEX review_logs_card_time ON review_logs(card_id, reviewed_at);

INSERT INTO settings (key, value) VALUES ('desired_retention', '0.9'), ('day_rollover_hour', '4');
