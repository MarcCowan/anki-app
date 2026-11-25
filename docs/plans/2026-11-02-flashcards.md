# Flashcards App Implementation Plan

Status: done

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a single-user spaced-repetition flashcard web app (Go backend + React frontend + SQLite + FSRS) that renders code-heavy Markdown cards well on a laptop and an iPhone.

**Architecture:**
- **One Go binary** serves a JSON API under `/api/` and an embedded React SPA everywhere else.
- **SQLite** holds decks, cards (with their FSRS state), review logs, settings and sessions.
- **Scheduling** is computed only on the server: go-fsrs is wrapped in a pure `srs` package and orchestrated by a `study` package.
- **Card Markdown** is rendered only in the browser, by one sanitised `<Markdown>` component.

**Tech Stack:**
- **Go:** current stable, standard library `net/http`, `modernc.org/sqlite`, `github.com/open-spaced-repetition/go-fsrs` (latest major).
- **Web:** Node LTS, React + TypeScript (strict), Vite, React Router, markdown-it, highlight.js, DOMPurify, Vitest + React Testing Library, Playwright, ESLint (typescript-eslint, eslint-plugin-react, eslint-plugin-react-hooks).

**Spec:** `docs/specs/2026-11-02-flashcards-design.md`. **Read it in full before Task 1.** Section references (§n) below point into it. Where this plan and the spec disagree, the spec wins; stop and report the conflict.

## How to run this plan

- **Platform:** Windows 11, with Git Bash available as `bash`. All shell commands below are bash. The Go server must also build on Linux (future VM), so never use Windows-only APIs.
- **Milestones and branches:** for each milestone, create the branch from `main`, do its tasks in order, run `scripts/check.sh` (the done bar, spec §9), and show its output. Then merge to `main` with `git merge --no-ff` and delete the branch. **Never** push; there is no remote.

| Milestone | Branch | Tasks |
|---|---|---|
| M0 Scaffolding | `feat/scaffold` | 1 |
| M1 Backend | `feat/backend` | 2–10 |
| M2 Frontend | `feat/frontend` | 11–16 |
| M3 Passphrase mode | `feat/passphrase` | 17–18 |
| M4 E2E + phone access | `feat/e2e` | 19–20 |

- **Commits:** every commit message ends with the attribution line the harness gives you. When a commit adds a dependency, give the reason in one line in the message (root CLAUDE.md rule).
- **Not in this plan:** cleaning `Cpp.xml` into `data/cpp.json`. That is a separate task (spec §10). Task 20 only explains how to import the file once it exists.

## Global Constraints

