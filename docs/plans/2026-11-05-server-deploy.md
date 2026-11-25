# Server Deployment Implementation Plan

Status: done

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move anki-app from the Windows laptop to a free GCP e2-micro VM: in-app backups with retention, a health endpoint, a Docker image built by GitHub Actions, and a pull-based updater on the VM that rolls itself back.

**Architecture:**
- **App (Go):** a new `internal/backup` package owns file names, retention, the writer and the scheduler. SQL stays in `internal/store` (`VacuumInto`, `QuickCheck`, `Ping`, the migration guard). `serve` runs the scheduler; a `backup` subcommand writes one backup; `GET /api/health` reports version and last backup.
- **Image + CI:** a multi-stage Dockerfile (node → go → distroless nonroot). GitHub Actions runs `check.sh`, then pushes `:main` and `:sha-<12>` to GHCR.
- **VM:** bash scripts and systemd units under `deploy/vm/`. `anki-update` pulls every minute, backs up, swaps, health-checks and rolls back. Scripts share `deploy/vm/lib.sh` and are tested with stub commands on `PATH`.

**Tech Stack:** Go (stdlib, `modernc.org/sqlite`), React + TypeScript + Vitest + Playwright, Docker (Buildx), GitHub Actions, bash + systemd on Debian 12, `gcloud storage`.

**Spec:** `docs/specs/2026-11-05-server-deploy-design.md` ("§n" below). It extends `docs/specs/2026-11-02-flashcards-design.md` ("main §n"). **Read both before your task.** Where this plan and the spec disagree, the spec wins; stop and report the conflict.

## How to run this plan

- **Platform:** Windows 11 with Git Bash. The Go code must also build and run on Linux. `jq`, `flock` and `shellcheck` are not installed locally; Docker Desktop is installed but may not be running (ask Marc to start it).
- **Milestones = branches** (spec §14). Each is branched from `main`, done bar (`bash scripts/check.sh`) run and output shown, then merged.

| Milestone | Branch | Tasks | Parallel waves |
|---|---|---|---|
| M1 Backups | `feat/backups` | 1–5 | {1, 2, 5} → 3 → 4 → e2e-tester |
| M2 Container + CI | `feat/container-ci` | 6–7 | {6, 7} |
| M3 VM | `feat/vm` | 8–10 | 10 can start any time (Marc needs it early); 8 → 9 |
| M4 Cutover | `feat/cutover` | 11–12 | 11 → 12 (with Marc) |

- **Parallel implementers** run in separate git worktrees (`isolation: "worktree"`) and commit there; the main session merges their commits into the milestone branch. Tasks in one wave touch disjoint files.
- **Commits** end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. No new Go or npm dependencies are needed; if one turns out to be, stop and ask.

## Global Constraints

