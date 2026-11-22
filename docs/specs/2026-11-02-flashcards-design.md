# Flashcards app: design spec

- **Date:** 2026-11-02
- **Status:** Draft, awaiting review
- **Owner:** Marc (sole user)

## 1. Purpose and success criteria

Marc studies C++ (and later other topics) with spaced-repetition flashcards. He currently uses AnkiApp. The cards there are plain text with code mixed into it, and AnkiApp centres everything, which wrecks code layout (see the original screenshots). This project replaces AnkiApp with a small personal web app built for **cards that contain code**.

Success means:

1. Cards render Markdown, with **syntax-highlighted fenced code blocks** and **inline code**, on both the front and the back.
2. The layout reads well on the **laptop and on an iPhone**, including code that is wider than the phone screen.
3. Reviews use **FSRS** with the standard **Again / Hard / Good / Easy** buttons.
4. **Multiple decks**, which can be created, renamed and deleted. Cards are created, edited and deleted **in the web app** (the app is the only place content is edited).
5. The existing 94-card `Cpp` deck is loaded into the app (after a separate cleanup step, §10).
6. It runs on this laptop and is reachable from the iPhone over a private Tailscale network, with no login. It can later move to a free cloud VM by changing configuration only.
   *Update 2026-11-05:* it now runs on a Google Cloud VM, reachable over Tailscale with passphrase mode on. See [2026-11-05-server-deploy-design.md](2026-11-05-server-deploy-design.md).

**What Marc said vs. what I assumed:** Marc specified the items above, Go + React, FSRS, Tailscale first and the cloud VM later, centred prose with left-aligned code blocks, a scroll/wrap setting for code with an 80-column default, the web app manifest, React Router, cleanup before import, the card-size limit rule, and TDD where it makes sense. I chose the remaining details (exact schema, endpoints, security controls) during design and Marc approved them section by section.

## 2. Scope

**In scope:** everything needed for the six criteria above, plus a passphrase mode that is **built but switched off by default** (§7.3), so the later cloud move needs no code changes.

**Out of scope (YAGNI):** user accounts, roles or sharing; offline mode or a service worker; full-text search (a client-side filter is enough); moving cards between decks; images and other media; FSRS parameter optimisation (the logs keep the data for it); the AnkiApp XML parser and content cleanup (§10).

*Update 2026-11-05:* backups (with a `backup` subcommand) and provisioning the cloud VM are now in scope. See [2026-11-05-server-deploy-design.md](2026-11-05-server-deploy-design.md).

## 3. Architecture

```
anki-app/
  CLAUDE.md                 project-wide rules (Appendix A)
  server/                   Go module
    CLAUDE.md               Go-specific rules (Appendix B)
    cmd/anki-app/           main.go: config, wiring, subcommands `serve`, `import`, `version`
    internal/store/         all SQL; embedded migrations; owns the *sql.DB
    internal/srs/           the only package that imports go-fsrs; pure (no DB)
    internal/study/         study queue, answer, undo: uses store + srs + clock
    internal/clock/, internal/validate/, internal/config/   small support packages
    internal/api/           HTTP handlers, middleware (security headers, Host/Origin, auth)
    internal/webui/         go:embed of the built frontend (dist/ is generated, gitignored)
  web/                      React + TypeScript + Vite
    CLAUDE.md               frontend rules (Appendix C)
  data/                     cleaned seed JSON (produced by the cleanup task, §10)
  docs/specs/  docs/plans/  docs/ideas/  docs/runbooks/
  deploy/vm/                VM scripts and systemd units (server-deploy spec §7)
  Dockerfile                container image (server-deploy spec §5)
```

- **One process, one binary, one SQLite file.** The Go binary serves the JSON API under `/api/` and the embedded single-page app for every other path. Unknown non-API paths return `index.html` so client-side routing works.
- **Dev loop:** Vite's dev server forwards `/api` to the Go server. `vite build` writes straight into `server/internal/webui/dist/`, because `go:embed` cannot reach outside its package directory.
- **Libraries:**
  - **Go:** standard library `net/http` routing (method and path patterns), `modernc.org/sqlite` (pure Go, so no C compiler on Windows), and `github.com/open-spaced-repetition/go-fsrs/v4` (official port, FSRS-6).
  - **Web:** React, React Router, `markdown-it`, **highlight.js** (core plus the registered languages only), DOMPurify, Vitest + React Testing Library, and Playwright.
  - *Changed from Shiki during planning:* Shiki outputs inline `style` attributes, which `style-src 'self'` blocks, and its default regex engine is WebAssembly, which `script-src 'self'` blocks. highlight.js outputs class names only and runs synchronously, so it works under the strict CSP with a normal stylesheet.