- **Listen address:** default `ADDR` is `127.0.0.1:8080`. Never default to `0.0.0.0` or `:8080`.
- **Card sides:** 1–6000 code points after normalisation (`\r\n`→`\n`, drop leading blank lines, trim trailing whitespace, **keep** first-line indentation).
- **Deck names:** trimmed (`strings.TrimSpace`), 1–100 code points. `new_per_day` is 0–9999, default 20.
- **Settings:** `desiredRetention` 0.70–0.99 (default 0.90); `dayRolloverHour` 0–23 (default 4).
- **Request bodies:** at most 1 MB (`1 << 20` bytes); unknown JSON fields rejected; `durationMs` clamped to 0–600000.
- **Times:** stored as UTC Unix milliseconds `INTEGER`, and sent in the API as Unix-ms numbers.
- **Every SQLite connection** has `foreign_keys=ON`, `journal_mode=WAL` and `busy_timeout=5000`, with `db.SetMaxOpenConns(1)`.
- **Passphrase:** if set, it must be at least 16 characters, or startup fails. Login is limited to 5 attempts per minute in total. Sessions use 256-bit tokens stored as SHA-256 and last 30 days. Cookie: `session`, `HttpOnly; Secure; SameSite=Strict; Path=/`.
- **CSP (exact):** `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`. Also `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.
- **No external network resources at runtime:** no web fonts, CDNs or analytics.
- **`dangerouslySetInnerHTML`** may appear only in `web/src/components/Markdown.tsx`, and ESLint enforces it.
- **Package boundaries:** SQL lives only in `server/internal/store`; go-fsrs is imported only in `server/internal/srs`. An architecture test enforces both.
- **Character counting:** in code points, `utf8.RuneCountInString` in Go and `[...s].length` in TypeScript.
- **Code display:** the column width defaults to 80 (allowed range 40–200). Wrap mode is `auto | scroll | wrap`, where `auto` means wrap below 600 px viewport width.

## Review Focus

These are failure modes the spec implies but doesn't spell out as tests. Each one has a test in the task named.

1. **Card text that begins with indented code** (e.g. `    int& ref = x;` as the first line). Normalisation must not strip that indentation. → Task 2, `TestNormalizeSideKeepsFirstLineIndent`.
2. **Fence languages written in other ways** (` ```c++ `, ` ```C++ `, ` ```text `, ` ```madeup `, no tag). Each renders as highlighted or as plain escaped text, and never throws or leaves the code out. → Task 11, `highlight.test.ts` "aliases and unknown languages".
3. **A very long unbroken token in wrap mode** (a 200-character template type, or a URL in prose) on an iPhone. The page must not scroll sideways. → Task 19, smoke test "no horizontal page overflow".
4. **Day rollover on a daylight-saving change day**, and `TZ` unset on Windows (Go only reads `TZ` on Unix, and Windows has no zoneinfo database). → Task 2 (`config` loads `TZ` with `time/tzdata` embedded) and Task 7, `TestDayStartAcrossDST`.
5. **A card or deck deleted on one device while it's being studied on the other.** The review or undo returns 404, and the study screen fetches the next card instead of getting stuck on an error. → Task 15, reducer test "404 on answer loads next".

---

## M0: Scaffolding (`git switch -c feat/scaffold`)

### Task 1: Toolchain, repo skeleton, CLAUDE.md files, check script, architecture test

**Files:**
- Create: `CLAUDE.md`, `server/CLAUDE.md`, `web/CLAUDE.md` (text from spec Appendices A–C, with the *Commands* section updated to match the scripts below)
- Create: `server/go.mod`, `server/cmd/anki-app/main.go` (temporary: prints `anki-app` and exits 0), `server/internal/webui/dist/.gitkeep`
- Create: `server/internal/archtest/arch_test.go`
- Create: `web/` (Vite React-TS scaffold), `web/eslint.config.js`, `web/vitest.config.ts` (or a `test` block in `vite.config.ts`), `web/src/test/setup.ts`
- Create: `scripts/check.sh`, `scripts/build.sh`
- Modify: `.gitignore` (add `web/dist/` and `server/anki-app.exe` if not already covered)

**Interfaces:**
- Produces: `scripts/check.sh`, which runs the full done bar and exits non-zero on the first failure. `scripts/build.sh`, which builds the web app into `server/internal/webui/dist/` and then `server/anki-app(.exe)`. These npm scripts in `web/package.json`: `dev`, `build`, `lint`, `typecheck`, `test`, `e2e`.

- [ ] **Step 1: Install the toolchain**

```bash
winget install --id GoLang.Go -e
winget install --id OpenJS.NodeJS.LTS -e
winget install --id Tailscale.Tailscale -e
```

Open a new shell, then confirm with `go version` (current stable), `node -v` (current LTS, ≥ 22), and `tailscale version`. If winget needs elevation or interaction, stop and ask Marc to run the command with the `!` prefix.

- [ ] **Step 2: Create the Go module**

In `server/`, run `go mod init anki-app/server`. Set the `go` directive to the installed version. Add the linters as pinned tool dependencies:

```bash
cd server
go get -tool honnef.co/go/tools/cmd/staticcheck@latest
go get -tool golang.org/x/vuln/cmd/govulncheck@latest
```

From then on they run as `go tool staticcheck ./...` and `go tool govulncheck ./...`, with the versions recorded in `go.mod`.

- [ ] **Step 3: Write the architecture test (it should pass trivially for now)**

`server/internal/archtest/arch_test.go` walks every non-test `.go` file under `server/` with `go/parser` (`parser.ImportsOnly`) and fails if:
- any file outside `internal/store/` imports `database/sql` or `modernc.org/sqlite`, or
- any file outside `internal/srs/` imports a path starting with `github.com/open-spaced-repetition/go-fsrs`.

```go
func TestOnlyStoreImportsSQL(t *testing.T)
func TestOnlySrsImportsFSRS(t *testing.T)
```

It locates the module root by walking up from the test's working directory until it finds `go.mod`. Run `go test ./internal/archtest/`. Expected: PASS.

- [ ] **Step 4: Scaffold the web app**

```bash
npm create vite@latest web -- --template react-ts
cd web && npm install
```

Then:
- Install the runtime dependencies `react-router markdown-it highlight.js dompurify`.
- Install the dev dependencies `@types/markdown-it vitest jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom @playwright/test eslint-plugin-react`. Keep the template's `eslint-plugin-react-hooks` and `typescript-eslint`.
- Pin exact versions: set `save-exact=true` in `web/.npmrc` before installing, and strip any `^` already added.
- Add `"engines": {"node": ">=22"}`.

Configure the scaffold:
- **`tsconfig.app.json`:** `"strict": true` and `"noUncheckedIndexedAccess": true`.
- **`vite.config.ts`:**
  - `build.outDir: '../server/internal/webui/dist'` and `build.emptyOutDir: true`.
  - `server.proxy: { '/api': 'http://127.0.0.1:8080' }`.
  - `test: { environment: 'jsdom', setupFiles: './src/test/setup.ts' }`. The setup file imports `@testing-library/jest-dom/vitest`.
- **`package.json` scripts:**
  - `"build": "tsc -b && vite build && node -e \"require('fs').writeFileSync('../server/internal/webui/dist/.gitkeep','')\""`. This recreates `.gitkeep` after `emptyOutDir` wipes it.
  - `"typecheck": "tsc -b --noEmit"`, `"test": "vitest run"`, `"lint": "eslint ."`, `"e2e": "playwright test"`.
- **`eslint.config.js`:**
  - Enable `react/no-danger: 'error'` for `src/**`, with an override turning it off **only** for `src/components/Markdown.tsx`.
  - `@typescript-eslint/no-explicit-any: 'error'`.
  - The react-hooks recommended rules.
- Delete the template's demo content (counter, logos, `App.css`). Leave a minimal `App.tsx` that renders `<h1>anki-app</h1>`.

- [ ] **Step 5: Write `scripts/check.sh` (the done bar, spec §9)**

`set -euo pipefail`. Run in order:
- **In `server/`:** `test -z "$(gofmt -l .)"`, `go vet ./...`, `go tool staticcheck ./...`, `go test ./...`, `go tool govulncheck ./...`.
- **In `web/`:** `npm run lint`, `npm run typecheck`, `npm test`, `npm run build`, `npm audit --omit=dev`.
- **Then** `npm run e2e`, but only if `web/playwright.config.ts` exists (Task 19 adds it).

Print a banner before each step so a failure is easy to find.

`scripts/build.sh` runs `npm run build` in `web/`, then `go build -o anki-app$( [ "$OS" = "Windows_NT" ] && echo .exe ) ./cmd/anki-app` in `server/`.

- [ ] **Step 6: Write the three CLAUDE.md files**

Copy them from spec Appendices A, B and C. Update the root file's *Commands* to `scripts/check.sh` (full done bar), `scripts/build.sh`, and the dev commands. In `web/CLAUDE.md`, replace any mention of Shiki with highlight.js.

- [ ] **Step 7: Verify**

Run `bash scripts/check.sh`. Expected: every step passes (a few tests, the build succeeds, audit is clean). Show the output.

- [ ] **Step 8: Commit and merge M0**

```bash
git add -A && git commit -m "chore: scaffold Go module, Vite app, CLAUDE.md files and check script"
git switch main && git merge --no-ff feat/scaffold && git branch -d feat/scaffold
```

---

## M1: Backend (`git switch -c feat/backend`)

All backend tests use `t.TempDir()` for SQLite files and `clock.Fake` for time. Commit after each task with a `feat(server): …` message.

### Task 2: `clock`, `validate`, `config` packages

**Files:**
- Create: `server/internal/clock/clock.go`, `clock_test.go`
- Create: `server/internal/validate/validate.go`, `validate_test.go`
- Create: `server/internal/config/config.go`, `config_test.go`

**Interfaces:**
- Produces:
  ```go
  package clock
  type Clock interface{ Now() time.Time }
  type Real struct{}                 // Now() returns time.Now()
  type Fake struct{ T time.Time }    // Now() returns T; Advance(d time.Duration) moves T forward
  func (f *Fake) Now() time.Time
  func (f *Fake) Advance(d time.Duration)

  package validate
  const MaxSideRunes = 6000
  const MaxDeckNameRunes = 100
  var ErrInvalid = errors.New("invalid")         // all errors wrap this; message says which field and why
  func NormalizeSide(s string) string
  func Side(field, s string) (string, error)     // normalize, then require 1..MaxSideRunes
  func DeckName(s string) (string, error)        // TrimSpace, then require 1..MaxDeckNameRunes
  func NewPerDay(n int) error                    // 0..9999
  func DesiredRetention(r float64) error         // 0.70..0.99 inclusive
  func RolloverHour(h int) error                 // 0..23

  package config
  type Config struct {
      Addr         string
      DBPath       string
      AllowedHosts []string        // lower-cased hostnames, no ports
      Passphrase   string          // "" = passphrase mode off
      Location     *time.Location
  }
  func Load(getenv func(string) string) (Config, error)
  ```

- [ ] **Step 1: Write failing tests**

```go
func TestFakeAdvance(t *testing.T)                  // Fake{T: t0}; Advance(90*time.Minute); Now() == t0.Add(90m)

func TestNormalizeSideKeepsFirstLineIndent(t *testing.T) // "\n\n    int& r = x;\r\nfoo  \n\n" -> "    int& r = x;\nfoo"
func TestSideLimits(t *testing.T)    // 6000 runes of "→" ok; 6001 -> errors.Is(err, ErrInvalid); "" and " \n\t" -> ErrInvalid
func TestDeckName(t *testing.T)      // "  Cpp  " -> "Cpp"; 100 runes ok; 101 -> ErrInvalid; "   " -> ErrInvalid
func TestRanges(t *testing.T)        // NewPerDay(-1), NewPerDay(10000), DesiredRetention(0.69), DesiredRetention(1.0), RolloverHour(24) all ErrInvalid; boundaries 0, 9999, 0.70, 0.99, 0, 23 ok

func TestLoadDefaults(t *testing.T)  // empty env: Addr "127.0.0.1:8080", DBPath "anki-app.db", AllowedHosts ["localhost","127.0.0.1"], Passphrase "", Location time.Local
func TestLoadAllowedHosts(t *testing.T) // " Laptop.tail1.ts.net , localhost " -> ["laptop.tail1.ts.net","localhost"]
func TestLoadShortPassphrase(t *testing.T) // 15 chars -> error mentioning PASSPHRASE; 16 chars ok
func TestLoadTZ(t *testing.T)        // TZ="Europe/London" -> Location.String()=="Europe/London"; TZ="Nope/Nowhere" -> error
```

- [ ] **Step 2: Run `cd server && go test ./internal/clock/ ./internal/validate/ ./internal/config/`.** Expected: FAIL (undefined symbols).
- [ ] **Step 3: Implement.** `config` imports `_ "time/tzdata"` so `time.LoadLocation` works on Windows. When `TZ` is unset it uses `time.Local`. `NormalizeSide` order: replace `\r\n` with `\n`, drop leading lines that contain only whitespace, then `strings.TrimRightFunc(s, unicode.IsSpace)`.
- [ ] **Step 4: Run the same command.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 3: `store`: open, migrations, decks

**Files:**
- Create: `server/internal/store/store.go` (Open, Close, sentinel errors, time helpers)
- Create: `server/internal/store/migrate.go`, `server/internal/store/migrations/0001_init.sql`
- Create: `server/internal/store/decks.go`
- Test: `server/internal/store/store_test.go`, `decks_test.go`

**Interfaces:**
- Consumes: `validate` is **not** used here. The store trusts its callers, and validation happens in `api` and in `import`.
- Produces:
  ```go
  package store
  var (
      ErrNotFound = errors.New("not found")
      ErrConflict = errors.New("conflict")   // unique-name clash
      ErrStale    = errors.New("stale")      // expectedReps mismatch
  )
  type Store struct{ /* *sql.DB */ }
  func Open(ctx context.Context, path string) (*Store, error)
  func (s *Store) Close() error
  type Deck struct {
      ID        int64
      Name      string
      NewPerDay int
      CreatedAt time.Time
  }
  func (s *Store) CreateDeck(ctx context.Context, name string, now time.Time) (Deck, error)   // ErrConflict on duplicate name
  func (s *Store) GetDeck(ctx context.Context, id int64) (Deck, error)                       // ErrNotFound
  func (s *Store) ListDecks(ctx context.Context) ([]Deck, error)                             // ORDER BY name COLLATE NOCASE
  func (s *Store) UpdateDeck(ctx context.Context, id int64, name *string, newPerDay *int) (Deck, error) // ErrNotFound, ErrConflict
  func (s *Store) DeleteDeck(ctx context.Context, id int64) error                            // ErrNotFound
  ```

- [ ] **Step 1: Write failing tests**

```go
func TestOpenAppliesPragmas(t *testing.T)    // PRAGMA foreign_keys == 1, journal_mode == "wal", busy_timeout == 5000
func TestOpenIsIdempotent(t *testing.T)      // Open, Close, Open same path: no error; PRAGMA user_version == 1
func TestSettingsSeeded(t *testing.T)        // SELECT value FROM settings: desired_retention "0.9", day_rollover_hour "4" (raw SQL in test is fine; tests are in package store)
func TestDeckCRUD(t *testing.T)              // create "Cpp" -> ID>0, NewPerDay 20; Get matches; Update name "C++"; List returns 1; Delete; Get -> ErrNotFound
func TestDeckDuplicateName(t *testing.T)     // second CreateDeck("Cpp") -> errors.Is(err, ErrConflict); UpdateDeck to an existing name -> ErrConflict
```

- [ ] **Step 2: Run `go test ./internal/store/`.** Expected: FAIL.
- [ ] **Step 3: Implement**
  - **`Open`:** use the `modernc.org/sqlite` driver (`"sqlite"`) with the DSN `"file:" + filepath.ToSlash(path) + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"`. `_pragma` params apply to every connection. Call `SetMaxOpenConns(1)` (single user, and it avoids SQLite lock-upgrade `BUSY` errors in deferred transactions). If the DSN form fails on Windows paths, fix it here and add a test with a path that contains a space.
  - **`0001_init.sql`:** the full schema from spec §4, including `sessions`, the two indexes from §4.1, and `INSERT INTO settings` defaults.
  - **`migrate.go`:** `//go:embed migrations/*.sql`. Files are sorted by their numeric prefix. Each file is applied in a transaction that sets `PRAGMA user_version = N`, and files with `N <= user_version` are skipped.
  - **Time:** store `time.Time` as `t.UnixMilli()` and read it back with `time.UnixMilli(v).UTC()`.
  - **Duplicate names:** map the SQLite unique-constraint error to `ErrConflict` by checking for the `sqlite3.SQLITE_CONSTRAINT_UNIQUE` code via `*sqlite.Error`.
