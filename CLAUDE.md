# anki-app

Personal spaced-repetition flashcard app for code-heavy cards. Single user.
Go backend (`server/`) + React/TypeScript frontend (`web/`), SQLite, FSRS.
Design spec: docs/specs/2026-11-02-flashcards-design.md. Read it before changing behaviour.

## Commands
- Checks (spec §9): `bash scripts/check.sh` runs every group; `bash scripts/check.sh backend frontend` runs some
  (groups: `backend frontend e2e scripts vm workflows`; which ones a change needs: Working rules below)
- Once per clone: `bash scripts/setup-hooks.sh` (pinned gitleaks + git hooks; check.sh fails without it). Needs the
  private list at `~/.config/anki-app/gitleaks-private.toml` (format: `scripts/gitleaks-private.example.toml`).
  Scan by hand: `bash scripts/leakscan.sh staged|stdin|range <log-opts>|history`.
- Update gitleaks: `bash scripts/bump-gitleaks.sh` (newest release at least 14 days old; a weekly CI job opens an
  "Update gitleaks to X" issue), then commit on a `feat/` branch and open a PR, whose CI exercises the new binary.
  Commits, commit messages and pushes (pre-push) are scanned. Never bypass the hooks. The Claude Code hook scans
  only Bash and PowerShell commands that send text out (`gh`, `git commit`/`tag`/`notes`, the GitHub API, found
  anywhere in the command, so `bash -c` and other wrappers count) and blocks the usual bypass commands. It also blocks
  a body file it can't read yet: write the file first, then run the command on its own with a literal path. Files you
  write are caught by the pre-commit hook. Accepted old-history hits go in `.gitleaksignore` (emptied at cutover).
- Build the binary (web app embedded): `bash scripts/build.sh`, then `server/anki-app serve`
- Deploy: merge to `main` and push. CI builds the image, and the VM deploys it about a minute later
  (spec docs/specs/2026-11-05-server-deploy-design.md; runbook docs/runbooks/vm.md). The live DB and settings are
  on the VM (`/var/lib/anki-app/data`, `/etc/anki-app`), not the repo.
- Operations, on the VM as root (`ssh root@anki`):

  | Task | How |
  |---|---|
  | Status | `systemctl status anki-app`, `cat /var/lib/anki-app/deploy/image.env` |
  | Stop the app (kill switch) | `systemctl stop anki-app` (a new image or reboot restarts it: also pause, `systemctl disable anki-app`) |
  | Pause / resume deploys | `touch /etc/anki-app/paused` / `rm /etc/anki-app/paused` |
  | Deploy a specific build | pause, then `systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-update ghcr.io/marccowan/anki-app:sha-<12>` |
  | Restore a backup | `systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-restore --yes <file>` |
  | Off-site copy now | `systemctl start anki-offsite.service` |
  | Logs | `journalctl -u anki-app` / `-u anki-update` / `-u anki-offsite` |
  | Turn off the Claude connector | `tailscale funnel --https=443 off` (Settings → Disconnect Claude revokes its tokens) |

- URLs: web UI `https://anki.<tailnet>.ts.net:8443` (tailnet only). Claude connector
  `https://anki.<tailnet>.ts.net/mcp` (public via Funnel on 443, the only port Claude connects to). Never map a
  web UI path on 443: everything on that port is public.

- Docker image check (needs Docker): `bash scripts/docker-smoke.sh`.
- Dependabot (`.github/dependabot.yml`) opens grouped weekly PRs (Mondays, minor/patch per ecosystem, majors alone) for Go, npm, Actions and Docker base images. The pinned gitleaks in `scripts/setup-hooks.sh` is not covered; it is handled separately.
- Ideas not yet scheduled are bookmarked in `docs/ideas/YYYY-MM-DD-<name>.md`.
- Outages and data scares get a blameless write-up in `docs/incidents/` (format in its README).
- Backend tests: `cd server && go test ./...`
- Frontend tests: `cd web && npm test`
- Dev: `cd server && go run ./cmd/anki-app serve` + `cd web && npm run dev` (Vite proxies `/api` to :8080)

## Non-negotiable security rules
- Never write real infrastructure identifiers (tailnet name, `*.ts.net` hosts, Tailscale IPs, GCP project ID,
  bucket names, machine names, local user paths) into files, commit messages, PR or issue text. Redact command
  output before quoting it. Use placeholders: `<tailnet>`, `<PROJECT_ID>`, `<BUCKET>`, `100.x.y.z`.
- Never change the default listen address away from 127.0.0.1. Never weaken the Host allowlist, the
  Sec-Fetch-Site/Origin check, the JSON content-type requirement, or the CSP to make something work. Fix the cause.
