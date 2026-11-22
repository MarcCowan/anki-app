# Faster checks: run only what a change needs

Status: **draft**, awaiting review.

Agents run the whole done bar (`bash scripts/check.sh`, about 5 minutes on Windows) even for a Markdown-only change,
and CI runs all of it on every PR. Most of that is wasted: nothing in a docs change can fail Go tests or Playwright.

## Where the time goes

One timed run on Windows (the Go and npm groups ran side by side, so those numbers are a little high):

| Step | Cost |
|---|---|
| Go: vet 12s, staticcheck 15s, test 20s, govulncheck 9s | ~1 min |
| shellcheck | seconds (not installed here, so skipped locally) |
| `deploy/vm/test/run.sh` | ~130s (many bash subprocesses; slow on Windows) |
| leakscan tests, Claude hook tests | over 3 min together (bash and gitleaks process spawning; still running when this was written, so no exact figure) |
| web: lint 13s, typecheck 15s, Vitest 28s, build 10s, audit 3s | ~1 min |
| Playwright e2e (33 tests, iPhone 16 and desktop projects) | ~100s |
| `npm ci` (not in check.sh, needed on a fresh worktree) | ~33s |

Total is 8+ minutes in this run, a bit more than the 5 minutes agents usually see because of the side-by-side runs.
The biggest items are the bash test suites (`deploy/vm`, leakscan), which only matter when `deploy/vm/`, `scripts/`,
`.githooks/` or `.claude/hooks/` change, and e2e, which only matters for UI.

## Change-to-checks map

Each changed path turns on a group. A change can turn on several; the union runs.

| Changed path | Checks |
|---|---|
| `*.md`, `docs/`, `.gitignore`, other inert config | none beyond the commit hooks' leak scan |
| `server/**` (not `go.mod`/`go.sum`) | gofmt, vet, staticcheck, `go test` |
| `server/go.mod`, `server/go.sum` | the Go group plus `govulncheck` |
| `web/src/**`, `web/index.html`, Vite/TS/ESLint config | lint, typecheck, Vitest, build; e2e if UI-affecting (below) |
| `web/package.json`, `web/package-lock.json` | the web group plus `npm audit --omit=dev` and e2e |
| `web/e2e/**`, `web/playwright.config.ts` | e2e (both projects) |
| `scripts/**`, `.githooks/**`, `.claude/hooks/**`, `.claude/settings.json` | shellcheck, leakscan tests, hook tests |
| `.gitleaks.toml`, `scripts/gitleaks.version` | the same, since the leakscan tests exercise the rules |
| `deploy/vm/**` | shellcheck, VM script tests |
| `.github/workflows/**` | actionlint (new), and the PR's own CI run |
| `Dockerfile`, `scripts/docker-smoke.sh` | `docker-smoke.sh` where Docker is available; otherwise CI's `image` build on main |
| `scripts/check.sh` itself, or any path the map doesn't know | everything (the safe default) |

Edge cases:

- **UI-affecting** means anything under `web/src/` that renders (components, pages, CSS, the Markdown renderer) or
  `web/index.html`. Pure logic under `web/src/api/` with no render path can skip e2e. When in doubt, run e2e; it is
  only about 100s.
- **`server/` changes that e2e covers.** e2e runs the real binary with the embedded web build, so a change to an
  API handler, auth, the CSP or the Host/Origin checks, or the embed can break it. Rule: `server/internal/api/**`,
  `server/cmd/**` and the embed code also turn on the build and e2e. Store-only changes (the store package,
  migrations) stay Go-only, since the store and API tests already cover them.
- **API contract.** The frontend types mirror the API table in spec section 6. A change to either side of that
  contract turns on both groups.
- **Unknown paths** run everything. The map has to fail towards running more, never less.
- **Deleted and renamed files** count at both paths.

## Options

1. **`scripts/check.sh --changed [base]`.** Default base is `origin/main`. It reads
   `git diff --name-only base...HEAD` plus uncommitted and untracked files, picks groups from the map, prints each
   group it ran or skipped and why, and fails on any error. `--all` keeps today's behaviour. One script holds the
   map, so agents and CI cannot drift apart. Costs: some bash, plus a table-driven test for the map itself
   (path -> groups).
2. **CI `paths` filters.** Rejected as the main mechanism. A workflow or job skipped by a path filter leaves its
   required status check pending, which blocks the merge under the coming ruleset, and the map would then live in
   YAML as well as in the script. Better: one `check` job that always runs, and calls `check.sh --changed` on PRs
   and `--all` on `main`. A required check that always runs and decides internally cannot be skipped.
3. **Parallel steps.** The Go group, the web group and the bash suites are independent. Running them as three
   background jobs with buffered per-group output could cut the full run roughly in half. Cost: more script
   complexity and harder-to-read logs. Worth doing second, after `--changed`.
4. **Caching.** CI already caches Go modules, npm and Playwright browsers, and `go test` and `go vet` cache
   locally. Little left to gain: the slow steps are process spawning, not downloads.
5. **e2e on one project for non-layout changes.** A logic-only UI change could run the desktop project alone and
   save ~50s, for a rule that is easy to get wrong. Skip for now.
6. **Speed up the bash suites.** The VM and leakscan tests spawn many processes, which Windows does slowly. Worth a
   look, but it is a refactor of test code, and out of scope here.

## Recommendation

Do option 1 plus the always-running CI job from option 2, then option 3.

- Agents run `check.sh --changed`. Docs-only changes run nothing (the interim rule below).
- CI runs `--changed` on PRs and `--all` on every push to `main` and on the weekly schedule. `image` already needs
  the `main` run, so nothing reaches the VM without the whole bar.

What could slip through: a wrong map entry, or a cross-cutting change (a server change that breaks the UI) that
the map misses. The backstops are the run-everything default for unknown paths, a test that every tracked path
matches a group or an explicit "inert" entry, and the full run on `main` before the `:main` image is pushed. A miss
then costs a red `main` and no deploy, not a bad deploy. The leak scan (`secrets` job and the hooks) is separate and
never skipped.

## Rule changes

**Now (in this PR).** Pure Markdown and docs changes, and inert config such as `.gitignore`, skip
`scripts/check.sh`. The PR says "docs only, check.sh not run". Everything else still runs the full bar.

**After `--changed` lands.** Replace the done-bar line in CLAUDE.md with:

> Before saying anything is done: run `bash scripts/check.sh --changed` and show the output. It picks the checks for
> the files you changed and prints what it skipped. Pure docs changes run nothing beyond the commit hooks; say
> "docs only, check.sh not run" in the PR. Before merging, and for changes to `scripts/check.sh` or the CI workflow,
> run `bash scripts/check.sh --all`. If something fails, say so.

**Subagent briefs.** Implementer and reviewer briefs should say: "Verify with `bash scripts/check.sh --changed`; do
not run the full bar for docs-only changes. Do not dispatch `e2e-tester` unless the change touches the
UI-affecting paths in the map." The `e2e-tester` brief keeps asking for what changed, pages and widths, so it can run
one spec file while iterating; the final run is `--changed` or `--all`.

## Steps

1. Add `--changed` and `--all` and the map to `check.sh`, with a table-driven test for the map.
2. Add actionlint to the workflow group.
3. Make CI's `check` job call `--changed` on PRs and `--all` on `main`; make that one job the required check.
4. Run the groups in parallel.
5. Update CLAUDE.md, `web/CLAUDE.md` and the subagent briefs.