- [ ] **Step 4: Run `go test ./internal/store/`.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 4: `srs`: the go-fsrs wrapper

**Files:**
- Create: `server/internal/srs/srs.go`, `server/internal/srs/interval.go`
- Test: `server/internal/srs/srs_test.go`, `interval_test.go`
- Modify: spec §5 (record the observed learning-step behaviour)

**Interfaces:**
- Produces (no go-fsrs type appears in any exported signature):
  ```go
  package srs
  type State int
  const (New State = 0; Learning State = 1; Review State = 2; Relearning State = 3)
  type Rating int
  const (Again Rating = 1; Hard Rating = 2; Good Rating = 3; Easy Rating = 4)
  type CardState struct {
      State                      State
      Due                        time.Time
      Stability, Difficulty      float64
      ElapsedDays, ScheduledDays int64
      Reps, Lapses               int64
      LastReview                 time.Time   // zero = never reviewed
  }
  func NewCardState(now time.Time) CardState                    // State New, Due = now
  type Scheduler struct{ /* fsrs instance */ }
  func NewScheduler(desiredRetention float64) *Scheduler          // default weights, short-term scheduling enabled
  func (s *Scheduler) Next(c CardState, now time.Time, r Rating) CardState
  func (s *Scheduler) Preview(c CardState, now time.Time) map[Rating]CardState   // all four ratings
  func FormatInterval(d time.Duration) string
  ```

- [ ] **Step 1: Write failing tests**

```go
func TestNewCardGoodSchedulesAhead(t *testing.T)   // NewCardState(t0) -> Next(Good): Due.After(t0), Reps==1, State != New
func TestPreviewOrdering(t *testing.T)             // for a New card and for a Review card 10 days old: Due(Again) <= Due(Hard) <= Due(Good) <= Due(Easy)
func TestNextMatchesPreview(t *testing.T)          // Preview(c,now)[Good] == Next(c,now,Good)
func TestAgainOnReviewIsRelearning(t *testing.T)   // take a card to Review, then Again -> State==Relearning, Lapses==1
func TestRoundTripStateFields(t *testing.T)        // converting CardState -> fsrs.Card -> CardState is lossless (zero LastReview preserved)
func TestFormatInterval(t *testing.T)              // table: 30s->"<1m", 1m->"1m", 10m->"10m", 59m->"59m", 1h->"1h", 23h->"23h", 24h->"1d", 30d->"1mo", 45d->"1.5mo", 365d->"1y", 548d->"1.5y"
```

`FormatInterval` rules: `<1m` → `"<1m"`; under 1h → whole minutes + `m`; under 1d → whole hours + `h`; under 30d → whole days + `d`; under 365d → months (days/30) with at most one decimal and no trailing `.0`, + `mo`; otherwise years (days/365) formatted the same way, + `y`. Truncate the minutes, hours and days rather than rounding.

- [ ] **Step 2: Run `go test ./internal/srs/`.** Expected: FAIL.
- [ ] **Step 3: Implement against go-fsrs.**
  - Run `go get github.com/open-spaced-repetition/go-fsrs/v<N>@latest` with the newest major version, and read its `go doc` before writing code.
  - The v3 API looks like `fsrs.DefaultParam()`, `p.RequestRetention`, `p.EnableShortTerm = true`, `fsrs.NewFSRS(p)`, `f.Repeat(card, now)` (returns all four outcomes) and `f.Next(card, now, rating)`. If the latest major version differs, adapt **only** the inside of this package; the exported interface above is fixed.
  - Convert the library's unsigned counters to `int64`.
