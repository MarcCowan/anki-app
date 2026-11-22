# Two-sided cards: design spec

Date: 2026-11-14. Extends [2026-11-02-flashcards-design.md](2026-11-02-flashcards-design.md) (§4, §5, §6, §8).

## 1. Purpose

A **two-sided** card is studied in both directions: front → back, and back → front. Each direction has its own
FSRS schedule and review history, but it is still one card: one piece of content, edited and deleted together.

- A card can be made two-sided when it is created, or later in the editor.
- A deck has a **two-sided default**, chosen when the deck is created and changeable in deck settings.
- Turning the default **on** for a deck makes every card in it two-sided, including cards turned off one by one
  earlier. Individual cards can then be turned off again.
- Turning the default **off** changes only the default. A confirmation asks whether to also remove the reverse
  direction from every card in the deck.
- Turning one card's two-sided **off** deletes its reverse schedule and that schedule's review history, after a
  warning. Turning it on again starts the reverse as a new item.

## 2. Data model (migration `0004_two_sided.sql`)

Scheduling moves off `cards` into `review_states`, one row per direction:

```sql
ALTER TABLE decks ADD COLUMN two_sided INTEGER NOT NULL DEFAULT 0 CHECK (two_sided IN (0, 1));

CREATE TABLE cards (            -- rebuilt without the FSRS columns
  id, deck_id, front_md, back_md, created_at, updated_at, batch_id,
  two_sided INTEGER NOT NULL DEFAULT 0 CHECK (two_sided IN (0, 1))
);

CREATE TABLE review_states (
  id         INTEGER PRIMARY KEY,
  card_id    INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  deck_id    INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  reverse    INTEGER NOT NULL CHECK (reverse IN (0, 1)),   -- 0 front→back, 1 back→front
  state, due, stability, difficulty, elapsed_days, scheduled_days, reps, lapses, remaining_steps, last_review,
  UNIQUE (card_id, reverse)
);

-- review_logs: card_id is replaced by review_state_id REFERENCES review_states(id) ON DELETE CASCADE.
```

- Every card has a forward state. A card has a reverse state **if and only if** `cards.two_sided = 1`. SQLite
  cannot enforce a rule across two tables, so the store changes both in one transaction every time, and a store
  test checks the invariant after each operation that touches it.
- `review_states.deck_id` copies the card's deck (cards never move between decks, §2 of the main spec). It
  exists so the queue index can be `(deck_id, state, due)`.
- **Migration safety.** Migrations run in a transaction with `foreign_keys = ON`, which cannot be turned off
  there, and `DROP TABLE` on a parent table deletes its rows first, so `ON DELETE CASCADE` fires. The migration
  therefore builds the new tables, copies the data (each forward state keeps its card's id, so logs map by
  `review_state_id = card_id`), drops the old `review_logs` **before** the old `cards`, and only then renames.
  A migration test seeds a version-3 database with history and checks that every schedule and log survives.
  Take a manual backup on the VM before deploying.

### 2.1 Indexes

| Index | Queries it serves |
|---|---|
| `review_states_deck_state_due ON review_states(deck_id, state, due)` | The queue queries and deck counts (§3); the `review_states.deck_id` foreign key. Replaces `cards_deck_state_due`. |
| `UNIQUE (card_id, reverse)` (implicit) | A card's state for a direction; the sibling lookup for burying; the `review_states.card_id` foreign key. |
| `cards_deck ON cards(deck_id)` | Cards in a deck (list, fronts, deck search, card counts); the `cards.deck_id` foreign key. |
| `cards_batch ON cards(batch_id)` | Recreated unchanged after the rebuild. |
| `review_logs_state_time ON review_logs(review_state_id, reviewed_at)` | Undo (latest log for a state); burying (a sibling's logs since day start); new introduced today; the foreign key. Replaces `review_logs_card_time`. |

## 3. Study queue (replaces main spec §5.1 steps 3–5)

The queue is made of review states, not cards.

- **Burying siblings.** A state is held back until the next day rollover if the other direction of its card has
  a review log at or after `dayStart`. This applies to every queue step, including learn-ahead and the counts.
- **No new-card cap.** Every unburied New state is available. `new_per_day` stays on the deck as a **target**:
  the deck page shows how many new items were introduced today against it, and nothing is held back. Each
  direction counts as one new item.
- Learning, review and new ordering is unchanged. Reverse states created later have `due` = their creation time,
  so they come after the deck's existing new items.

## 4. HTTP API changes (main spec §6)

| Method & path | Change |
|---|---|
| `GET /api/decks`, `GET /api/decks/{id}` | Deck adds `twoSided`. `counts.new` is every unburied New item; counts add `newToday`. |
| `POST /api/decks` | `{name, twoSided?}` (default false). |
| `PATCH /api/decks/{id}` | Adds `twoSided?` and `removeReverses?`. `twoSided: true` makes every card two-sided. `removeReverses: true` is only accepted with `twoSided: false` and removes every reverse state in the deck. |
| Card responses | Add `twoSided`. `state`, `due` and `reps` are the forward direction's. |
| `POST /api/decks/{id}/cards` | `{front, back, twoSided?}`; when omitted, the deck's default. |
| `PATCH /api/cards/{id}` | Adds `twoSided?`. |
| `GET /api/decks/{id}/next` | Adds `stateId` and `reverse`. `reps` is that state's. |
| `POST /api/states/{id}/review`, `POST /api/states/{id}/undo` | Replace `/api/cards/{id}/review` and `/undo`; same bodies; respond `{state: {id, cardId, reverse, state, due, reps}}`. |

The MCP `add_cards` tool and the `import` command take the deck's default. The MCP tools stay add-only.

## 5. Frontend (main spec §8)

- **Study:** for a reverse item, the back is shown first and **Show answer** reveals the front below the divider.
  A small "Reverse" label marks it.
- **Card editor:** a **Two-sided** switch. A new card starts with the deck's default. Turning it off on a saved
  card asks for confirmation (the reverse review history is deleted) when the change is saved.
- **Deck settings:** a **Two-sided by default** switch. Turning it on confirms that every card becomes two-sided.
  Turning it off asks whether to also remove the reverse from every card (keep / remove).
- **New deck:** the existing new-deck form gets a **Two-sided** checkbox.
- New cards per day is labelled as a daily **target**, and the deck page shows "new today: n / target".
