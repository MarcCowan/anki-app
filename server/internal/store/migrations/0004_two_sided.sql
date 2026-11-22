-- Two-sided cards (spec docs/specs/2026-11-14-two-sided-cards-design.md §2).
-- Scheduling moves from cards into review_states (one row per direction), and
-- review_logs point at a review state instead of a card.
--
-- ORDER MATTERS. This runs in a transaction with foreign_keys = ON, which can't
-- be turned off here. DROP TABLE on a parent deletes its rows first, so any
-- child still referencing it would be cascade-deleted. So: build the new tables
-- and copy into them, drop the old review_logs BEFORE the old cards (nothing
-- references the old cards by then), and rename last. Renaming cards_new to
-- cards also rewrites review_states' REFERENCES clause to "cards".

ALTER TABLE decks ADD COLUMN two_sided INTEGER NOT NULL DEFAULT 0 CHECK (two_sided IN (0, 1));

CREATE TABLE cards_new (
  id         INTEGER PRIMARY KEY,
  deck_id    INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  front_md   TEXT    NOT NULL,
  back_md    TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  batch_id   INTEGER REFERENCES mcp_batches(id) ON DELETE SET NULL,
  two_sided  INTEGER NOT NULL DEFAULT 0 CHECK (two_sided IN (0, 1))
);

INSERT INTO cards_new (id, deck_id, front_md, back_md, created_at, updated_at, batch_id)
SELECT id, deck_id, front_md, back_md, created_at, updated_at, batch_id FROM cards;

CREATE TABLE review_states (
  id              INTEGER PRIMARY KEY,
  card_id         INTEGER NOT NULL REFERENCES cards_new(id) ON DELETE CASCADE,
  deck_id         INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  reverse         INTEGER NOT NULL CHECK (reverse IN (0, 1)),   -- 0 front->back, 1 back->front
  state           INTEGER NOT NULL DEFAULT 0,
  due             INTEGER NOT NULL,
  stability       REAL    NOT NULL DEFAULT 0,
  difficulty      REAL    NOT NULL DEFAULT 0,
  elapsed_days    INTEGER NOT NULL DEFAULT 0,
  scheduled_days  INTEGER NOT NULL DEFAULT 0,
  reps            INTEGER NOT NULL DEFAULT 0,
  lapses          INTEGER NOT NULL DEFAULT 0,
  remaining_steps INTEGER NOT NULL DEFAULT 0,
  last_review     INTEGER,
  UNIQUE (card_id, reverse)
);

-- Each card's forward state keeps the card's id, so its logs map by id.
INSERT INTO review_states (id, card_id, deck_id, reverse, state, due, stability, difficulty,
  elapsed_days, scheduled_days, reps, lapses, remaining_steps, last_review)
SELECT id, id, deck_id, 0, state, due, stability, difficulty,
  elapsed_days, scheduled_days, reps, lapses, remaining_steps, last_review FROM cards;

CREATE TABLE review_logs_new (
  id                     INTEGER PRIMARY KEY,
  review_state_id        INTEGER NOT NULL REFERENCES review_states(id) ON DELETE CASCADE,
  rating                 INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 4),
  reviewed_at            INTEGER NOT NULL,
  duration_ms            INTEGER NOT NULL CHECK (duration_ms >= 0),
  state_before           INTEGER NOT NULL,
  due_before             INTEGER NOT NULL,
  stability_before       REAL    NOT NULL,
  difficulty_before      REAL    NOT NULL,
  elapsed_days_before    INTEGER NOT NULL,
  scheduled_days_before  INTEGER NOT NULL,
  reps_before            INTEGER NOT NULL,
  lapses_before          INTEGER NOT NULL,
  remaining_steps_before INTEGER NOT NULL,
  last_review_before     INTEGER
);

INSERT INTO review_logs_new (id, review_state_id, rating, reviewed_at, duration_ms, state_before, due_before,
  stability_before, difficulty_before, elapsed_days_before, scheduled_days_before, reps_before,
  lapses_before, remaining_steps_before, last_review_before)
SELECT id, card_id, rating, reviewed_at, duration_ms, state_before, due_before,
  stability_before, difficulty_before, elapsed_days_before, scheduled_days_before, reps_before,
  lapses_before, remaining_steps_before, last_review_before FROM review_logs;

DROP TABLE review_logs;   -- first: it is the only table referencing the old cards
DROP TABLE cards;         -- safe now: no child rows left to cascade into
ALTER TABLE cards_new RENAME TO cards;
ALTER TABLE review_logs_new RENAME TO review_logs;

-- Serves: the queue queries and deck counts (FirstDue, FirstNew, CountDue, CountState, MinDue),
-- and the review_states.deck_id foreign key. Replaces cards_deck_state_due.
CREATE INDEX review_states_deck_state_due ON review_states(deck_id, state, due);

-- Serves: cards in a deck (ListCards, ListFronts, the deck search, DeckCardCounts) and the
-- cards.deck_id foreign key.
CREATE INDEX cards_deck ON cards(deck_id);

-- Unchanged from 0003 (dropped with the old table): DeleteBatch, ListBatches, cards.batch_id FK.
CREATE INDEX cards_batch ON cards(batch_id);

-- Serves: undo (latest log for a state), burying (a sibling's logs since day start),
-- CountNewIntroducedSince, and the review_logs.review_state_id foreign key.
-- Replaces review_logs_card_time.
CREATE INDEX review_logs_state_time ON review_logs(review_state_id, reviewed_at);