- [ ] **Step 4: Run `go test ./internal/srs/`.** Expected: PASS.
- [ ] **Step 5: Record the learning-step behaviour.** Write a throwaway test (don't commit it) that prints the `Preview` intervals for a New card and for a Learning card after one Good. Replace the "During planning, check how go-fsrs actually handles learning steps…" sentence in spec §5 with the intervals you observed (e.g. "New: Again 1m · Hard 5m · Good 10m · Easy 2d").
- [ ] **Step 6: Commit** the code and the spec edit.

### Task 5: `store`: cards and queue queries (with index assertions)

**Files:**
- Create: `server/internal/store/cards.go`
- Test: `server/internal/store/cards_test.go`, `server/internal/store/plan_test.go`

**Interfaces:**
- Consumes: `srs.CardState`, `srs.State`, `srs.NewCardState` (Task 4). Store imports `srs` for these types only; `srs` is pure.
- Produces:
  ```go
  type Card struct {
      ID, DeckID           int64
      Front, Back          string
      CreatedAt, UpdatedAt time.Time
      Sched                srs.CardState
  }
  type NewCard struct{ Front, Back string }
  func (s *Store) CreateCard(ctx context.Context, deckID int64, c NewCard, now time.Time) (Card, error)   // ErrNotFound if no deck; Sched = srs.NewCardState(now)
  func (s *Store) GetCard(ctx context.Context, id int64) (Card, error)
  func (s *Store) ListCards(ctx context.Context, deckID int64) ([]Card, error)          // ORDER BY id; ErrNotFound if no deck
  func (s *Store) UpdateCardContent(ctx context.Context, id int64, front, back *string, now time.Time) (Card, error) // never touches Sched
  func (s *Store) DeleteCard(ctx context.Context, id int64) error
  func (s *Store) ImportDeck(ctx context.Context, name string, cards []NewCard, now time.Time) (Deck, error) // one tx; ErrConflict if name exists

  // Queue queries: all use cards_deck_state_due
  func (s *Store) FirstDue(ctx context.Context, deckID int64, states []srs.State, dueAtOrBefore time.Time) (Card, bool, error) // ORDER BY due, id LIMIT 1
  func (s *Store) FirstNew(ctx context.Context, deckID int64) (Card, bool, error)                     // state=0 ORDER BY due, id LIMIT 1
  func (s *Store) CountDue(ctx context.Context, deckID int64, states []srs.State, dueAtOrBefore time.Time) (int, error)
  func (s *Store) CountState(ctx context.Context, deckID int64, st srs.State) (int, error)
  func (s *Store) MinDue(ctx context.Context, deckID int64, states []srs.State) (time.Time, bool, error)
  ```
  The `states` lists are expanded into `IN (?,?)` placeholders. Build the placeholder string from `len(states)`; never interpolate the values themselves.

- [ ] **Step 1: Write failing tests**

```go
func TestCardCRUD(t *testing.T)                 // create in deck; Get; UpdateCardContent(front only) changes Front + UpdatedAt, Sched unchanged; Delete; Get -> ErrNotFound
func TestCreateCardMissingDeck(t *testing.T)    // deckID 999 -> ErrNotFound
func TestDeleteDeckCascades(t *testing.T)       // deck with 2 cards + 1 review_logs row (raw INSERT): DeleteDeck -> SELECT COUNT(*) FROM cards == 0 AND FROM review_logs == 0
func TestNewCardOrder(t *testing.T)             // ImportDeck with 3 cards at the same now: FirstNew returns the lowest id
func TestFirstDueOrderAndFilter(t *testing.T)   // set Sched via raw UPDATE: learning due t-5m, learning due t-10m, review due t-1d, learning due t+5m -> FirstDue([Learning,Relearning], t) is the t-10m one; CountDue(...)==2
func TestImportDeckAtomic(t *testing.T)         // ImportDeck("Cpp", 2 cards) ok; ImportDeck("Cpp", ...) -> ErrConflict and card count still 2
func TestMinDue(t *testing.T)                   // returns the smallest due among given states; false when none
```

Plan assertions (`plan_test.go`): one helper `explain(t, s, query string, args ...any) string` that joins the `detail` column of `EXPLAIN QUERY PLAN`.

```go
func TestQueuePlansUseIndex(t *testing.T)   // for the exact SQL strings used by FirstDue, FirstNew, CountDue, CountState, MinDue:
                                            // strings.Contains(plan, "USING INDEX cards_deck_state_due") || strings.Contains(plan, "USING COVERING INDEX cards_deck_state_due")
```

To let tests reach the exact SQL strings, keep each query as an unexported package-level `const` (e.g. `qFirstDue`) built with the placeholder helper.

- [ ] **Step 2: Run `go test ./internal/store/`.** Expected: FAIL.
- [ ] **Step 3: Implement.** A new card's `due` is its `created_at`, so ordering new cards by `due, id` follows creation order and is served by the index.
- [ ] **Step 4: Run `go test ./internal/store/`.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 6: `store`: reviews, undo, settings

**Files:**
- Create: `server/internal/store/reviews.go`, `server/internal/store/settings.go`
- Test: `server/internal/store/reviews_test.go`, `settings_test.go`; extend `plan_test.go`

**Interfaces:**
- Produces:
  ```go
  type ReviewInput struct {
      CardID       int64
      ExpectedReps int64
      Rating       srs.Rating
      Before       srs.CardState
      After        srs.CardState
      ReviewedAt   time.Time
      DurationMs   int64
  }
  func (s *Store) ApplyReview(ctx context.Context, in ReviewInput, now time.Time) (Card, error)
  // One tx: UPDATE cards SET <After>, updated_at=now WHERE id=? AND reps=?. 0 rows -> ErrNotFound if
  // the card is gone, else ErrStale. Then INSERT review_logs with the Before fields.
  func (s *Store) UndoLatestReview(ctx context.Context, cardID, expectedReps int64, now time.Time) (Card, error)
  // One tx: latest log (ORDER BY reviewed_at DESC, id DESC LIMIT 1). None -> ErrNotFound. Restore the
  // *_before fields WHERE reps=expectedReps (else ErrStale), then delete that log.
  func (s *Store) CountNewIntroducedSince(ctx context.Context, deckID int64, since time.Time) (int, error)
  type Settings struct {
      DesiredRetention float64
      DayRolloverHour  int
  }
  func (s *Store) GetSettings(ctx context.Context) (Settings, error)
  func (s *Store) UpdateSettings(ctx context.Context, st Settings) error
  ```

- [ ] **Step 1: Write failing tests**

```go
func TestApplyReviewWritesLogAndCard(t *testing.T)   // Card.Sched == After; one log row with rating and state_before == Before.State
func TestApplyReviewStale(t *testing.T)              // ExpectedReps wrong -> ErrStale; no log row inserted
func TestApplyReviewMissingCard(t *testing.T)        // deleted card -> ErrNotFound
func TestUndoRestoresExactly(t *testing.T)           // snapshot card; ApplyReview; UndoLatestReview(expectedReps = after.Reps) -> card.Sched deep-equals snapshot.Sched; log count 0
func TestUndoNothing(t *testing.T)                   // no logs -> ErrNotFound
func TestCountNewIntroducedSince(t *testing.T)       // 2 new-card reviews today, 1 yesterday, 1 review of a Review-state card today -> 2
func TestSettingsRoundTrip(t *testing.T)             // defaults {0.9, 4}; update {0.85, 3}; get matches
// plan_test.go
func TestNewTodayPlanUsesLogIndex(t *testing.T)      // plan contains "review_logs_card_time"
```

- [ ] **Step 2: Run `go test ./internal/store/`.** Expected: FAIL.
- [ ] **Step 3: Implement.** Store `LastReview` zero as SQL `NULL`.
- [ ] **Step 4: Run `go test ./internal/store/`.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 7: `study`: queue, counts, answer, undo

**Files:**
- Create: `server/internal/study/study.go`, `server/internal/study/day.go`
- Test: `server/internal/study/study_test.go`, `day_test.go`

**Interfaces:**
- Consumes: `store.*` (Tasks 3, 5, 6), `srs.*` (Task 4), `clock.Clock`.
- Produces:
  ```go
  package study
  func DayStart(now time.Time, loc *time.Location, rolloverHour int) time.Time
  type Service struct {
      Store *store.Store
      Clock clock.Clock
      Loc   *time.Location
  }
  type Counts struct{ New, Learning, Review int }
  type Preview struct {
      Due           time.Time
      IntervalLabel string
  }
  type NextResult struct {
      Done      bool
      Card      store.Card                // valid when !Done
      Previews  map[srs.Rating]Preview    // valid when !Done
      Remaining Counts
      NextDueAt *time.Time                // set when Done and a future card exists
  }
  func (s *Service) Counts(ctx context.Context, deckID int64) (Counts, error)
  func (s *Service) Next(ctx context.Context, deckID int64) (NextResult, error)            // store.ErrNotFound if no deck
  func (s *Service) Answer(ctx context.Context, cardID int64, r srs.Rating, expectedReps, durationMs int64) (store.Card, error)
  func (s *Service) Undo(ctx context.Context, cardID, expectedReps int64) (store.Card, error)
  ```
  - **Scheduler:** each call builds `srs.NewScheduler(settings.DesiredRetention)` from `Store.GetSettings` (cheap, and it picks up settings changes immediately).
  - **`Counts`:** Learning = due Learning+Relearning at `now`; Review = due Review at `now`; New = `min(CountState(New), max(0, NewPerDay − CountNewIntroducedSince(dayStart)))`.
  - **`Next`:** follows spec §5.1 exactly, including the 20-minute learn-ahead (`FirstDue([Learning,Relearning], now+20m)`).
  - **`Answer`:** clamps `durationMs` to 0..600000, loads the card, computes `After = sched.Next(card.Sched, now, r)`, and calls `ApplyReview`.

- [ ] **Step 1: Write failing tests**

```go
func TestDayStart(t *testing.T)        // loc London, hour 4: 2026-11-02 03:59 -> 2026-11-01 04:00; 04:00 -> 2026-11-02 04:00; 23:00 -> 2026-11-02 04:00
func TestDayStartAcrossDST(t *testing.T) // London 2026-10-25 (clocks go back at 02:00): 2026-10-25 12:00 local -> 2026-10-25 04:00 local; 2026-03-29 12:00 -> 2026-03-29 04:00
func TestQueueOrder(t *testing.T)      // deck with: 1 due learning, 1 due review, 2 new -> successive Next+Answer(Good) yields learning, then review, then new
func TestDailyNewLimit(t *testing.T)   // NewPerDay=2, 5 new cards: answering 2 new (Easy, so they leave the day's queue) -> Next is Done; Counts.New == 0
func TestRolloverResetsNewLimit(t *testing.T) // same as above, then Fake.Advance to next day 04:00 -> Counts.New == 2
func TestLearnAhead(t *testing.T)      // only card is learning due now+15m -> Next returns it; due now+25m -> Done with NextDueAt == that time
func TestPreviewsPresent(t *testing.T) // Next returns 4 previews, labels non-empty, Due(Again) <= Due(Easy)
func TestAnswerStale(t *testing.T)     // Answer with wrong expectedReps -> errors.Is(err, store.ErrStale)
func TestClampDuration(t *testing.T)   // unexported clampDuration(ms int64) int64: -5->0, 1234->1234, 600001->600000; Answer calls it before ApplyReview
func TestUndoThenRequeue(t *testing.T) // answer a new card, Undo -> same card is Next again and Counts.New restored
func TestEmptyDeckDone(t *testing.T)   // deck with no cards -> Done, NextDueAt nil
```

- [ ] **Step 2: Run `go test ./internal/study/`.** Expected: FAIL.
- [ ] **Step 3: Implement.** `DayStart`: `l := now.In(loc)`; `ds := time.Date(l.Year(), l.Month(), l.Day(), hour, 0, 0, 0, loc)`; if `ds.After(l)`, use the previous calendar day. Build it with `time.Date` on that date, **not** `ds.Add(-24h)`, which is wrong across DST.
- [ ] **Step 4: Run `go test ./internal/study/`.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 8: `api`: middleware, errors, deck and card handlers

**Files:**
- Create: `server/internal/api/api.go` (`New`, routing), `middleware.go`, `errors.go`, `json.go`, `decks.go`, `cards.go`, `dto.go`
- Test: `server/internal/api/middleware_test.go`, `decks_test.go`, `cards_test.go`, `helpers_test.go`

**Interfaces:**
- Consumes: `store`, `study`, `validate`, `clock`, `config.Config`.
- Produces:
  ```go
  package api
  type Deps struct {
      Store  *store.Store
      Study  *study.Service
      Clock  clock.Clock
      Config config.Config
      Static http.Handler   // SPA handler from webui (Task 10); nil in tests -> 404
      Logger *slog.Logger
  }
  func New(d Deps) http.Handler
  ```
  - **Middleware order (outermost first):** recover → security headers (every response) → Host allowlist (403) → on `/api/*` mutations, Content-Type (415) then fetch-metadata (403) → auth (Task 17; a no-op until then) → mux.
  - **Errors:** a single `writeError(w, status, code, msg)` writes `{"error":{"code","message"}}`. A single `mapErr(err)` maps `validate.ErrInvalid`→400 `invalid_input`, `store.ErrNotFound`→404 `not_found`, `store.ErrConflict`→409 `conflict`, `store.ErrStale`→409 `stale`, `*http.MaxBytesError`→413 `too_large`, and anything else →500 `internal` with the message `"internal error"`. A 500 is logged with `slog` (method, path, error), never with card content.
  - **`decodeJSON(w, r, dst any) error`:** `http.MaxBytesReader(w, r.Body, 1<<20)`, `DisallowUnknownFields`, and it rejects trailing data after the JSON value.
  - **Host check:** strip the port with `net.SplitHostPort` (falling back to the raw value), lower-case it, and look it up in `AllowedHosts`.
  - **Fetch-metadata check:** allow when `Sec-Fetch-Site` is `same-origin` or `none`. If the header is absent, allow when `Origin` is absent or its hostname is allowed. Otherwise 403 `forbidden_origin`.
  - **DTOs (`dto.go`):** camelCase JSON, times as Unix ms. `deckDTO{id,name,newPerDay,counts{new,learning,review}}` and `cardDTO{id,deckId,front,back,state,due,reps,createdAt,updatedAt}`.
  - **Routes:** standard-library patterns, e.g. `mux.HandleFunc("GET /api/decks", …)`, plus a catch-all `"/api/"` → 404 `not_found` JSON. Path IDs are parsed with `strconv.ParseInt` and must be > 0, else 400.
  - **Handlers:** each one decodes, validates with `validate.*`, calls the store or service, and encodes. Deck creation and rename use `validate.DeckName`; cards use `validate.Side("front"|"back", …)`.

- [ ] **Step 1: Write failing tests**

`helpers_test.go` builds a real store in `t.TempDir()`, a `clock.Fake`, and `api.New` with `AllowedHosts ["localhost"]`. Its `do(method, path, body string, hdr ...)` helper sets `Host: localhost`, `Content-Type: application/json` and `Sec-Fetch-Site: same-origin` by default, with per-test overrides.

```go
func TestSecurityHeaders(t *testing.T)       // GET /api/decks: CSP header == exact string from Global Constraints; nosniff; no-referrer
func TestHostRejected(t *testing.T)          // Host: evil.example -> 403 and JSON error code "forbidden_host"; Host: LOCALHOST:8080 -> 200
func TestCrossSiteRejected(t *testing.T)     // POST /api/decks with Sec-Fetch-Site: cross-site -> 403; no Sec-Fetch-Site + Origin http://evil.example -> 403; no headers at all -> 201
func TestMutationNeedsJSON(t *testing.T)     // POST /api/decks with Content-Type text/plain -> 415
func TestBodyTooLarge(t *testing.T)          // POST /api/decks with 1<<20 + 1 bytes -> 413
func TestUnknownField(t *testing.T)          // {"name":"x","extra":1} -> 400
func TestUnknownAPIPath(t *testing.T)        // GET /api/nope -> 404 JSON, Content-Type application/json
func TestDeckEndpoints(t *testing.T)         // POST {"name":"  Cpp "} -> 201 name "Cpp"; dup -> 409 "conflict"; GET list has counts {0,0,0}; PATCH newPerDay 5; PATCH newPerDay 10000 -> 400; DELETE -> 204; GET /api/decks/1 -> 404; GET /api/decks/abc -> 400
func TestCardEndpoints(t *testing.T)         // POST /api/decks/{id}/cards front "Q" back "A" -> 201; GET list len 1; PATCH {"front":"Q2"} -> front "Q2"; 6000 "→" ok, 6001 -> 400; empty back -> 400; DELETE -> 204
func TestInternalErrorHidden(t *testing.T)   // close the store then GET /api/decks -> 500 with message "internal error" exactly
```

- [ ] **Step 2: Run `go test ./internal/api/`.** Expected: FAIL.
- [ ] **Step 3: Implement.** Also add `GET /api/decks/{id}` (returns one deckDTO) for the deck view. **Update spec §6's table** to list it.
- [ ] **Step 4: Run `go test ./internal/api/`.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 9: `api`: study, review, undo, settings handlers

**Files:**
- Create: `server/internal/api/study.go`, `server/internal/api/settings.go`
- Test: `server/internal/api/study_test.go`, `settings_test.go`

**Interfaces:**
- Consumes: `study.Service` (Task 7).
- Produces:
  - **`GET /api/decks/{id}/next`:** returns either `{"done":false,"card":cardDTO,"reps":n,"previews":{"again":{"due":ms,"intervalLabel":"1m"},"hard":…,"good":…,"easy":…},"remaining":{"new","learning","review"}}` or `{"done":true,"nextDueAt":ms|null,"remaining":{…}}`.
  - **`POST /api/cards/{id}/review`:** body `{"rating":1..4,"expectedReps":n,"durationMs":n}` → `{"card":cardDTO}`.
  - **`POST /api/cards/{id}/undo`:** body `{"expectedReps":n}` → `{"card":cardDTO}`.
  - **`GET /api/settings`** → `{"desiredRetention":0.9,"dayRolloverHour":4}`. **`PATCH /api/settings`** takes either field, validates with `validate.DesiredRetention` and `validate.RolloverHour`, and returns the full object.

- [ ] **Step 1: Write failing tests**

```go
func TestNextAndReview(t *testing.T)      // deck+card; GET next -> done false, 4 previews; POST review rating 3 expectedReps 0 -> 200 reps 1; same POST again -> 409 code "stale"
func TestReviewValidation(t *testing.T)   // rating 0 and 5 -> 400; missing rating -> 400
func TestReviewDeletedCard(t *testing.T)  // DELETE card then POST review -> 404
func TestUndoEndpoint(t *testing.T)       // review then undo expectedReps 1 -> reps 0; undo again -> 404
func TestNextDone(t *testing.T)           // empty deck -> {"done":true,"nextDueAt":null}
func TestSettingsEndpoints(t *testing.T)  // GET defaults; PATCH {"desiredRetention":0.85} ok; 0.5 -> 400; {"dayRolloverHour":24} -> 400
```

- [ ] **Step 2: Run `go test ./internal/api/`.** Expected: FAIL.
- [ ] **Step 3: Implement.** Map `srs.Rating` 1–4 to the keys `again|hard|good|easy`.
- [ ] **Step 4: Run `go test ./internal/api/`.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 10: `webui` embed, `main` (`serve` and `import`)

**Files:**
- Create: `server/internal/webui/webui.go`, `webui_test.go`
- Create: `server/cmd/anki-app/main.go` (replace the stub), `server/cmd/anki-app/import.go`, `import_test.go`

**Interfaces:**
- Produces:
  ```go
  package webui
  //go:embed all:dist
  func Handler() http.Handler
  ```
  - Serves files from `dist/`.
  - A path that matches no file, has no extension and isn't under `/api/` gets `index.html` (SPA fallback); a missing path *with* an extension gets 404.
  - `index.html` is sent with `Cache-Control: no-cache`, and `/assets/*` with `Cache-Control: public, max-age=31536000, immutable`.
  - If `dist/index.html` is missing, respond 503 with the text `frontend not built: run scripts/build.sh`.
  ```go
  package main
  func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error
  func importFile(ctx context.Context, st *store.Store, path string, now time.Time) (store.Deck, int, error)
  ```
  - **Usage:** `anki-app serve` (the default when there are no args) and `anki-app import <file.json>`.
  - **`serve`:** `config.Load` → `store.Open` → `study.Service{Loc: cfg.Location}` → `api.New` → `http.Server` with `ReadHeaderTimeout 5s`, `ReadTimeout 15s`, `WriteTimeout 30s`, `IdleTimeout 120s`. Graceful shutdown on SIGINT (`signal.NotifyContext`, 10 s). Log at startup: the listen address, `DBPath`, whether passphrase mode is on (never the passphrase itself), and the allowed hosts.
  - **`import`:** decode `{"deck":string,"cards":[{"front","back"}]}` strictly (unknown fields are an error); validate the deck name and every side with `validate`. The first invalid card aborts with an error that names its 1-based index. On success, call `store.ImportDeck` and print `imported N cards into deck "X"`.

- [ ] **Step 1: Write failing tests**

```go
func TestSPAFallback(t *testing.T)   // using an fstest.MapFS injected via an unexported constructor handlerFS(fs.FS): "/" and "/decks/3" -> index.html with no-cache; "/assets/a.js" -> file with immutable; "/missing.png" -> 404
func TestNotBuilt(t *testing.T)      // empty MapFS -> 503
func TestImportOK(t *testing.T)      // temp JSON with 2 cards -> deck created, 2 cards, all State New
func TestImportDuplicate(t *testing.T) // import twice -> second error wraps store.ErrConflict
func TestImportInvalidCard(t *testing.T) // second card back "" -> error containing "card 2", and no deck created
func TestImportUnknownField(t *testing.T) // {"deck":"x","cards":[],"extra":1} -> error
```

- [ ] **Step 2: Run `go test ./internal/webui/ ./cmd/anki-app/`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the tests.** Expected: PASS. Then check by hand:
  - `go run ./cmd/anki-app serve`, then in another shell `curl -s -H 'Host: localhost' http://127.0.0.1:8080/api/decks`. Expected: `[]`.
  - `curl -s -o /dev/null -w '%{http_code}' -H 'Host: evil.example' http://127.0.0.1:8080/api/decks`. Expected: `403`.
- [ ] **Step 5: Commit, run `bash scripts/check.sh` (show the output), and merge M1** into `main` (`--no-ff`).

---

## M2: Frontend (`git switch -c feat/frontend`)

Frontend tests are Vitest with jsdom. Commit after each task with a `feat(web): …` message. **Before Task 13**, load the `frontend-design` skill once to choose the visual direction (palette, type scale, spacing), within these limits: the system font stack only, no external assets, and both light and dark themes through CSS custom properties on `:root` with `prefers-color-scheme`.

### Task 11: Markdown renderer, highlighting, wrap marker, `<Markdown>`

**Files:**
- Create: `web/src/markdown/highlight.ts`, `web/src/markdown/splitLines.ts`, `web/src/markdown/render.ts`
- Create: `web/src/components/Markdown.tsx`
- Create: `web/src/styles/card.css`
- Test: `web/src/markdown/highlight.test.ts`, `splitLines.test.ts`, `render.test.ts`, `web/src/components/Markdown.test.tsx`

**Interfaces:**
- Produces:
  ```ts
  // splitLines.ts: split highlight.js HTML into per-line HTML, closing open <span>s at each "\n" and reopening them on the next line
  export function splitHighlightedLines(html: string): string[]
  // highlight.ts: registers cpp, c, python, bash, json on highlight.js/lib/core
  export function highlightCode(code: string, lang: string | undefined): { html: string; language: string | null }
  // render.ts
  export function renderMarkdown(source: string): string   // sanitised HTML string
  // Markdown.tsx
  export function Markdown(props: { source: string; className?: string }): JSX.Element
  ```
  - **`highlightCode`:** lower-cases `lang` and resolves aliases through `hljs.getLanguage`. For an unknown or missing language it returns the HTML-escaped code and `language: null`.
  - **`renderMarkdown`:** `markdown-it` with `{ html: false, linkify: false, typographer: false }`. Set `md.validateLink` to allow only `http:`, `https:`, `mailto:` (anything else is rendered as text).
  - **Fences:** the `highlight` option returns the full `<pre class="code-block"><code class="hljs language-X">…</code></pre>` (markdown-it uses the returned string as-is when it starts with `<pre`). The inner HTML is `splitHighlightedLines(html).map(l => '<span class="line">' + l + '</span>').join('')`.
  - **Sanitising:** finally `DOMPurify.sanitize(html, { ALLOWED_ATTR: ['class', 'href', 'title'], FORBID_TAGS: ['style'] })`.
  - **`<Markdown>`:** memoises `renderMarkdown(source)` and renders `<div className={'md ' + (className ?? '')} dangerouslySetInnerHTML={{ __html }} />`. This is the only allowed use.

The splitting algorithm (not fully determined by the tests above):

```ts
// Tokenise html with /(<span[^>]*>)|(<\/span>)|(\n)|([^<\n]+)/g.
// Keep a stack of open-tag strings. On "\n": push current line + "</span>".repeat(stack.length) to output,
// start the next line with stack.join(''). On open tag: append + push. On close: append + pop. Text: append.
// At end, push the final line. hljs escapes text, so no raw "<" appears except in tags.
```

`card.css` (the layout rules from spec §8.3):
- **Prose:** `.md { text-align: center; overflow-wrap: anywhere; }`
- **Blocks:** `.md pre.code-block, .md ul, .md ol { text-align: left; width: fit-content; max-width: 100%; margin-inline: auto; }`
- **Code width:** `.md pre.code-block { max-width: min(100%, calc(var(--code-cols, 80) * 1ch + 2 * var(--code-pad))); overflow-x: auto; font-family: var(--font-mono); }`
- **Lines:** `.md .line { display: block; min-height: var(--code-lh); line-height: var(--code-lh); }`. In scroll mode `.md code { white-space: pre; }`.
- **Wrap mode** (`.code-wrap` set on `<html>` by Task 12): `.code-wrap .md .line { white-space: pre-wrap; overflow-wrap: anywhere; padding-left: 2ch; text-indent: -2ch; }`.
- **The ↪ marker** (it marks continuation lines only, and copying never picks it up):
  ```css
  .code-wrap .md .line {
    background:
      linear-gradient(var(--code-bg), var(--code-bg)) 0 0 / 2ch var(--code-lh) no-repeat,  /* hides marker on first visual line */
      url("data:image/svg+xml,<url-encoded SVG of a ↪ arrow drawn with a <path>, stroke = muted colour>") 0 0 / 2ch var(--code-lh) repeat-y;
  }
  ```
- **Token colours:** classes `hljs-keyword, hljs-built_in, hljs-type, hljs-literal, hljs-number, hljs-string, hljs-comment, hljs-meta, hljs-title, hljs-params, hljs-attr, hljs-symbol`, coloured from CSS variables, with light values on `:root` and dark values under `@media (prefers-color-scheme: dark)`.
- **Inline code:** `.md :not(pre) > code { background: var(--inline-code-bg); padding: 0.1em 0.35em; border-radius: 4px; }`

- [ ] **Step 1: Write failing tests**

```ts
// splitLines.test.ts
it('splits plain lines')                              // 'a\nb' -> ['a','b']
it('closes and reopens spans across newlines')        // '<span class="c">x\ny</span>z' -> ['<span class="c">x</span>', '<span class="c">y</span>z']
it('handles nested spans and empty lines')            // '<span class="a"><span class="b">p\n\nq</span></span>' -> 3 lines, each balanced (count "<span" === count "</span>")
// highlight.test.ts
it('highlights cpp')                                  // highlightCode('int x;', 'cpp').html contains 'hljs-type' or 'hljs-keyword'; language 'cpp'
it('aliases and unknown languages')                   // 'c++' and 'C++' -> language 'cpp'; 'madeup' and undefined and 'text' -> language null, html === escaped input ('a<b' -> 'a&lt;b')
// render.test.ts
it('strips raw html')                                 // '<script>alert(1)</script>' -> result has no '<script'
it('neutralises event handlers')                      // '<img src=x onerror=alert(1)>' -> no 'onerror'
it('blocks javascript: links')                        // '[x](javascript:alert(1))' -> no 'href="javascript'
it('renders fenced cpp as lines')                     // '```cpp\nint a;\nint b;\n```' -> contains 'class="code-block"' and exactly 2 'class="line"'
it('renders inline code')                             // 'The `std::vector<T>` type' -> '<code>std::vector&lt;T&gt;</code>'
it('preserves indentation in fences')                 // '```cpp\n    int& r = x;\n```' -> line content starts with 4 spaces
// Markdown.test.tsx
it('renders sanitised html into the DOM')             // render(<Markdown source={'**b** `c`'} />) -> getByText('b').tagName === 'STRONG'; container.querySelector('code')
```

- [ ] **Step 2: Run `cd web && npx vitest run src/markdown src/components`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the same command.** Expected: PASS. Also run `npm run lint`. Expected: no `react/no-danger` errors (the override covers `Markdown.tsx` only).
- [ ] **Step 5: Commit.**

### Task 12: API client and per-device display settings

**Files:**
- Create: `web/src/api/types.ts`, `web/src/api/client.ts`
- Create: `web/src/settings/display.ts`
- Test: `web/src/api/client.test.ts`, `web/src/settings/display.test.ts`

**Interfaces:**
- Produces:
  ```ts
  // types.ts: mirrors spec §6 exactly (times are Unix ms numbers)
  export type CardState = 0 | 1 | 2 | 3;
  export interface Counts { new: number; learning: number; review: number }
  export interface Deck { id: number; name: string; newPerDay: number; counts: Counts }
  export interface Card { id: number; deckId: number; front: string; back: string; state: CardState; due: number; reps: number; createdAt: number; updatedAt: number }
  export type RatingKey = 'again' | 'hard' | 'good' | 'easy';
  export interface Preview { due: number; intervalLabel: string }
  export type NextResponse =
    | { done: false; card: Card; reps: number; previews: Record<RatingKey, Preview>; remaining: Counts }
    | { done: true; nextDueAt: number | null; remaining: Counts };
  export interface Settings { desiredRetention: number; dayRolloverHour: number }
  export interface AuthStatus { enabled: boolean; authenticated: boolean }

  // client.ts: the only module that calls fetch
  export class ApiError extends Error { status: number; code: string }
  export const api: {
    listDecks(): Promise<Deck[]>; getDeck(id: number): Promise<Deck>;
    createDeck(name: string): Promise<Deck>; updateDeck(id: number, p: { name?: string; newPerDay?: number }): Promise<Deck>; deleteDeck(id: number): Promise<void>;
    listCards(deckId: number): Promise<Card[]>; getCard(id: number): Promise<Card>;
    createCard(deckId: number, p: { front: string; back: string }): Promise<Card>;
    updateCard(id: number, p: { front?: string; back?: string }): Promise<Card>; deleteCard(id: number): Promise<void>;
    next(deckId: number): Promise<NextResponse>;
    review(cardId: number, p: { rating: 1 | 2 | 3 | 4; expectedReps: number; durationMs: number }): Promise<{ card: Card }>;
    undo(cardId: number, expectedReps: number): Promise<{ card: Card }>;
    getSettings(): Promise<Settings>; updateSettings(p: Partial<Settings>): Promise<Settings>;
    authStatus(): Promise<AuthStatus>; login(passphrase: string): Promise<void>; logout(): Promise<void>;
  };
  export function onUnauthorized(handler: () => void): void   // called on any 401 (used by Task 18)

  // display.ts: per-device, localStorage key "display.v1", every access wrapped in try/catch
  export type WrapMode = 'auto' | 'scroll' | 'wrap';
  export interface DisplaySettings { wrapMode: WrapMode; codeCols: number }   // defaults { 'auto', 80 }, codeCols clamped 40..200
  export function loadDisplay(): DisplaySettings
  export function saveDisplay(s: DisplaySettings): void
  export function applyDisplay(s: DisplaySettings, root?: HTMLElement): void  // sets --code-cols via style.setProperty; toggles 'code-wrap' class (auto => matchMedia('(max-width: 599px)'))
  ```
  Every request sends `credentials: 'same-origin'`, and every body is JSON with `Content-Type: application/json`. A non-2xx response throws `ApiError` from the `{error:{code,message}}` body, using code `network` / status 0 when the request fails or the body isn't JSON.

- [ ] **Step 1: Write failing tests**

```ts
// client.test.ts (stub global fetch with vi.fn)
it('sends JSON with content type')          // createDeck('Cpp') -> fetch called with method POST, headers Content-Type application/json, body '{"name":"Cpp"}'
it('throws ApiError with server code')      // 409 {"error":{"code":"stale","message":"m"}} -> rejects ApiError status 409 code 'stale'
it('maps network failure')                  // fetch rejects -> ApiError status 0 code 'network'
it('calls onUnauthorized on 401')
it('returns undefined for 204')             // deleteDeck resolves undefined
// display.test.ts
it('defaults when storage empty or throws') // localStorage.getItem throws -> { wrapMode:'auto', codeCols:80 }
it('clamps codeCols')                       // saved 500 -> 200; saved 10 -> 40; saved 'x' -> 80
it('applies css var and wrap class')        // applyDisplay({wrapMode:'wrap',codeCols:72}, el) -> el.style.getPropertyValue('--code-cols') === '72' && el.classList.contains('code-wrap')
```

- [ ] **Step 2: Run `npx vitest run src/api src/settings`.** Expected: FAIL.
- [ ] **Step 3: Implement.** For `applyDisplay` with `auto`, listen for `matchMedia` changes and re-apply when the width crosses 600 px.
- [ ] **Step 4: Run the same command.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 13: App shell, routing, Decks page, Deck page

**Files:**
- Create: `web/src/main.tsx` (replace), `web/src/App.tsx` (routes and layout), `web/src/styles/base.css`
- Create: `web/src/pages/DecksPage.tsx`, `web/src/pages/DeckPage.tsx`, `web/src/components/ConfirmDialog.tsx`, `web/src/components/ErrorBanner.tsx`, `web/src/hooks/useAsync.ts`
- Test: `web/src/pages/DecksPage.test.tsx`, `DeckPage.test.tsx`

**Interfaces:**
- Consumes: `api`, `Markdown`, `loadDisplay`/`applyDisplay` (called once in `main.tsx`).
- Produces:
  - **Routes** (React Router `createBrowserRouter`): `/`, `/decks/:id`, `/decks/:id/study`, `/decks/:id/cards/new`, `/cards/:id/edit`, `/settings`, `/login`. Unknown routes show a "Not found" page with a link home.
  - **`useAsync<T>(fn: () => Promise<T>, deps: unknown[]): { data?: T; error?: ApiError; loading: boolean; reload(): void }`**
  - **`ConfirmDialog` props:** `{ open, title, body, confirmLabel, requireText?: string, onConfirm, onCancel }`. When `requireText` is set, the confirm button stays disabled until the input equals it exactly.
- **DecksPage:** a list of decks. Each row shows the name, the counts (learning / review / new) and a **Study** link (disabled when all counts are 0), and the name links to `/decks/:id`. A **New deck** form shows a 409 inline as "A deck with that name already exists". Header nav: Decks, Settings.
- **DeckPage:**
  - Deck name and counts, a **Study** button, and **Add card**.
  - A filter input matching the front or back text case-insensitively, and the card list.
  - Each list row shows the first non-empty line of the front as **plain text** (not rendered Markdown), linking to the editor.
  - Settings: rename, new per day (number 0–9999), and **Delete deck**, which opens `ConfirmDialog` with `requireText = deck.name` and navigates to `/` after deleting.
- **base.css:** system font stack, the `:root` tokens for both themes, `--code-lh: 1.5em`, `--code-pad: 0.75rem`, `--code-bg`, and `--font-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`. Interactive elements are at least 44 px tall. A visible `:focus-visible` outline. Side padding of 16 px at phone widths, with content `max-width: 72rem` centred on desktop.

- [ ] **Step 1: Write failing tests** (mock `api` with `vi.mock('../api/client')`; render inside `MemoryRouter`)

```ts
it('DecksPage lists decks with counts and study link')
it('DecksPage shows duplicate-name error inline')           // createDeck rejects ApiError 409 'conflict'
it('DeckPage filters cards by text')                        // 3 cards, type 'vector' -> 1 row
it('DeckPage delete requires typing the deck name')         // confirm disabled until input === 'Cpp'; then deleteDeck called with id
```

- [ ] **Step 2: Run `npx vitest run src/pages`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the same command.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 14: Card editor

**Files:**
- Create: `web/src/pages/CardEditorPage.tsx`, `web/src/text/countChars.ts`
- Test: `web/src/pages/CardEditorPage.test.tsx`, `web/src/text/countChars.test.ts`

**Interfaces:**
- Produces: `export function countChars(s: string): number` returns `[...s].length`. `export const MAX_SIDE = 6000`.
- **Behaviour:**
  - Front and back `<textarea>`s, each with a counter in the form `123 / 6000` that turns into a warning colour above 6000. **Save** is disabled while either side is empty or over the limit.
  - The preview uses `<Markdown>`. Below 900 px wide, **Edit / Preview** tabs (one textarea pair or one preview visible); at 900 px and up, side by side. Use CSS media queries plus a tab state; the tabs only exist in the narrow layout.
  - `Ctrl/Cmd+Enter` saves. After creating a card, the form clears for the next card and shows "Card added". After editing, it navigates back to `/decks/:deckId`.
  - **Delete card** (edit mode only) opens `ConfirmDialog` without `requireText`.
  - A server 400 shows the server's message.

- [ ] **Step 1: Write failing tests**

```ts
it('countChars counts code points')       // countChars('→→') === 2; countChars('😀') === 1
it('save disabled when empty or over limit')
it('create posts and clears form')        // createCard called with {front, back}; textareas empty afterwards; 'Card added' visible
it('edit loads card and saves changes')   // getCard -> values shown; save -> updateCard(id, {front, back}); navigates to /decks/:deckId
it('preview renders markdown')            // typing '`x`' in front -> preview contains <code>x</code>
```

- [ ] **Step 2: Run `npx vitest run src/pages/CardEditorPage.test.tsx src/text`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the same command.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 15: Study screen

**Files:**
- Create: `web/src/study/studyReducer.ts`, `web/src/pages/StudyPage.tsx`
- Test: `web/src/study/studyReducer.test.ts`, `web/src/pages/StudyPage.test.tsx`

**Interfaces:**
- Produces:
  ```ts
  export type StudyState =
    | { kind: 'loading' }
    | { kind: 'front'; next: Extract<NextResponse, { done: false }>; shownAt: number; last?: LastAnswer }
    | { kind: 'back';  next: Extract<NextResponse, { done: false }>; shownAt: number; last?: LastAnswer }
    | { kind: 'done'; nextDueAt: number | null; last?: LastAnswer }
    | { kind: 'error'; message: string; last?: LastAnswer };
  export interface LastAnswer { cardId: number; repsAfter: number }
  export type StudyEvent =
    | { type: 'loaded'; res: NextResponse; now: number }
    | { type: 'reveal' }
    | { type: 'answered'; cardId: number; repsAfter: number }
    | { type: 'undone' }
    | { type: 'failed'; error: ApiError };
  export function studyReducer(s: StudyState, e: StudyEvent): StudyState
  export function shouldReloadAfterError(e: ApiError): boolean   // true for 409 and 404
  ```
- **StudyPage behaviour (spec §8.2):**
  - On mount it calls `api.next`. The front is shown; **Show answer** (or `Space`) reveals the back under an `<hr>` while the front stays visible.
  - **Rating buttons** are labelled `Again · 1m` and so on from `previews`. `1`–`4` rate; `Space` on the back rates Good; `Z` undoes. **Undo** is visible whenever `last` is set.
  - **The answer flow:** the rating posts `review` with `durationMs = Date.now() - shownAt` and `expectedReps = next.reps`, dispatches `answered`, then reloads `next`.
  - **Undo:** posts `undo(last.cardId, last.repsAfter)`, then reloads `next`.
  - **Errors:** when `shouldReloadAfterError`, silently reload `next`; otherwise dispatch `failed` and show `ErrorBanner` with **Retry**.
  - **Double-submit guard:** while a request is in flight, ignore further key and button presses. This is the client-side half; the server's 409 is the other.
  - **Layout:** the action bar is `position: sticky; bottom: 0` with `padding-bottom: max(12px, env(safe-area-inset-bottom))`, the four buttons in one row, each at least 44 px tall. The remaining counts show in the header.
  - **Done state:** "Done for now". If `nextDueAt` is set, show "Next card due at HH:MM" (local time). Link back to the deck.
  - Keyboard handlers ignore events whose target is an input or textarea.

- [ ] **Step 1: Write failing tests**

```ts
// studyReducer.test.ts
it('loaded not-done -> front with shownAt = now')
it('reveal: front -> back; reveal in back is a no-op')
it('answered records last and goes to loading')
it('loaded done -> done with nextDueAt, keeps last')
it('undone clears last')
it('404 on answer loads next')     // shouldReloadAfterError(new ApiError(404,'not_found')) === true; same for 409; false for 500
// StudyPage.test.tsx (mock api)
it('space reveals then rates good')            // keyboard ' ' -> back visible; ' ' -> api.review called with rating 3 and expectedReps from next.reps
it('number keys rate')                          // '1' on back -> rating 1
it('buttons show interval labels')              // 'Good · 10m'
it('ignores double press while in flight')      // two quick '3' -> api.review called once
it('undo calls api.undo with repsAfter')
it('shows done state with next due time')
```

- [ ] **Step 2: Run `npx vitest run src/study src/pages/StudyPage.test.tsx`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the same command.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 16: Settings page, web app manifest, icons

**Files:**
- Create: `web/src/pages/SettingsPage.tsx`, `web/public/manifest.webmanifest`, `web/public/icons/icon-192.png`, `icon-512.png`, `apple-touch-icon.png` (180×180)
- Create: `server/cmd/genicons/main.go` (standard library `image/png` only; writes the three PNGs into `web/public/icons/`)
- Modify: `web/index.html`
- Test: `web/src/pages/SettingsPage.test.tsx`

**Interfaces:**
- **SettingsPage:**
  - **Scheduling (server):** desired retention (number input with step 0.01, 0.70–0.99) and rollover hour (0–23), saved through `api.updateSettings`, with a server 400 shown inline.
  - **Display (this device):** wrap mode (Auto / Scroll / Wrap radio) and code width (number 40–200), saved with `saveDisplay` and `applyDisplay` immediately.
  - Under Display, a short **sample code block** rendered through `<Markdown>`, with one line longer than 100 characters, so the effect is visible straight away.
- **`manifest.webmanifest`:** `name` "anki-app", `short_name` "Cards", `start_url` "/", `display` "standalone", `background_color` and `theme_color` matching the dark theme background, and the icons (192, 512, `purpose: "any maskable"` on 512).
- **`index.html`:**
  - `<link rel="manifest" href="/manifest.webmanifest">`, `<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png">`.
  - `<meta name="apple-mobile-web-app-capable" content="yes">`, `<meta name="mobile-web-app-capable" content="yes">`, `<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">`.
  - `<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">`, `<meta name="theme-color">` (one for light, one for dark via `media`).
  - `<title>anki-app</title>`.
  - **No inline `<script>` or `<style>`.**
- **`genicons`:** a simple geometric mark (e.g. two offset rounded "cards" in the accent colour on the background colour), with no text and no font dependency. Run it once (`cd server && go run ./cmd/genicons`) and commit the PNGs.

- [ ] **Step 1: Write failing tests**

```ts
it('loads and saves scheduling settings')       // getSettings -> inputs show 0.9 / 4; change to 0.85 -> updateSettings({desiredRetention:0.85})
it('shows server validation error')             // updateSettings rejects ApiError 400 -> message visible
it('saves display settings locally')            // choose Wrap -> saveDisplay called with wrapMode 'wrap'; document.documentElement has class 'code-wrap'
```

- [ ] **Step 2: Run `npx vitest run src/pages/SettingsPage.test.tsx`.** Expected: FAIL.
- [ ] **Step 3: Implement**, run `genicons`, and update `index.html`.
- [ ] **Step 4: Run the tests.** Expected: PASS. Then `bash scripts/build.sh` and `./server/anki-app serve`; open `http://localhost:8080` and check by hand that the manifest loads (DevTools → Application → Manifest shows no errors) and that the console shows **no CSP violations** on any page.
- [ ] **Step 5: Commit, run `bash scripts/check.sh` (show the output), and merge M2.**

---

## M3: Passphrase mode (`git switch -c feat/passphrase`)

### Task 17: Sessions, login, logout, auth middleware, `/api/auth`

**Files:**
- Create: `server/internal/store/sessions.go`, `sessions_test.go`
- Create: `server/internal/api/auth.go`, `auth_test.go`
- Modify: `server/internal/api/api.go` (insert the auth middleware and routes)

**Interfaces:**
- Produces:
  ```go
  // store
  func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, now, expires time.Time) error
  func (s *Store) SessionValid(ctx context.Context, tokenHash []byte, now time.Time) (bool, error)
  func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error
  func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error
  ```
  - **`GET /api/auth`** → `{"enabled":bool,"authenticated":bool}`. Always reachable. When disabled, it returns `authenticated: true`.
  - **`POST /api/login`:** body `{"passphrase":string}`. Order of checks:
    1. If the rate limiter is exhausted, 429 `rate_limited`.
    2. Compare `subtle.ConstantTimeCompare(sha256(input), sha256(configured))`; on a mismatch, 401 `bad_passphrase`.
    3. On success, generate a 32-byte token with `crypto/rand`, store `sha256(token)` with a 30-day expiry, and set the `session` cookie to the base64url token with `HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=2592000`. Return 204.
    4. Call `DeleteExpiredSessions` on every login.
  - **`POST /api/logout`:** deletes the session row and expires the cookie. Returns 204.
  - **Auth middleware:** when the passphrase is empty it does nothing. Otherwise, every `/api/*` except `GET /api/auth` and `POST /api/login` needs a valid cookie, else 401 `unauthorized`. Non-API (static SPA) paths stay public; they contain no data.
  - **Rate limiter:** in memory, at most 5 attempts in any rolling 60-second window, using the injected clock. Failed and successful attempts both count.
  - **HSTS:** `Strict-Transport-Security: max-age=31536000` is added only when the passphrase is set.
  - **Startup:** `DeleteExpiredSessions` also runs once at startup in `serve` (modify `main.go`).

- [ ] **Step 1: Write failing tests**

```go
func TestSessionsLifecycle(t *testing.T)     // create; valid before expiry; invalid after (now > expires); delete -> invalid; DeleteExpired removes only expired rows
func TestAuthDisabled(t *testing.T)          // no passphrase: GET /api/decks 200 without cookie; GET /api/auth {"enabled":false,"authenticated":true}; no HSTS header
func TestAuthRequired(t *testing.T)          // passphrase set: GET /api/decks 401; GET /api/auth 200 {"enabled":true,"authenticated":false}; GET / (static) not 401
func TestLoginFlow(t *testing.T)             // wrong -> 401; right -> 204 + Set-Cookie with HttpOnly, Secure, SameSite=Strict; with cookie GET /api/decks 200; logout -> cookie no longer works
func TestLoginRateLimit(t *testing.T)        // 5 wrong attempts -> 6th (even correct) 429; Fake.Advance(61s) -> correct login 204
func TestHSTSWhenEnabled(t *testing.T)
func TestCookieIsHashedAtRest(t *testing.T)  // after login, the sessions row's token_hash != raw cookie bytes and == sha256(raw)
```

- [ ] **Step 2: Run `go test ./internal/store/ ./internal/api/`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the same command.** Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 18: Login page and the client-side auth gate

**Files:**
- Create: `web/src/pages/LoginPage.tsx`, `web/src/auth/AuthGate.tsx`
- Modify: `web/src/App.tsx` (wrap every route except `/login` in `AuthGate`), `web/src/pages/SettingsPage.tsx` (a **Log out** button, shown only when `authStatus().enabled`)
- Test: `web/src/pages/LoginPage.test.tsx`, `web/src/auth/AuthGate.test.tsx`

**Interfaces:**
- **`AuthGate`:** calls `api.authStatus()` once. If `enabled && !authenticated`, it navigates to `/login?next=<path>`; otherwise it renders its children. It registers `onUnauthorized` to do the same redirect.
- **`LoginPage`:** one password field (`autocomplete="current-password"`) and a submit button. On 204 it navigates to `next`, accepting only same-origin paths that start with `/` and not `//`, else `/`. A 401 shows "Wrong passphrase"; a 429 shows "Too many attempts, wait a minute".

- [ ] **Step 1: Write failing tests**

```ts
it('AuthGate renders children when auth disabled')
it('AuthGate redirects to /login when required')
it('Login success navigates to next')
it('Login rejects open-redirect next')        // next=//evil.example -> navigates to '/'
it('Login shows rate-limit message on 429')
```

- [ ] **Step 2: Run `npx vitest run src/auth src/pages/LoginPage.test.tsx`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the same command.** Expected: PASS.
- [ ] **Step 5: Commit, run `bash scripts/check.sh` (show the output), and merge M3.**

---

## M4: End-to-end tests and phone access (`git switch -c feat/e2e`)

### Task 19: Playwright smoke test at iPhone and desktop sizes

**Files:**
- Create: `web/playwright.config.ts`, `web/e2e/smoke.spec.ts`
- Modify: `.gitignore` (`web/playwright-report/`, `web/test-results/`)

**Interfaces:**
- **`playwright.config.ts`:**
  - **Projects:** `iphone` (`devices['iPhone 15']`, WebKit) and `desktop` (Chromium, viewport 1440×900).
  - **`webServer`:** command `bash ../scripts/build.sh && ../server/anki-app serve` (with `.exe` on Windows), `url: 'http://127.0.0.1:18080/api/auth'`, `reuseExistingServer: false`, env `ADDR=127.0.0.1:18080`, `DB_PATH=<os.tmpdir()>/anki-e2e-<Date.now()>.db`, `ALLOWED_HOSTS=127.0.0.1,localhost`.
  - `use.baseURL: 'http://127.0.0.1:18080'`.
- Install the browsers once with `npx playwright install chromium webkit`.

- [ ] **Step 1: Write the smoke test**

```ts
test('create deck, add code card, study, rate good', async ({ page }) => {
  // unique deck name per project: `E2E ${testInfo.project.name} ${Date.now()}`
  // create deck from home; open it; Add card with front 'What is `x`?\n\n```cpp\nint x = 0;\n```' and back 'zero'
  // go to Study: expect '.code-block .line' visible; press Show answer; expect 'zero'; click button /Good/
  // expect done state text 'Done for now'
});
test('no horizontal page overflow in wrap mode', async ({ page }) => {
  // set Display wrap mode to Wrap in Settings; create a card whose fence contains one 200-char token (e.g. 'std::' + 'a'.repeat(195))
  // open the card's study screen; reveal
  // expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
});
test('no CSP violations', async ({ page }) => {
  // collect page.on('console') messages of type 'error' containing 'Content Security Policy' while visiting /, /settings, a deck page, the editor, and study; expect none
});
```

- [ ] **Step 2: Run `cd web && npm run e2e`.** Expected: all three tests pass in both projects (6 passed). If one fails, fix the app, not the test, unless the test itself is wrong.
- [ ] **Step 3: Commit.**

### Task 20: README, phone access through Tailscale, final verification

**Files:**
- Create: `README.md`

**Interfaces:**
- **`README.md` sections:**
  1. **What it is** (one paragraph, linking the spec).
  2. **Prerequisites.**
  3. **Build and run:** `scripts/build.sh`, then `server/anki-app serve`.
  4. **Development:** two shells, Go server + `npm run dev`; open the Vite URL.
  5. **Import the Cpp deck:** `server/anki-app import data/cpp.json`, once the cleanup task has produced it (spec §10). Include the JSON format.
  6. **Phone access with Tailscale:**
     - Install Tailscale on the laptop and the iPhone and sign in to the same tailnet.
     - In the admin console enable MagicDNS and HTTPS certificates.
     - `tailscale serve --bg 8080`, and `tailscale serve status` to see the `https://<machine>.<tailnet>.ts.net` URL.
     - Set `ALLOWED_HOSTS=localhost,127.0.0.1,<machine>.<tailnet>.ts.net` before starting the server.
     - On the iPhone, open the URL in Safari → Share → **Add to Home Screen**.
     - The laptop must be awake for the phone to reach it.
  7. **Configuration:** the env var table from spec §3.1.
  8. **Backup:** stop the server and copy the `.db` file. Copying `-wal`/`-shm` isn't needed after a clean shutdown.
  9. **Moving to a VM later:**
     - Build with `GOOS=linux GOARCH=amd64` and copy the binary and the db.
     - Set `PASSPHRASE` (≥16 characters) and `ALLOWED_HOSTS`.
     - Put it behind HTTPS (Tailscale on the VM, or a reverse proxy with TLS).
     - Never expose it over plain HTTP.

- [ ] **Step 1: Write the README.**
- [ ] **Step 2: Check phone access by hand (needs Marc).**
  - Start the server with the tailnet host in `ALLOWED_HOSTS`, then run `tailscale serve --bg 8080`.
  - Ask Marc to open the URL on his iPhone, add it to the Home Screen, and study one card.
  - If Tailscale sign-in or the admin console settings need him, give him the exact steps and wait. **Do not** claim phone access works until he confirms.
- [ ] **Step 3: Final verification.** Run `bash scripts/check.sh` and show the full output; everything must pass. Then `git status` must be clean apart from the intended files.
- [ ] **Step 4: Commit, merge M4 into `main`, and report:** what was built, the check output, anything skipped or deviating from the spec (with reasons), and the next step (the cleanup task producing `data/cpp.json`, then `anki-app import`).