- **Toolchain prerequisites** (installed with `winget` at the start of the plan): current stable Go, current Node LTS (the installed Node 20.14 is end of life and too old for current Vite), and Tailscale.

### 3.1 Configuration (environment variables)

| Variable | Default | Purpose |
|---|---|---|
| `ADDR` | `127.0.0.1:8080` | Listen address. The app never binds to all interfaces by default. |
| `DB_PATH` | `./anki-app.db` | SQLite file. |
| `ALLOWED_HOSTS` | `localhost,127.0.0.1` | Comma-separated `Host` allowlist (§7.1). Add the tailnet name, e.g. `laptop.tailXXXX.ts.net`. |
| `PASSPHRASE` | unset | If set, enables passphrase mode (§7.3). |
| `TZ` | system zone | Time zone used for the day rollover. |
| `BACKUP_DIR` | `backups` next to `DB_PATH` | Root of the backup folders (server-deploy spec §4). |

## 4. Data model

All timestamps are stored as **UTC Unix milliseconds (`INTEGER`)**, so comparisons and index range scans are simple and unambiguous. Every connection runs `PRAGMA foreign_keys = ON` (SQLite ignores `ON DELETE CASCADE` without it), `PRAGMA journal_mode = WAL` and `PRAGMA busy_timeout = 5000`.

```sql
CREATE TABLE decks (
  id           INTEGER PRIMARY KEY,
  name         TEXT    NOT NULL UNIQUE,          -- trimmed, 1..100 code points
  new_per_day  INTEGER NOT NULL DEFAULT 20 CHECK (new_per_day BETWEEN 0 AND 9999),
  created_at   INTEGER NOT NULL
);

CREATE TABLE cards (
  id             INTEGER PRIMARY KEY,
  deck_id        INTEGER NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
  front_md       TEXT    NOT NULL,               -- 1..6000 code points
  back_md        TEXT    NOT NULL,               -- 1..6000 code points
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  -- FSRS state (mirrors go-fsrs v4 Card; elapsed_days is computed by internal/srs)
  state          INTEGER NOT NULL DEFAULT 0,     -- 0 New, 1 Learning, 2 Review, 3 Relearning
  due            INTEGER NOT NULL,
  stability      REAL    NOT NULL DEFAULT 0,
  difficulty     REAL    NOT NULL DEFAULT 0,
  elapsed_days   INTEGER NOT NULL DEFAULT 0,
  scheduled_days INTEGER NOT NULL DEFAULT 0,
  reps           INTEGER NOT NULL DEFAULT 0,
  lapses         INTEGER NOT NULL DEFAULT 0,
  remaining_steps INTEGER NOT NULL DEFAULT 0,     -- position in the (re)learning steps
  last_review    INTEGER                          -- NULL until first review
);

CREATE TABLE review_logs (
  id              INTEGER PRIMARY KEY,
  card_id         INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  rating          INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 4),
  reviewed_at     INTEGER NOT NULL,
  duration_ms     INTEGER NOT NULL CHECK (duration_ms >= 0),
  -- full FSRS state of the card BEFORE this review (used for undo)
  state_before    INTEGER NOT NULL,
  due_before      INTEGER NOT NULL,
  stability_before REAL   NOT NULL,
  difficulty_before REAL  NOT NULL,
  elapsed_days_before INTEGER NOT NULL,
  scheduled_days_before INTEGER NOT NULL,
  reps_before     INTEGER NOT NULL,
  lapses_before   INTEGER NOT NULL,
  remaining_steps_before INTEGER NOT NULL,
  last_review_before INTEGER
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);  -- seeded: desired_retention = 0.90, day_rollover_hour = 4

CREATE TABLE sessions (                -- used only in passphrase mode
  token_hash  BLOB    PRIMARY KEY,     -- SHA-256 of the random 256-bit token
  created_at  INTEGER NOT NULL,
  expires_at  INTEGER NOT NULL
);
```

