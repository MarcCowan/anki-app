# Faster checks: run only what a change needs

Status: **reviewed** 2026-11-23

Agents ran every check (`bash scripts/check.sh`, about 5 minutes on Windows) even for a Markdown-only change, and CI
ran all of it on every PR. Most of that is wasted: nothing in a docs change can fail Go tests or Playwright.

## Where the time goes

One timed run on Windows (the Go and npm groups ran side by side, so those numbers are a little high):

| Step | Cost |
|---|---|
| Go: vet 12s, staticcheck 15s, test 20s, govulncheck 9s | ~1 min |
| shellcheck | seconds (not installed here, so skipped locally) |
| `deploy/vm/test/run.sh` | ~130s (many bash subprocesses; slow on Windows) |
| leakscan tests, Claude hook tests | over 3 min together (bash and gitleaks process spawning) |
| web: lint 13s, typecheck 15s, Vitest 28s, build 10s, audit 3s | ~1 min |
| Playwright e2e (33 tests, iPhone 16 and desktop projects) | ~100s |
| `npm ci` (not in check.sh, needed on a fresh worktree) | ~33s |

The biggest items are the bash test suites (`deploy/vm`, leakscan), which only matter when `deploy/vm/`, `scripts/`,
`.githooks/` or `.claude/hooks/` change, and e2e, which only matters for the UI and the API it calls.

## Decisions

1. **The path map lives in CI YAML, not in bash.** GitHub can only put `paths:` on a whole workflow, and a workflow
   skipped that way leaves its required status check pending, which blocks the merge. A job skipped by its own `if:`
   reports success instead. So a `changes` job runs `dorny/paths-filter` (pinned by commit SHA; it needs
   `pull-requests: read`), one job per group runs `if:` its filter matched, and one summary job, `all-checks`, is the
   required check. CI is the authority. Locally, `check.sh` takes group names and the agent picks them from the
   table in CLAUDE.md; a wrong local pick costs a red PR, not a bad merge.
2. **Changes to the API, the binary's entry point, its config or the embed run e2e on the PR.** e2e drives the real
   binary with the embedded web build, so a CSP, Host/Origin or JSON-field change can pass every Go test and still
   break the browser. The store and migrations stay backend-only.
3. **Local runs are parallel with short output.** With more than one group, `check.sh` runs them side by side, logs
   each to a file and prints one line per group plus the end of each failing log. Agents run it as a background task.
   Groups that share build output (`backend`, `frontend`, `e2e`: the Vite build writes the directory the Go binary
   embeds) run one after another in one lane; the bash suites get their own lanes.
4. **actionlint** joins as the `workflows` group, pinned as a Go tool like staticcheck.

## Groups

| Group | Runs | Paths that turn it on |
|---|---|---|
| `backend` | gofmt, go vet, staticcheck, go test, govulncheck | `server/**` |
| `frontend` | ESLint, tsc, Vitest, Vite build, `npm audit --omit=dev` | `web/**` |
| `e2e` | Playwright, iPhone 16 and desktop projects | `web/**`, `server/cmd/anki-app/**`, `server/internal/api/**`, `server/internal/config/**`, `server/internal/webui/**` |
| `scripts` | shellcheck (scripts, hooks), leakscan, hook and bump-gitleaks tests | `scripts/**`, `.githooks/**`, `.claude/hooks/**`, `.claude/settings.json`, `.gitleaks.toml` |
| `vm` | shellcheck (deploy/vm), VM script tests | `deploy/vm/**` |
| `workflows` | actionlint | `.github/workflows/**` |
| `docker` | `scripts/docker-smoke.sh` (CI only, needs Docker) | `Dockerfile`, `.dockerignore`, `scripts/docker-smoke.sh` |

- Markdown anywhere, `.gitignore` files, `.gitattributes`, `.gitleaksignore`, `LICENSE.md` and
  `.github/dependabot.yml` turn on nothing. The leak scan (`secrets` job and the git hooks) is separate and always
  runs.
- **`scripts/check.sh` and `.github/workflows/**` turn on everything**, since they change how every group runs.
- **Any other path turns on everything.** A second filter matches files outside every known pattern. The map
  fails towards running more, never less.
- Renamed and deleted files count at both paths (paths-filter lists a rename as an add plus a delete).
- Pushes to `main`, the weekly schedule and manual runs ignore the filters and run every group except `docker`
  (the `image` job builds the image there). `all-checks` fails on those events unless every one of those groups
  succeeded, so nothing reaches the VM without the whole set.

What could slip through: a wrong map entry, or a cross-cutting change the map misses. The backstops are the
run-everything default for unknown paths and the full run on `main` before the `:main` image is pushed. A miss then
costs a red `main` and no deploy until a revert or fix lands, not a bad deploy.

## Rejected

- **A bash map shared by CI and agents.** One source of truth, but it needs its own tests, and CI jobs would lose
  their readable per-group names and results.
- **e2e on one project for non-layout changes.** Saves ~50s for a rule that is easy to get wrong.
- **Haiku subagents to run the checks.** The checks already give a definite result; a model in the middle adds
  tokens and the risk of a wrong summary, and runs nothing faster. The script's short summary does the same job.
- **Speeding up the bash suites.** Worth a look later; it is a refactor of test code.

## Steps

1. `check.sh`: group arguments (none or `--all` runs everything), the lanes and the summary output.
2. actionlint as a Go tool, in the `workflows` group.
3. CI: `changes`, one job per group, `all-checks`; `image` needs `all-checks`. Then change the `main` ruleset's
   required check from `check` to `all-checks`.
4. CLAUDE.md, `web/CLAUDE.md`, the README and the `e2e-tester` brief: the group table and the new rule.