- Default listen address stays `127.0.0.1:8080`. Never weaken the Host allowlist, Origin/Sec-Fetch-Site check, JSON content-type rule or CSP.
- SQL only in `server/internal/store`, `?` parameters only (`VACUUM INTO ?`). `internal/archtest` must keep passing.
- Time from the injected `clock.Clock` in logic; times stored and sent as UTC Unix ms.
- Backup file name: `anki-app-YYYYMMDDTHHMMSSZ.db` (UTC). Only names matching `^anki-app-\d{8}T\d{6}Z\.db$` are backups; other files are ignored and never deleted.
- Backup folders `BACKUP_DIR/daily/` and `BACKUP_DIR/pre-deploy/`, mode `0700`. `BACKUP_DIR` default: `filepath.Join(filepath.Dir(DB_PATH), "backups")`.
- Deletion: `os.Remove` / `rm -f` on single, exactly named files only. No recursive or computed-directory deletes, in Go, scripts or tests (Marc's standing rule). Tests use fresh `t.TempDir()` folders.
- Never log card content, passphrases or tokens. Backup logs: name, size, duration only.
- Scheduled backup: first check at or after **03:00 local** (`cfg.Location`), every **15 min**. Pre-deploy keeps newest **10**.
- Health: `GET /api/health` → `200 {"version","lastBackupAt": ms|null}`; `503` code `unavailable` if `SELECT 1` fails; exempt from passphrase auth; Host allowlist applies.
- Migration guard message, exact: `database schema v<N> is newer than this build (v<M>)`.
- Usage text, exact: `usage: anki-app [serve] | anki-app import <file.json> | anki-app backup [--pre-deploy] | anki-app version`.
- Image: `ghcr.io/marccowan/anki-app`, tags `:main` and `:sha-<first 12 of SHA>`, uid `65532`, `linux/amd64`. Base images pinned by digest; GitHub Actions pinned by full commit SHA with the version in a trailing comment.
- VM paths and the systemd unit are exactly as in §7.3 and §7.4.

## Review Focus

1. **GHCR pull fails** (network blip, expired PAT). The updater must exit non-zero **without stopping the running app**. → Task 8, test `pull_failure_leaves_app_running`.
2. **The backup subcommand prints a container path** (`/data/backups/pre-deploy/…`) and may write log lines to stderr. The updater must use the last stdout line and map `/data/` to the host data folder, or the rollback restores nothing. → Task 8, test `rollback_restores_predeploy_backup` (the stub prints a log line on stderr and the container path on stdout).
3. **A backup fails** (disk full, `VACUUM INTO` error). The app keeps serving, no `.partial` remains, and `/api/health` still answers with the older `lastBackupAt`. → Task 3, `TestWriteFailureLeavesNoPartial`; Task 4, `TestHealthLastBackupIgnoresPartials`.
4. **Backups dated in the future** (VM clock corrected backwards). Retention must never drop them and `Due` must not be fooled into skipping today forever by them alone. → Task 2, `TestKeepNeverDropsFutureBackups`, `TestDueIgnoresFutureBackups`.
5. **SIGTERM during a scheduled backup.** The store must be closed only after the scheduler goroutine returns. → Task 4, `TestServeWaitsForSchedulerBeforeClosingStore`.

---

## M1 — `feat/backups`

### Task 1: Config `BACKUP_DIR`, store primitives, migration guard

**Files:**
- Modify: `server/internal/config/config.go`, `server/internal/config/config_test.go`
- Create: `server/internal/store/backup.go`, `server/internal/store/backup_test.go`
- Modify: `server/internal/store/migrate.go`, `server/internal/store/migrate_test.go`

**Interfaces:**
- Produces: `config.Config.BackupDir string`; `func (s *Store) Ping(ctx context.Context) error`; `func (s *Store) VacuumInto(ctx context.Context, path string) error`; `func QuickCheck(ctx context.Context, path string) error`.

- [ ] **Step 1: Failing tests**
  - `TestLoadBackupDirDefault`: `DB_PATH=/x/y/anki-app.db` → `BackupDir == filepath.Join("/x/y", "backups")`; no `DB_PATH` → `filepath.Join(".", "backups")`.
  - `TestLoadBackupDirOverride`: `BACKUP_DIR=/b` → `"/b"` (trimmed like other vars).
  - `TestVacuumIntoIncludesWALRows`: open store, create a deck (row sits in the WAL, no checkpoint), `VacuumInto(dst)`, `QuickCheck(dst) == nil`, open `dst` with `Open` and the deck is there.
  - `TestVacuumIntoRefusesExistingFile`: target exists → error.
  - `TestQuickCheckRejectsGarbage`: a file of 4096 random bytes → error.
  - `TestPingFailsWhenClosed`: `Ping` nil when open, non-nil after `Close`.
  - `TestOpenRefusesNewerSchema`: set `PRAGMA user_version = <latest+1>` on a migrated db, close, `Open` fails with exactly `database schema v<latest+1> is newer than this build (v<latest>)`.
- [ ] **Step 2: Run** `cd server && go test ./internal/config ./internal/store` → the new tests FAIL.
- [ ] **Step 3: Implement**
  - `Ping`: `SELECT 1`.
  - `VacuumInto`: `VACUUM INTO ?` with the path bound.
  - `QuickCheck`: `sql.Open` on `file:<ToSlash(path)>?mode=ro&immutable=1` (the file is a private, finished snapshot, so `immutable` avoids creating `-shm`), `PRAGMA quick_check`, first row must be `ok`, otherwise `fmt.Errorf("quick_check: %s", row)`. Always close.
  - Guard in `migrate`: after reading `current`, if `current >` highest embedded version, return the exact message.
- [ ] **Step 4: Run** the same command → PASS; `go test ./internal/archtest` → PASS.
- [ ] **Step 5: Commit** `feat(store): backup primitives, migration guard; config BACKUP_DIR`

### Task 2: Backup names, retention and the schedule rule (pure)

**Files:**
- Create: `server/internal/backup/names.go`, `names_test.go`, `retention.go`, `retention_test.go`

**Interfaces:**
- Produces:
  - `type Kind string`; `const Daily Kind = "daily"`, `PreDeploy Kind = "pre-deploy"`.
  - `func FileName(t time.Time) string` → `anki-app-20261105T030000Z.db` (UTC).
  - `func ParseName(name string) (time.Time, bool)` (pattern above; `.partial` and anything else → false).
  - `func List(dir string) ([]time.Time, error)` — backup times in `dir`, ascending; a missing folder → `nil, nil`.
  - `func Latest(dir string) (time.Time, bool, error)`.
  - `func Keep(backups []time.Time, now time.Time, loc *time.Location) (keep, drop []time.Time)` — §4.3.
  - `func KeepNewest(backups []time.Time, n int) (keep, drop []time.Time)`.
  - `func Due(now time.Time, loc *time.Location, daily []time.Time) bool` — §4.4.

- [ ] **Step 1: Failing tests** (all with `loc = time.LoadLocation("Europe/London")`)
  - Names: round trip `FileName`/`ParseName`; rejects `anki-app-20261105T030000Z.db.partial`, `anki-app.db`, `anki-app-2026115T030000Z.db`, `ANKI-APP-…`; `List` ignores those and subfolders, returns ascending.
  - `TestKeepTierBoundaries`: single backups at ages 6/7 (daily), 31/32 (newest of its week), 92/93 (newest of its month), 366/367 (newest of its quarter); first of each pair kept, second dropped when no other tier keeps it (build fixtures so only the tier under test applies).
  - `TestKeepWeekIsMondayToSunday`: Sun 2026-10-11 and Mon 2026-10-12 backups at age > 6 are in different weeks.
  - `TestKeepMissingSundayUsesSaturday`: week with Mon–Sat backups, no Sunday → Saturday kept at age 20.
  - `TestKeepMonthAndQuarterEnds`: 31 Mar vs 1 Apr; 30 Sep vs 1 Oct.
  - `TestKeepAcrossDST`: daily backups at 00:30 London across 2026-03-29 and 2026-10-25; each lands on its own local date (no two backups share a date, none is missing).
  - `TestKeepTwoYearsOfDailies`: simulate one backup a day at 03:05 London from 2026-01-01 to 2027-12-31, running `Keep` each day and keeping only `keep`; `len(keep) <= 20` every day; on 2027-12-31 the survivors include 2027-12-25…2027-12-31, 2027-09-30 (newest of Q3), 2027-10-31 (newest of October).
  - `TestKeepNeverDropsFutureBackups`: a backup dated tomorrow is in `keep`.
  - `TestKeepNewest`: 12 → keep newest 10, drop oldest 2; 3 → keep all.
  - `TestDue`: 02:59 local, none today → false; 03:00 → true; 03:00 with one at 03:00 today → false; last backup 4 days ago at 10:00 → true; in summer, a backup at 00:30 *today local* (23:30 UTC yesterday) and now 03:10 local → false (dates are compared in `loc`, not UTC).
  - `TestDueIgnoresFutureBackups`: only backup is dated tomorrow, now 03:10 → true.
- [ ] **Step 2: Run** `cd server && go test ./internal/backup` → FAIL (undefined).
- [ ] **Step 3: Implement.** Local date = `t.In(loc)` year/month/day; age = days between `time.Date(y,m,d,0,0,0,0,time.UTC)` values (immune to DST). Negative age → keep. Week key = local date minus `(weekday+6)%7` days. Quarter key = year, `(month-1)/3`. "Newest of its period" is over all inputs. Output slices ascending.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(backup): file names, retention and schedule rule`

### Task 3: Backup writer and scheduler loop

**Files:**
- Create: `server/internal/backup/writer.go`, `writer_test.go`, `scheduler.go`, `scheduler_test.go`

**Interfaces:**
- Consumes: Task 1 `(*store.Store).VacuumInto`, `store.QuickCheck`; Task 2 everything.
- Produces:
  - `type Snapshotter interface { VacuumInto(ctx context.Context, path string) error }` (satisfied by `*store.Store`).
  - `type Writer struct { Src Snapshotter; Root string; Clock clock.Clock; Loc *time.Location; Log *slog.Logger }`
  - `func (w *Writer) Write(ctx context.Context, kind Kind) (string, error)` — absolute path of the new backup; prunes that folder afterwards (`Keep` for daily, `KeepNewest(…,10)` for pre-deploy). Prune errors are logged at ERROR, not returned.
  - `func (w *Writer) RunScheduler(ctx context.Context, every time.Duration)` — checks at once, then every `every`, until `ctx` is done.

- [ ] **Step 1: Failing tests** (real `store` in `t.TempDir()`, `clock.Fake`)
  - `TestWriteDaily`: path is `Root/daily/<FileName(now)>`, opens with the same rows, folders exist with mode `0700` (skip the mode check on Windows).
  - `TestWriteRemovesLeftoverPartials`: a leftover `anki-app-20260101T000000Z.db.partial` is removed; `notes.txt` and `anki-app.db` in the folder survive.
  - `TestWriteFailureLeavesNoPartial`: a `Snapshotter` that writes some bytes to the path then errors → `Write` errors, folder has no `.partial`.
  - `TestWriteQuickCheckFailure`: snapshotter writes garbage and returns nil → error, no `.partial`, no final file.
  - `TestPreDeployKeepsTen`: 12 writes, clock advanced 1 s each → exactly 10 backups; an unrelated file survives.
  - `TestDailyAppliesRetention`: pre-seed `daily/` with empty files named for 40 consecutive days → after one `Write`, only `Keep`'s survivors plus the new one remain.
  - `TestSchedulerTakesOneBackupPerDay`: fake clock at 03:10 London, `every = 10ms`, run 100 ms → exactly one daily backup; cancel returns promptly.
  - `TestSchedulerLogsFailureAndContinues`: failing snapshotter → an `ERROR` log line, the function keeps running until cancelled.
- [ ] **Step 2: Run** `go test ./internal/backup` → FAIL.
- [ ] **Step 3: Implement** §4.2 steps 0–4 in order: remove leftover `.partial`s by exact name, `VacuumInto(<name>.partial)`, `QuickCheck`, open+`Sync`+close, `os.Rename`, then best-effort fsync of the folder (ignore its error on Windows). On any failure `os.Remove` that one `.partial`. Log `backup written` with name, size, duration.
- [ ] **Step 4: Run** → PASS, `go vet ./...` clean.
- [ ] **Step 5: Commit** `feat(backup): writer with quick_check and the daily scheduler`

### Task 4: `backup` subcommand, scheduler in `serve`, `GET /api/health`

**Files:**
- Modify: `server/cmd/anki-app/main.go`, `main_test.go`
- Create: `server/cmd/anki-app/backup.go`, `backup_test.go`
- Create: `server/internal/api/health.go`, `health_test.go`
- Modify: `server/internal/api/api.go` (route, `Deps`), `server/internal/api/auth.go` (exemption)

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces: `api.Deps.Version string`, `api.Deps.LastBackup func() (time.Time, bool, error)` (nil → `lastBackupAt: null`). JSON: `{"version": string, "lastBackupAt": number|null}`.

- [ ] **Step 1: Failing tests**
  - `TestBackupCommandDaily` / `TestBackupCommandPreDeploy`: with `DB_PATH` and `BACKUP_DIR` in a temp dir, stdout is one line: an absolute path under `daily/` (or `pre-deploy/`) that exists.
  - `TestBackupCommandRejectsUnknownArgs`: `backup --nope`, `backup --pre-deploy x` → the exact usage error.
  - `TestUsageText`: unknown command → exact usage string from Global Constraints.
  - `TestServeWaitsForSchedulerBeforeClosingStore`: run `serve` with a temp DB at a time after 03:00 (change the signature to `run(ctx, args, getenv, clk clock.Clock, stdout, stderr)`; `main` passes `clock.Real{}`, tests pass a `*clock.Fake`; update existing call sites in `main_test.go` and `import_test.go`), wait for `backup written` in the log, cancel, `run` returns nil and the daily backup exists. Tests must not depend on wall-clock hour.
  - Health (`helpers_test.go` server with `Version: "abc123"`): `lastBackupAt` null with nil `LastBackup`; set to `t0` ms when it returns `t0, true`; `200` without a session when `Passphrase` is set; `403` with Host `evil.example`; `503` + code `unavailable` after `store.Close()`.
  - `TestHealthLastBackupIgnoresPartials`: wired with `backup.Latest` over a folder holding one real backup and a newer `.partial` → the real one's time.
- [ ] **Step 2: Run** `go test ./cmd/... ./internal/api` → FAIL.
- [ ] **Step 3: Implement**
  - `run` gains `case "backup"`; `runBackup(ctx, preDeploy bool, getenv, stdout, clk)` opens the store, `Writer{…}.Write`, prints the path.
  - `serve` starts `w.RunScheduler(ctx, 15*time.Minute)` in a goroutine and waits for it (`sync.WaitGroup` or done channel) after `srv.Shutdown` and before the deferred `st.Close`. Pass `Version: version`, `LastBackup: func() { return backup.Latest(filepath.Join(cfg.BackupDir, string(backup.Daily))) }`.
  - Health handler: `Ping` error → `writeError(w, 503, "unavailable", "database unavailable")`; `LastBackup` error → log at ERROR, `lastBackupAt: null`.
  - `authMiddleware`: also exempt `GET /api/health`. Update the comment.
  - Update the package doc comment in `main.go` with the backup line.
- [ ] **Step 4: Run** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat: backup subcommand, scheduled backups, GET /api/health`

### Task 5: Settings version line (web)

**Files:**
- Modify: `web/src/api/types.ts`, `web/src/api/client.ts`, `web/src/test/mockApi.ts`, `web/src/pages/SettingsPage.tsx`, `web/src/pages/SettingsPage.test.tsx`, `web/src/styles/base.css`, `web/playwright.config.ts`
- Create: `web/src/settings/versionLine.ts`, `web/src/settings/versionLine.test.ts`

**Interfaces:**
- Consumes: the JSON contract from Task 4 (can be built before it).
- Produces: `interface Health { version: string; lastBackupAt: number | null }`; `api.health(): Promise<Health>`; `versionLine(h: Health, timeZone?: string): string`.

- [ ] **Step 1: Failing tests**
  - `versionLine({version:'3f2a9c1d0b7e', lastBackupAt: Date.UTC(2026,10,5,3,0)}, 'Europe/London')` → `'Version 3f2a9c1d0b7e · Last backup 5 Nov, 03:00'`; summer: `Date.UTC(2026,6,1,2,0)` in London → `'… Last backup 1 Jul, 03:00'`; `lastBackupAt: null` → `'Version 3f2a9c1d0b7e · Last backup: none yet'`.
  - SettingsPage: health resolves → the line text is shown at the bottom (after Account); health rejects → no element matching `/^Version /`; the rest of the page still renders.
- [ ] **Step 2: Run** `cd web && npm test` → FAIL.
- [ ] **Step 3: Implement.** Build the date from `Intl.DateTimeFormat('en-GB', {day:'numeric', month:'short', hour:'2-digit', minute:'2-digit', hourCycle:'h23', timeZone}).formatToParts` as `${day} ${month}, ${hour}:${minute}` (device zone when `timeZone` is undefined). `VersionLine` component: `<p className="muted version-line">`, rendered only after a successful fetch. CSS: `overflow-wrap: anywhere`. In `playwright.config.ts` add `BACKUP_DIR: join(tmpdir(), \`anki-e2e-backups-${Date.now()}\`)` to `webServer.env` so e2e runs don't write into the shared temp root.
- [ ] **Step 4: Run** `npm run lint && npm run typecheck && npm test` → PASS.
- [ ] **Step 5: Commit** `feat(web): version and last-backup line on Settings`

**After Task 5 and Task 4 are merged (main session):** dispatch `e2e-tester`: "Settings page gained a muted version/last-backup line at the bottom (from `GET /api/health`). Pages: Settings. Widths: iPhone 15 and 1440 desktop. Update `layout.spec.ts`: the line is visible and causes no horizontal overflow; write tests, then run the full suite." Then `bash scripts/check.sh`, show output, merge.

## M2 — `feat/container-ci`

### Task 6: Dockerfile, `.dockerignore`, `scripts/docker-smoke.sh`

**Files:** Create `Dockerfile`, `.dockerignore`, `scripts/docker-smoke.sh`.

- [ ] **Step 1: Resolve digests** with `docker buildx imagetools inspect <image>` (index digest) for `node:26-bookworm-slim`, `golang:1.27.3-bookworm`, `gcr.io/distroless/static-debian12:nonroot`. Write them as `FROM image:tag@sha256:… AS stage`.
- [ ] **Step 2: Write the Dockerfile** per §5.1. Specifics the spec leaves open:
  - `web` stage: `WORKDIR /src/web`, `mkdir -p /src/server/internal/webui/dist` first (the build script writes `.gitkeep` there), copy `web/package*.json` + `.npmrc`, `npm ci`, then the rest of `web/`, `npm run build`.
  - `build` stage: `WORKDIR /src/server`, copy `go.mod`/`go.sum`, `go mod download`, copy `server/`, copy `dist/` from `web`, build to `/out/anki-app`. Also `mkdir /out/data`.
  - final: `COPY --from=build --chown=65532:65532 /out/data /data` so a fresh named volume is writable by uid 65532; `ENV DB_PATH=/data/anki-app.db BACKUP_DIR=/data/backups`; `ARG VERSION=dev`; labels per §5.1.
  - `.dockerignore` exactly per §5.1.
- [ ] **Step 3: Write `scripts/docker-smoke.sh`** per §5.2 (`set -euo pipefail`, `step()` like `check.sh`, a `trap` that stops the container and removes the volume **by its exact name**). Version: `git describe --always --dirty`. uid check: `docker top <ctr> -o uid` (or `-eo uid,comm`) shows `65532`. Backup check: last stdout line of `docker run --rm -v <vol>:/data anki-app:smoke backup` matches `^/data/backups/daily/anki-app-[0-9]{8}T[0-9]{6}Z\.db$`.
- [ ] **Step 4: Run** `bash scripts/docker-smoke.sh` → ends with `SMOKE PASSED` (needs Docker Desktop running; if not, ask Marc).
- [ ] **Step 5: Commit** `feat: Dockerfile and local docker smoke test`

### Task 7: GitHub Actions workflow and the shellcheck step

**Files:** Create `.github/workflows/main.yml`. Modify `scripts/check.sh` (and any existing script shellcheck flags).

- [ ] **Step 1: Resolve action SHAs** with `git ls-remote --tags https://github.com/<owner>/<action>` (use the peeled `^{}` commit for annotated tags), latest major of: `actions/checkout`, `actions/setup-go`, `actions/setup-node`, `actions/cache`, `docker/setup-buildx-action`, `docker/login-action`, `docker/build-push-action`, `actions/delete-package-versions`. Pin as `uses: owner/action@<sha> # vX.Y.Z`.
- [ ] **Step 2: Write the workflow** per §6 exactly (triggers, concurrency group `main` with cancel, top-level `permissions: contents: read`, job `check` on `ubuntu-24.04` with `timeout-minutes: 25`, job `image` with `needs: check` and `packages: write`). Playwright cache path `~/.cache/ms-playwright`, key `playwright-${{ runner.os }}-<version>` where the version is read in a step with `node -p "require('./web/package-lock.json').packages['node_modules/@playwright/test'].version"`. `VERSION` and the `sha-` tag from `${GITHUB_SHA::12}` set in a step output. Buildx cache `type=gha`. Prune: one `delete-package-versions` step with `delete-only-untagged-versions: 'true'`, one keeping `min-versions-to-keep: 10` — check the action's docs that the second can never delete the version tagged `main` (it is always the newest), and say in a comment why it is safe.
- [ ] **Step 3: Add the shellcheck step** to `check.sh` after the Go steps: if `command -v shellcheck` → `shellcheck scripts/*.sh` plus every file in `deploy/vm/` whose first line is a bash shebang (the folder appears in M3; handle its absence); else print `shellcheck not installed: skipped`.
- [ ] **Step 4: Verify.** Run shellcheck over the current scripts via `docker run --rm -v "$PWD:/mnt" -w /mnt koalaman/shellcheck:stable scripts/*.sh` and fix every finding (they must pass in CI). Validate the YAML parses (`node -e` with a quick parse, or `docker run --rm -v "$PWD:/repo" rhysd/actionlint:latest -color` if available). Then `bash scripts/check.sh` → `ALL CHECKS PASSED` with `shellcheck not installed: skipped`.
- [ ] **Step 5: Commit** `ci: check and image workflow; shellcheck in the done bar`

**After M2 merges to `main` and is pushed (main session, with Marc):** watch the Actions run; confirm `:main` and `:sha-<12>` exist in GHCR (§11.3 steps 1–2). Ask Marc to check that the package is private and linked to the repo.

## M3 — `feat/vm`

### Task 8: `lib.sh`, `anki-update` and the stub-based test harness

**Files:**
- Create: `deploy/vm/lib.sh`, `deploy/vm/anki-update`
- Create: `deploy/vm/test/run.sh`, `deploy/vm/test/stubs/{docker,systemctl,curl,flock,chown}`
- Modify: `scripts/check.sh` (run `deploy/vm/test/run.sh` when `jq` is installed; else print `vm script tests: jq not installed: skipped`)

**Interfaces:**
- Produces (`lib.sh`, sourced from `$(dirname "$0")/lib.sh`):
  - Paths, overridable by env for tests: `ANKI_ETC=/etc/anki-app`, `ANKI_VAR=/var/lib/anki-app`, `ANKI_LOCK=/run/anki-update.lock`, `ANKI_REPO=ghcr.io/marccowan/anki-app`, `HEALTH_URL=http://127.0.0.1:8080/api/health`, `HEALTH_TIMEOUT=60`, `HEALTH_INTERVAL=2`. Derived: `DATA="$ANKI_VAR/data"`, `DEPLOY="$ANKI_VAR/deploy"`.
  - `log <msg>` (stderr, prefixed with the script name), `read_image_env` (sets `IMAGE`, `PREVIOUS`, empty if absent), `write_image_env <image> <previous>` (temp file + `mv`), `predeploy_backup <image>` (runs the §7.5 step 4 `docker run`, takes the last stdout line, maps `/data/` → `$DATA/`, echoes the host path; non-zero on failure or a path that doesn't exist), `restore_db <file>` (§7.5 step 7.2 exactly), `health_check <want>` (0 when status 200 and `.version == want` within the timeout).

- [ ] **Step 1: Write the harness and failing tests.** `run.sh` creates a fresh temp folder per test (`mktemp -d`), sets the path vars into it, puts `stubs/` first on `PATH`, and checks exit code, files and a call log. Stubs append `"<cmd> <args>"` to `$STUB_LOG` and behave per env vars (`STUB_PULL_FAIL`, `STUB_DIGEST`, `STUB_REVISION`, `STUB_BACKUP_FAIL`, `STUB_HEALTH_VERSION` / `STUB_HEALTH_STATUS` per image, …). The docker stub's `run … backup --pre-deploy` writes a copy of `$DATA/anki-app.db` to `$DATA/backups/pre-deploy/anki-app-<ts>.db`, prints `level=INFO msg=…` on stderr and `/data/backups/pre-deploy/anki-app-<ts>.db` on stdout. Tests (each a bash function):
  - `paused_without_arg_does_nothing` — no `docker pull` in the log.
  - `same_digest_exits_0` — no `systemctl stop`.
  - `bad_digest_skipped_without_arg`, `bad_digest_deployed_with_explicit_arg`.
  - `pull_failure_leaves_app_running` — exit 1, no `systemctl stop`, `image.env` unchanged.
  - `happy_path` — order: pull, stop, backup with the **old** image, `image.env` = `IMAGE=<new>`/`PREVIOUS=<old>`, start, `deployed <want>` logged, `docker image prune -f --filter until=168h`, exit 0.
  - `backup_failure_restarts_old` — `systemctl start`, exit 1, `image.env` unchanged.
  - `rollback_restores_predeploy_backup` — new image unhealthy → db content equals the backup, `-wal`/`-shm` gone, `IMAGE=<old>`, digest in `bad-digests`, exit 1.
  - `rollback_unhealthy_exits_2` — logs `MANUAL ATTENTION NEEDED`, exit 2.
  - `first_deploy_enables_service` — no `image.env`, no db: no backup call, pass → `systemctl enable anki-app`; `first_deploy_failure_stops` → `systemctl stop`, exit 1, no `bad-digests` restore attempt on a missing backup.
- [ ] **Step 2: Run** inside Linux (jq is not on the laptop): `docker run --rm -v "$PWD:/repo" -w /repo debian:12-slim bash -c 'apt-get update -qq && apt-get install -y -qq jq >/dev/null && bash deploy/vm/test/run.sh'` → tests FAIL (no `anki-update`).
- [ ] **Step 3: Implement `anki-update`** per §7.5 steps 1–7 and "First deploy". `set -euo pipefail`. Lock: `exec 9>"$ANKI_LOCK"; flock -n 9 || exit 0`. Digest: the `RepoDigests` entry whose repository equals the ref's repository (strip tag/digest). `WANT` from label `org.opencontainers.image.revision`. Health request: `curl -s -o <tmp> -w '%{http_code}' -H 'Host: 127.0.0.1' "$HEALTH_URL"`.
- [ ] **Step 4: Run** the docker command → all PASS; shellcheck via `koalaman/shellcheck:stable` on `deploy/vm/lib.sh deploy/vm/anki-update deploy/vm/test/run.sh deploy/vm/test/stubs/*` → clean.
- [ ] **Step 5: Commit** `feat(vm): pull-based updater with backup, health check and rollback`

### Task 9: `anki-restore`, `anki-offsite`, systemd units, `setup.sh`

**Files:**
- Create: `deploy/vm/anki-restore`, `deploy/vm/anki-offsite`, `deploy/vm/setup.sh`
- Create: `deploy/vm/systemd/{anki-app.service,anki-update.service,anki-update.timer,anki-offsite.service,anki-offsite.timer}`
- Modify: `deploy/vm/test/run.sh`, `deploy/vm/test/stubs/` (add `gcloud`)

**Interfaces:**
- Consumes: Task 8 `lib.sh` functions.
- Produces: `/etc/anki-app/offsite.env` holding `BUCKET=<name>` (root, `0644`, written by Marc; not a secret).

- [ ] **Step 1: Failing tests** in `run.sh`:
  - `restore_requires_yes_or_confirmation` (stdin `n` → exit 1, no `systemctl stop`).
  - `restore_happy_path` (`--yes <file>`): pre-deploy backup taken first, db content equals `<file>`, `-wal`/`-shm` gone, app started, health checked against the current image's revision, exit 0.
  - `restore_missing_file` → exit 1 before stopping anything.
  - `offsite_never_deletes`: gcloud stub log has `storage rsync --recursive $DATA/backups gs://<bucket>/backups` and no `--delete-unmatched-destination-objects`; missing `offsite.env` → exit 1.
- [ ] **Step 2: Run** (docker command from Task 8) → FAIL.
- [ ] **Step 3: Implement.**
  - `anki-restore [--yes] <file>` per §7.6; the health target is the revision label of the current `IMAGE`. On a failed health check, exit 1 and print the pre-restore backup path.
  - `anki-offsite` per §7.7.
  - Units: `anki-app.service` verbatim from §7.4. `anki-update.service`: `Type=oneshot`, `ExecStart=/opt/anki-app/bin/anki-update`. `anki-update.timer`: `OnBootSec=1min`, `OnUnitActiveSec=1min`, `AccuracySec=5s`. `anki-offsite.timer`: `OnCalendar=*-*-* 04:00:00 Europe/London`, `Persistent=true`.
  - `setup.sh` (idempotent, root, run from its own folder after `scp -r deploy/vm anki:/tmp/`): everything in §7.2 "Packages" and "Tuning"; set the system time zone to `Europe/London` (so `Automatic-Reboot-Time "04:30"` means London); unattended-upgrades with `Automatic-Reboot "true"`; create the §7.3 folders with owners and modes; install `lib.sh` + the three scripts into `/opt/anki-app/bin/` and the units; `systemctl daemon-reload`; enable and start both **timers**; **don't** enable `anki-app.service`. Each step checks before acting (re-running changes nothing). At the end, print the manual steps from §7.2 that remain.
- [ ] **Step 4: Run** tests → PASS; shellcheck clean on all of `deploy/vm/`; `systemd-analyze verify` on the units inside a `debian:12` container if practical (otherwise review by eye against §7.4).
- [ ] **Step 5: Commit** `feat(vm): restore, off-site copy, systemd units and host setup`

### Task 10: `docs/runbooks/vm.md`

**Files:** Create `docs/runbooks/vm.md`.

- [ ] **Step 1: Write** the runbook covering, in order: §12 checks (each a `- [ ]` box to confirm first); GCP resources (§7.1: project, billing budget $1 at 50%/100%, service account `anki-vm` with no project roles, VM settings, firewall rule deletions, bucket settings + lifecycle + the two bucket-level roles), with `gcloud` commands where they are simpler than console clicks; host setup (§7.2: `scp`, `setup.sh`, then `tailscale up --ssh --hostname anki`, disable key expiry, `tailscale serve --bg --https=443 http://127.0.0.1:8080`, classic PAT `read:packages` 1 year + `docker login ghcr.io`, writing `anki-app.env` with a generated passphrase (`openssl rand -base64 24`) and `offsite.env`); first deploy (`anki-update`); acceptance §11.3 steps 3–8 as checkboxes; cutover §9; operations table §7.6; PAT renewal; backup restore drill.
- [ ] **Step 2: Check** every path, unit name and command against §7 and Tasks 8–9 file names.
- [ ] **Step 3: Commit** `docs: VM runbook`

**After M3 (main session, with Marc):** Marc follows the runbook. Use AskUserQuestion at each hand-off (resources created; setup done; acceptance results). Fix any failure in code on `feat/vm`, then merge when §11.3 steps 3–8 pass.

## M4 — `feat/cutover`

### Task 11: Retire the Windows scripts, update docs

**Files:** Delete `scripts/deploy.sh`, `scripts/kill.sh`, `scripts/lib/winproc.sh`. Modify `CLAUDE.md`, `README.md`, `.claude/agents/e2e-tester.md` if it mentions them.

- [ ] **Step 1:** `grep -rn "deploy.sh\|kill.sh\|winproc" --exclude-dir=node_modules .` and list every hit outside `docs/specs`, `docs/plans`.
- [ ] **Step 2:** Delete the three files by name (`git rm`). Edit CLAUDE.md per §10 (deploy line, operations table, runbook path, `docs/ideas/` convention: "Ideas not yet scheduled are bookmarked in `docs/ideas/YYYY-MM-DD-<name>.md`", remove kill-switch line). README: replace Deploy, Backup, Phone access and "Moving to a VM later" with the VM summary, the operations table and a pointer to the runbook; add `BACKUP_DIR` to the configuration table.
- [ ] **Step 3: Run** `bash scripts/check.sh` → `ALL CHECKS PASSED`; repeat the grep → only spec/plan hits.
- [ ] **Step 4: Commit** `chore: retire Windows deploy scripts; document VM operations`

### Task 12: Cutover (main session with Marc)

- [ ] Follow §9 / the runbook cutover section step by step, using AskUserQuestion for each step Marc performs (freeze, laptop backup via `DB_PATH=… BACKUP_DIR=<fresh folder> server/anki-app backup`, `scp`, VM swap, count check, phone, `tailscale serve reset`).
- [ ] Merge `feat/cutover` once the phone uses the VM.