- SQL: `?` parameters only. Never build SQL from request data. Identifiers that vary come from fixed lists in code.
- Card content is rendered ONLY through `<Markdown>` (markdown-it html:false + DOMPurify).
  `dangerouslySetInnerHTML` is forbidden everywhere else.
- Validate every input at the API boundary (types, ranges, the 6,000 code point limit per side).
- Never log card content, passphrases, or session tokens. No secrets, .env, or .db files in git.
- New dependencies need a one-line justification in the commit message, exact pinned versions, and committed lockfiles.
- Claude connector: two written exceptions, scoped to the public listener (`api.NewPublic`, `MCP_ADDR`) and
  nothing else (spec docs/specs/2026-11-08-mcp-connector-design.md):
  - On `/mcp`, `/oauth/register`, `/oauth/token` and `/oauth/revoke`, **any** `Origin` or `Sec-Fetch-Site` header gets
    a 403. This replaces the same-origin rule there and is stricter: only Claude's servers call these routes, they
    use no cookies, and browsers always send one of those headers. The main handler's rule is unchanged.
  - The consent page (`/oauth/authorize`) has its own CSP: `default-src 'none'; style-src 'self'; form-action 'self'
    <origin>; frame-ancestors 'none'; base-uri 'none'`, where `<origin>` comes only from a redirect URI that matched
    the fixed allowlist in `oauth.AllowedRedirectURIs`. The SPA's CSP is unchanged.
  - Tools stay read or add only. Never add an MCP tool that edits or deletes.

## Working rules
- TDD where it makes sense: test-first by default on the backend; on the frontend for logic (renderer,
  API client, study flow). Layout is checked with Playwright at iPhone and desktop sizes.
- Before saying anything is done: run the check groups your change needs and show the summary. If something fails,
  say so. Run it as a background task: with more than one group, the groups run side by side and the summary lists
  each group's result plus the end of each failing log. Changes to `scripts/check.sh` or `.github/workflows/` run
  every group (`bash scripts/check.sh`). Pure Markdown/docs changes and inert config (`.gitignore`,
  `.gitattributes`, `.gitleaksignore`, `dependabot.yml`) need no checks; the PR says "docs only, checks not run".
  Paths outside this table: run every group. CI picks groups the same way (`changes` job in
  `.github/workflows/main.yml`) and runs every group on `main`.

  | Changed | Groups: `bash scripts/check.sh <group> ...` |
  |---|---|
  | `server/**` | `backend` |
  | `server/cmd/anki-app/`, `server/internal/{api,config,webui}/` | `backend e2e` |
  | `web/**` | `frontend e2e` |
  | `scripts/`, `.githooks/`, `.claude/hooks/`, `.claude/settings.json`, `.gitleaks.toml` | `scripts` |
  | `deploy/vm/**` | `vm` |
  | `.github/workflows/**` | `workflows` (and every group, see above) |
  | `Dockerfile`, `.dockerignore`, `scripts/docker-smoke.sh` | `bash scripts/docker-smoke.sh` if Docker runs; CI runs it |

- Subagent briefs: say which groups to run (from the table above), that docs-only changes run none, and not to
  dispatch `e2e-tester` unless the change touches the `e2e` paths.
- **Plans and ideas get a real review before work starts.** Docs in `docs/plans/` and `docs/ideas/` have a status
  line under the title: `Status: **draft**` (not yet reviewed), then `**reviewed** <date>`, then `done` or
  `superseded by …`. New docs start as draft, and merging the PR that adds one doesn't change that. Before
  implementing a draft, go through it with Marc: what it changes and why, the trade-offs, anything risky. Then ask
  Marc to confirm the key decisions one by one. The status changes to reviewed only after that.
- Migrations: never edit an applied migration; add a new numbered one.
- Indexes: every new query gets an `EXPLAIN QUERY PLAN` check in a store test. Add an index only when a real query
  needs it, and write down which query in the migration.
- Keep units small with one clear job. If a file grows past ~300 lines, consider splitting it.
- UI changes (a page, layout or breakpoint): dispatch the `e2e-tester` agent (Sonnet, `.claude/agents/e2e-tester.md`)
  with what changed, which pages and widths, and whether to write, update or only run tests. It edits only
  `web/e2e/` and reports suspected app bugs; fix those in the main session, then re-dispatch. Implementer subagents
  cannot start it, so the main session dispatches it after each UI task. Commit its test changes with the feature.
- Out of scope unless Marc asks: accounts, offline mode, search in the web UI (the MCP `search_cards` tool is for
  Claude only), media, moving cards between decks.
- Work on `feat/<name>` branches; merge to main only when `all-checks` passes on the PR.