*Update 2026-11-14:* two-sided cards move scheduling into a `review_states` table (one row per direction) and change the queue, the API and the editor; see [2026-11-14-two-sided-cards-design.md](2026-11-14-two-sided-cards-design.md).

### 4.1 Indexes (each one justified by a real query)

| Index | Queries it serves |
|---|---|
| `CREATE INDEX cards_deck_state_due ON cards(deck_id, state, due);` | The three study-queue queries (§5.1); the per-deck due and new counts on the deck list; and, because `deck_id` is its leftmost column, the `cards.deck_id` foreign key (cascade deletes and "cards in deck"). A separate `cards(deck_id)` index would be redundant. |
| `CREATE INDEX review_logs_card_time ON review_logs(card_id, reviewed_at);` | The `review_logs.card_id` foreign key (cascade deletes without a full scan per card); undo (latest log for a card); the "new cards introduced today" count (the deck's cards joined to logs with `reviewed_at >= day_start AND state_before = 0`). |
| `decks.name UNIQUE` (implicit index) | Duplicate-name check and lookup by name (`import`). |
| `sessions` primary key on `token_hash` | Session lookup on every request in passphrase mode. |

**Deliberately not indexed:** card text, `updated_at`, `reviewed_at` on its own, `sessions.expires_at` (expired sessions are swept by a full scan at startup and on login; the table holds a handful of rows). Store tests assert the planned queries with `EXPLAIN QUERY PLAN` (§9), so removing an index fails a test.

### 4.2 Migrations

Numbered SQL files (`0001_init.sql`, …) are embedded in the binary and applied in order at startup inside a transaction, with the current version kept in `PRAGMA user_version`. **A migration that has been applied is never edited; changes go in a new migration.**

## 5. Scheduling

- The **server is the only authority on scheduling**. The client never computes intervals.
- `internal/srs` wraps go-fsrs with parameters `desired_retention` (from settings), the library's default weights, and short-term (same-day) scheduling enabled. The library is **go-fsrs v4** (FSRS-6), with its default learning steps (1m, 10m) and relearning step (10m), and fuzz on. Observed intervals with retention 0.90 and default weights:
  - **New card:** Again 1m · Hard 6m · Good 10m · Easy 8d.
  - **Learning card after one Good** (at its due time): Again 1m · Hard 6m · Good 2d · Easy 4d. A second Good graduates it to Review.
  - **Review card 10 days after its last review:** Again 10m (to Relearning) · Hard 1mo · Good 1.5mo · Easy 2.5mo.
  - **Relearning card** (at its due time): Again 10m · Hard 15m · Good 1d · Easy 3d.
- The clock is **injected** (`interface{ Now() time.Time }`), so every time-dependent behaviour can be tested deterministically.

### 5.1 Study queue (for one deck, at time `now`)

`dayStart` = the most recent `day_rollover_hour` (default 04:00) in `TZ` at or before `now`.

1. **Learning/Relearning** cards (`state IN (1,3)`) with `due <= now`, oldest due first.
2. **Review** cards (`state = 2`) with `due <= now`, oldest due first.
3. **New** cards (`state = 0`), lowest `id` first, limited to `max(0, new_per_day − newIntroducedToday)`.
4. **Learn-ahead:** if 1–3 are empty and a Learning/Relearning card is due within 20 minutes, show it now.
5. Otherwise the deck is **done for now**, and the response includes the next due time (if any) for display.

*Update 2026-11-14:* two-sided cards move scheduling into a `review_states` table (one row per direction) and change the queue, the API and the editor; see [2026-11-14-two-sided-cards-design.md](2026-11-14-two-sided-cards-design.md).

### 5.2 Answering

- **Request:** `POST /api/cards/{id}/review` with `{rating: 1..4, expectedReps, durationMs}`. `durationMs` is clamped to 0..10 minutes on the server.
- **Duplicate guard:** if `expectedReps != cards.reps`, the server returns **409**. This stops a double-tap, or two open devices, from recording the same review twice.
- **On success:** in one transaction, insert the `review_logs` row (with the full prior state), update the card's FSRS state, and set `updated_at`.

### 5.3 Undo

- **Request:** `POST /api/cards/{id}/undo` with `{expectedReps}`. It loads the card's latest log, restores every `*_before` field, and deletes that log, all in one transaction.
- **Conflict:** returns 409 if `expectedReps` does not match.
- **What the UI offers:** undo for the most recently answered card only.

### 5.4 Editing content

Changing `front_md` or `back_md` **does not reset scheduling**. A content fix is not a new card.

## 6. HTTP API

JSON only, under `/api/`. Every error looks like `{"error": {"code": "<snake_case>", "message": "<human text>"}}`.

| Method & path | Body → Response |
|---|---|
| `GET /api/decks` | → `[{id, name, newPerDay, counts: {new, learning, review}}]`, where `new` is capped by today's remaining limit |
| `POST /api/decks` | `{name}` → deck; 409 `conflict` |
| `GET /api/decks/{id}` | → one deck, same shape as a list item |
| `PATCH /api/decks/{id}` | `{name?, newPerDay?}` → deck |
| `DELETE /api/decks/{id}` | → 204 (cascades to cards and logs) |
| `GET /api/decks/{id}/cards` | → `[{id, front, back, state, due}]` |
| `POST /api/decks/{id}/cards` | `{front, back}` → card |
| `GET /api/cards/{id}` | → card |
| `PATCH /api/cards/{id}` | `{front?, back?}` → card |
| `DELETE /api/cards/{id}` | → 204 |
| `GET /api/decks/{id}/next` | → `{card, reps, previews: {again, hard, good, easy}, remaining: {new, learning, review}}` or `{done: true, nextDueAt?}`. Each preview is `{due, intervalLabel}`. |
| `POST /api/cards/{id}/review` | see §5.2 → `{card}` |
| `POST /api/cards/{id}/undo` | see §5.3 → `{card}` |
| `GET /api/health` | → `{version, lastBackupAt}` or 503 `unavailable`. Exempt from passphrase auth (server-deploy spec §4.6). |
| `GET /api/settings`, `PATCH /api/settings` | `{desiredRetention: 0.70..0.99, dayRolloverHour: 0..23}` |
| `GET /api/auth` | → `{enabled, authenticated}`. Always reachable; tells the client whether to show the login screen. |
| `POST /api/login`, `POST /api/logout` | `{passphrase}` → 204; passphrase mode only (§7.3) |

All timestamps in the API are UTC Unix milliseconds (numbers). Unknown `/api/*` paths return a JSON 404, never the SPA.

*Update 2026-11-08:* the Claude connector adds `GET /api/mcp`, `DELETE /api/mcp/batches/{id}` and `DELETE /api/mcp/grants`, and serves its own public routes on a second listener; see §6 of [2026-11-08-mcp-connector-design.md](2026-11-08-mcp-connector-design.md).

*Update 2026-11-14:* two-sided cards move scheduling into a `review_states` table (one row per direction) and change the queue, the API and the editor; see [2026-11-14-two-sided-cards-design.md](2026-11-14-two-sided-cards-design.md).

**Validation (at the API boundary, server-side, always):**
- Request bodies are capped at **1 MB** with `http.MaxBytesReader`, and unknown JSON fields are rejected.
- **Why 1 MB:** the largest legitimate request is a card create or update: 2 × 6,000 code points × at most 4 UTF-8 bytes = 48 KB, under 75 KB even with worst-case JSON escaping. 1 MB is about 14× headroom.
- Deck name: trimmed, 1–100 code points.
- Card sides: 1–**6,000** code points (10× the current largest side of 600), after normalisation: `\r\n` → `\n`, leading **blank lines** removed, trailing whitespace removed. The indentation of the first non-blank line is **kept**, because it may be code.
- Path IDs: positive integers.
- **Status codes:** 400 invalid input, 404 not found, 409 conflict, 413 body too large, 415 wrong content type, 403 for a rejected Host/Origin, 401 when passphrase mode is on and there is no valid session, 500 with a generic message (details only in the server log).

**Server hardening:** `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` and `IdleTimeout` are set; graceful shutdown on Ctrl-C; structured logging with `log/slog`. Card content is never written to the logs.

## 7. Security

**Threat model:** a single trusted user, with the server private on the tailnet for now. The realistic attackers are (a) **other websites open in Marc's browser** reaching the local server through cross-site requests or DNS rebinding, (b) **content in cards** (pasted from the web) running script when rendered, and later (c) **the open internet**, once the app is on a VM.

*Update 2026-11-05:* for the VM threat model and controls, see §8 of [2026-11-05-server-deploy-design.md](2026-11-05-server-deploy-design.md).

*Update 2026-11-08:* the Claude connector (MCP) puts a few routes on the internet under two written exceptions to §7.1 and §7.4; see §7 of [2026-11-08-mcp-connector-design.md](2026-11-08-mcp-connector-design.md).

### 7.1 Network and request origin
- The server binds to `127.0.0.1` by default. In phase 1, iPhone access is through `tailscale serve`, which forwards HTTPS on the tailnet to the local port with a valid certificate. Nothing listens on the LAN or the internet.
- **Host allowlist:** every request whose `Host` is not in `ALLOWED_HOSTS` gets **403**. This defeats DNS rebinding.
- **Requests that change data** (POST, PATCH, DELETE) must have `Content-Type: application/json` (otherwise 415). They must also pass a fetch-metadata check: `Sec-Fetch-Site` is `same-origin` or `none`, or, when that header is absent, `Origin` matches an allowed host. Together these block cross-site form posts and cross-site `fetch`.

### 7.2 Content rendering (XSS)
- Card Markdown is rendered **only** by the `<Markdown>` component: `markdown-it` with `html: false`, `linkify: false`, and only the `http`, `https` and `mailto` link schemes, then **DOMPurify** on the output as a second layer.
- `dangerouslySetInnerHTML` is used **nowhere else** in the codebase.
- highlight.js output goes through the same sanitiser.

### 7.3 Passphrase mode (built now, off unless `PASSPHRASE` is set)
- When it's on, every route except `/api/login` and the login page's static assets requires a valid session.
- **Login:** the passphrase is compared in constant time (`crypto/subtle`) against a SHA-256 of both values. Login is rate-limited in memory to **5 attempts per minute** in total (single user, so there is no need to track per-IP).
- **Session:** a random 256-bit token from `crypto/rand`, stored as its SHA-256 hash, valid for 30 days. It is sent as cookie `session` with `HttpOnly; Secure; SameSite=Strict; Path=/`.
- **Logout** deletes the session row.
- The passphrase is read **only** from the environment, never written to disk or logs, and the server refuses to start with `PASSPHRASE` shorter than 16 characters.

### 7.4 Other controls
- **SQL injection:** all SQL uses `?` parameters. Identifiers that vary (e.g. a sort column) come only from fixed lists in code, never from request data.
- **Response headers:** `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`. HSTS only in passphrase mode behind TLS. Never loosen the CSP. Dynamic values (e.g. code column width) are set as CSS custom properties through `element.style.setProperty`, which the CSP allows.
- **Dependencies:** exact pinned versions with committed lockfiles (`go.sum`, `package-lock.json`). `govulncheck ./...` and `npm audit --omit=dev` must be clean before a milestone is called done.
- **Repository hygiene:** no secrets, `.env` files or `.db`/`-wal`/`-shm` files are committed.

## 8. Frontend

### 8.1 Screens (React Router)
| Route | Screen |
|---|---|
| `/` | **Decks:** each deck's name with its learning, review and new counts, a **Study** button, and **New deck**. A click anywhere on a deck's row (other than Study) opens the deck. |
| `/decks/:id/study` | **Study** (§8.2). |
| `/decks/:id` | **Deck view:** card previews with a client-side filter box. From 600px wide: a grid of fixed-size tiles (two columns, three from 900px) showing the rendered front above the rendered back, clipped with a fade. Narrower: one row per card showing the first line of the front with inline Markdown only (code spans formatted, block syntax left as text). Clicking a card opens it in the card editor's Preview tab; deck settings (rename, new cards per day, delete). Deleting a deck requires typing its name. |
| `/decks/:id/cards/new`, `/cards/:id/edit` | **Card editor:** front and back Markdown text boxes with a live preview through the same `<Markdown>` component. **Edit / Preview** tabs: an existing card opens in Preview (rendered front and back only), a new card opens in Edit. In Edit, the text boxes and live preview sit side by side from 900px wide, on one grid: the preview's front face is level with the Front box and its back face spans the Back label and box, so the two columns line up (a taller face stretches its box to match) (narrower, each pane would be too cramped for code); below that Edit shows only the text boxes. Character counters show the 6,000 limit. Saving an existing card stays on the page and shows a brief **✓ Saved** note; adding a new card clears the form for the next one. Delete card (with confirmation). |
| `/settings` | **Scheduling** (server, shared): desired retention and rollover hour. **Display** (this device only, `localStorage`): code wrap mode and code column width. |
| `/help` | **Help:** how studying works and the keyboard shortcuts (study keys, Ctrl/⌘+Enter to save in the editor). Other screens do not print shortcut hints. |

**Every screen:** a header pinned to the top (Home, Decks, Settings, Help, and a light/dark toggle). The theme follows `prefers-color-scheme` until the toggle is used; the choice is then kept on this device (`localStorage`). While a screen's data is loading it shows nothing that depends on the data, and a screen with several sources (Settings) renders once all have arrived, so nothing shifts after it appears (never "No decks" before the list arrives); a spinner fades in only if loading takes longer than ~0.3 s.

*Update 2026-11-14:* two-sided cards move scheduling into a `review_states` table (one row per direction) and change the queue, the API and the editor; see [2026-11-14-two-sided-cards-design.md](2026-11-14-two-sided-cards-design.md).

### 8.2 Study screen
- The front is shown, then **Show answer** reveals the back below a dashed divider on a slightly darker surface, with the front still visible (the same look as the deck view's tiles). Each side is at least as tall as the editor's text boxes.
- Then four buttons, each labelled with its interval (e.g. **Good · 3d**), plus **Undo** for the last answer.
- **Keyboard:** `Space` reveals / rates Good, `1`–`4` rate, `Z` undoes.
- **Phone:** the action bar is pinned to the bottom, tap targets are at least 44 px, and `env(safe-area-inset-bottom)` is respected.
- Time on the card (`durationMs`) is measured from when the front is shown until the answer is given.
- On a 409, the screen quietly reloads `/next`. On any other error it shows a visible message with a retry option.

### 8.3 Card rendering rules
- **Prose is centred.** Inline code (`` `std::vector<T>` ``) flows within the centred text, with a subtle background.
- **Fenced code blocks** sit as a centred block in the card, with their **contents left-aligned** and whitespace preserved exactly.
- **Lists** work the same way: the list block is centred and its items are left-aligned.
- **Code width:** code blocks are at most **N columns** wide (`ch` units), with N = 80 by default and a per-device setting.
- **When the screen is narrower than N columns:**
  - **Scroll** (default on wide screens): horizontal scrolling inside the block.
  - **Wrap** (default on narrow screens, i.e. below 600 px wide): lines wrap with a hanging indent and a visible **↪** marker on each continuation line, so a soft wrap is never mistaken for a real line break. The marker is not part of the text if you copy it.
- **Highlighting:** highlight.js with `cpp`, `c`, `python`, `bash` and `json` registered, plus their aliases (e.g. `c++`). An untagged fence or an unknown language is plain escaped text and never an error. Token colours come from our own stylesheet with light and dark palettes, following the app theme (`prefers-color-scheme` unless overridden by the header toggle).
- **Fonts:** the system UI font for prose and a system monospace stack for code. No web fonts are fetched, which keeps the CSP strict.

### 8.4 Installable app (web app manifest)
`manifest.webmanifest` with `display: standalone`, name, theme colour, and icons (including `apple-touch-icon`), plus the `apple-mobile-web-app-capable` meta tags, so **Add to Home Screen** on iOS opens it full screen without Safari's address bar. No service worker.

### 8.5 Client structure
- **API:** one typed API client module (`web/src/api/`) is the only place `fetch` is called. Its TypeScript types mirror the API table in §6.
- **Data loading:** no state-management or data-fetching library; components use small hooks on top of the client.
- **TypeScript:** `strict` mode, and `noUncheckedIndexedAccess` is on.

## 9. Testing and verification

**Approach:** TDD where it makes sense. **Test-first by default on the backend.** Test-first on the frontend wherever there is logic (Markdown/renderer, wrap mode, API client, study-flow state). Layout and visual polish are checked with Playwright at both screen sizes rather than written test-first.

| Area | What is tested |
|---|---|
| `internal/srs` | Queue order (§5.1), daily new limit, the rollover-hour boundary (03:59 vs 04:00 in `TZ`), learn-ahead, previews for all four ratings, all with a fake clock. |
| `internal/store` | Migrations from empty; **cascade delete** (a deck takes its cards and logs with it); review-and-undo leaves the card exactly as before; `EXPLAIN QUERY PLAN` shows the planned index for the queue and count queries. All against a temporary SQLite file. |
| `internal/api` | `httptest` covering validation limits (6,000 / 6,001 code points, a 1 MB body), the 409 duplicate guard, Host rejection, cross-site `Sec-Fetch-Site` rejection, 415 for a non-JSON mutation, security headers present, passphrase mode on and off, and the login rate limit. |
| `import` subcommand | Valid file creates the deck and cards; duplicate deck name is refused; invalid card (empty or over 6,000) aborts the whole import (single transaction). |
| `web` (Vitest + RTL) | `<Markdown>`: `<script>`, `onerror=`, and `javascript:` links are neutralised; fences are highlighted; inline code; centred prose vs. left-aligned blocks; wrap marker. API client error handling. Study flow: reveal, rate, 409 reload, undo. |
| Playwright smoke | Against the real binary with a temporary DB at **iPhone 15** and **1440×900** sizes: create deck, add a card with a code fence, study it, rate Good. Assert the code block has no page-level horizontal overflow in wrap mode. |

**The "done" bar for any milestone:** `gofmt -l` is empty; `go vet ./...`, `staticcheck ./...`, `go test ./...` and `govulncheck ./...` pass; `npm run lint`, `npm run typecheck`, `npm test`, `npm run build` and `npm audit --omit=dev` pass; the Playwright smoke test passes. **Command output is shown, not just summarised.**

## 10. Seed import and the content-cleanup handoff

- **Cleanup happens before import, as a separate task** (another Claude instance). It reads the Anki export `Cpp.xml` and writes the cleaned file `data/cpp.json`, which is committed.
  - **About the XML:** the export is *not* well-formed XML because `<`, `>` and `&` inside card text are not escaped (e.g. `std::vector<double>`), so strict XML parsers reject it. The structure is regular, though: `<card>` contains `<text name='Front'>`, `<code name='Code Both Sides'>`, `<text name='Back'>` and `<code name='Code Back Only'>`. Both `code` fields are empty on all 94 cards, so all code is mixed into the plain text without markers.
  - **What the cleanup does:** convert each side to Markdown (fences tagged `cpp` for multi-line code, backticks for inline identifiers and types). The cleanup may also fix source errors (e.g. card 25's `{ c ];`).
- **The only contract between that task and this app is the seed format:**

```json
{
  "deck": "Cpp",
  "cards": [
    { "front": "<markdown>", "back": "<markdown>" }
  ]
}
```

- `anki-app import data/cpp.json` creates the deck and all cards as **New** in one transaction. It refuses if the deck name already exists and applies the same validation as the API (§6). The import is a one-off command-line operation, not an HTTP endpoint.

## 11. Git and workflow

- `git init -b main` locally. *Update 2026-11-05:* there is now a private GitHub remote, and CI runs on push to `main` only (server-deploy spec §6).
- `.gitignore` covers `node_modules/`, `server/internal/webui/dist/` (except a `.gitkeep`), binaries, `*.db`, `*.db-wal`, `*.db-shm`, `.env*`, `.claude/settings.local.json`, and Playwright output.
- Each milestone gets its own branch (`feat/<name>`), merged into `main` locally once the done bar (§9) passes. Commits are small and focused, with imperative-mood messages.

## 12. Deployment

Replaced on 2026-11-05 by [2026-11-05-server-deploy-design.md](2026-11-05-server-deploy-design.md): a Docker image built by GitHub Actions, pulled and deployed by a Google Cloud VM. The Windows laptop deployment (`scripts/deploy.sh`, `scripts/kill.sh`, `scripts/lib/winproc.sh`) is removed in that spec's last milestone. Until then it still works as described in git history (this section before 2026-11-05).

## Appendix A: root `CLAUDE.md` (draft)

```markdown
# anki-app

Personal spaced-repetition flashcard app for code-heavy cards. Single user.
Go backend (`server/`) + React/TypeScript frontend (`web/`), SQLite, FSRS.
Design spec: docs/specs/2026-11-02-flashcards-design.md. Read it before changing behaviour.

## Commands
- Backend tests: `cd server && go test ./...`
- Frontend tests: `cd web && npm test`
- Dev: `cd server && go run ./cmd/anki-app serve` + `cd web && npm run dev`
- Build: `cd web && npm run build && cd ../server && go build ./cmd/anki-app`

## Non-negotiable security rules
- Never change the default listen address away from 127.0.0.1. Never weaken the Host allowlist, the
  Sec-Fetch-Site/Origin check, the JSON content-type requirement, or the CSP to make something work. Fix the cause.
- SQL: `?` parameters only. Never build SQL from request data. Identifiers that vary come from fixed lists in code.
- Card content is rendered ONLY through `<Markdown>` (markdown-it html:false + DOMPurify).
  `dangerouslySetInnerHTML` is forbidden everywhere else.
- Validate every input at the API boundary (types, ranges, the 6,000 code point limit per side).
- Never log card content, passphrases, or session tokens. No secrets, .env, or .db files in git.
- New dependencies need a one-line justification in the commit message, exact pinned versions, and committed lockfiles.

## Working rules
- TDD where it makes sense: test-first by default on the backend; on the frontend for logic (renderer,
  API client, study flow). Layout is checked with Playwright at iPhone and desktop sizes.
- Before saying anything is done: run the full done bar (spec §9) and show the output. If something fails, say so.
- Migrations: never edit an applied migration; add a new numbered one.
- Indexes: every new query gets an `EXPLAIN QUERY PLAN` check in a store test. Add an index only when a real query
  needs it, and write down which query in the migration.
- Keep units small with one clear job. If a file grows past ~300 lines, consider splitting it.
- Out of scope unless Marc asks: accounts, offline mode, search, media, moving cards between decks.
- Work on `feat/<name>` branches; merge to main only when the done bar passes.
```

## Appendix B: `server/CLAUDE.md` (draft)

```markdown
# server (Go)

- Standard library `net/http` routing only; no web framework.
- Package boundaries: SQL lives only in `internal/store`; go-fsrs is imported only in `internal/srs`;
  handlers in `internal/api` are thin (decode → validate → call → encode).
- Multi-statement writes run in one transaction (`store.WithTx`).
- Time comes from the injected clock, never `time.Now()` directly in logic. Store times as UTC unix ms.
- Errors: wrap with `fmt.Errorf("...: %w", err)`; map them to API error codes in one place; never return
  internal error text to clients.
- Pass `context.Context` through; no package-level mutable state.
- Every connection: `PRAGMA foreign_keys=ON`, WAL, busy_timeout (set in one place in `store.Open`).
- Count text length in code points (`utf8.RuneCountInString`), matching the frontend.
- Checks: `gofmt -l .` (empty), `go vet ./...`, `staticcheck ./...`, `go test ./...`, `govulncheck ./...`.
```

## Appendix C: `web/CLAUDE.md` (draft)

```markdown
# web (React + TypeScript)

- TypeScript strict + noUncheckedIndexedAccess. No `any`, no `@ts-ignore` without a comment explaining why.
- `src/api/` is the only place that calls `fetch`. Its types mirror the API table in spec §6.
- `<Markdown>` is the only HTML renderer. Never use dangerouslySetInnerHTML elsewhere.
- No inline scripts and no inline styles that need CSP changes. No external fonts, CDNs, or analytics.
- Count characters in code points (`[...s].length`), matching the server.
- Accessibility: real <button>s, tap targets of at least 44px, keyboard shortcuts on the study screen, visible focus.
- Device-only display settings live in localStorage behind a small wrapper with try/catch and defaults.
- Check layout at iPhone 15 and desktop sizes (Playwright) before saying UI work is done.
- Checks: `npm run lint`, `npm run typecheck`, `npm test`, `npm run build`, `npm audit --omit=dev`.
```
