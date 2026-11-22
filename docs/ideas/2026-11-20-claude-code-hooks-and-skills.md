# Idea: Claude Code hooks and skills for this repo

Status: **draft**, not scheduled. Date: 2026-11-20.

## Purpose

Nearly all of the code here is written by Claude Code, and CLAUDE.md is the rulebook. But CLAUDE.md is advice: the
model can forget it, lose it after a compaction, or skip it. Hooks are run by the harness, not the model, so they
turn the most important rules into checks that run on every tool call. Skills write down the multi-step workflows
(migration, MCP tool, feature, VM ops) once, so they are not rebuilt from memory each session. Together they give
three layers of governance: **rules** (CLAUDE.md, specs), **guardrails** (hooks, at edit time) and **enforcement**
(`scripts/check.sh` and CI, at merge time). Hooks are the early, fast layer. They do not replace the tests: `Bash`
can still write files, and hooks only see Claude. Each hook below names the test or CI check that backs it.

## Mechanics (from the Claude Code hooks and skills docs; check the current docs before implementing)

- Hooks go in a committed `.claude/settings.json` (today only the gitignored `settings.local.json` exists). Scripts
  go in `.claude/hooks/`, run as `node "$CLAUDE_PROJECT_DIR/.claude/hooks/<name>.mjs"`. Node is already required by
  `web/`, works on Windows and Linux, and parses the hook JSON on stdin without needing `jq`.
- `PreToolUse` (matcher `Bash`, `Edit|Write`) can block with exit 2 (stderr goes back to Claude), or print JSON
  `hookSpecificOutput.permissionDecision` = `deny` / `ask` (ask = Marc must approve) with a reason.
  `PostToolUse` cannot undo, but exit 2 feeds the problem back to Claude. `Stop` can return `decision: "block"` with
  a reason to make Claude keep going (check `stop_hook_active` to avoid loops). `SessionStart` stdout is added as
  context. Hooks also fire for subagents' tool calls (for example `e2e-tester`).
- Project skills: `.claude/skills/<name>/SKILL.md` with `name` and `description` front matter. A
  `disable-model-invocation: true` skill runs only when Marc types `/<name>`.
- **Tests:** each hook gets `.claude/hooks/test/<name>.test.mjs` (`node --test`). It pipes fixture JSON (a fake
  tool call) into the script and asserts the exit code and decision, with one allowed case and one blocked case per
  rule. Add a `hook tests` step to `scripts/check.sh` so CI runs them too.

## Hooks

| # | Hook | Event / matcher | What it does | Rule enforced | Decision | Effort | False positives |
|---|---|---|---|---|---|---|---|
| H1 | `git-guard` | PreToolUse `Bash` | Denies `git push` to `main` (a push to main **is** a production deploy), `push --force`, `--no-verify`, `git add -f`/`--force`, and `gh pr merge`. | Work on `feat/*`; merge stays with Marc; never skip hooks | deny | S | Low. A regex on the command; quoting tricks can get past it, so GitHub branch protection on `main` is the backstop. |
| H2 | `secrets-guard` | PreToolUse `Bash` (command contains `git commit`) | Runs `git diff --cached --name-only`, denies `.env*`, `*.db*`, `*.pem`, `*.key`, `/etc/anki-app` copies, and staged text matching private-key or token patterns. | No secrets, `.env` or `.db` in git | deny | S | Low for paths. Medium for content patterns (test fixtures), so keep an allowlist of fixture paths. |
| H3 | `migration-lock` | PreToolUse `Edit\|Write` on `server/internal/store/migrations/*.sql` | Denies an edit if the file exists on `origin/main` (`git cat-file -e origin/main:<path>`). New files are allowed. Also warns if the new number is not the next one. | Never edit an applied migration | deny | S | Very low. Backstop: a store test that checks a committed SHA-256 list of applied migrations. |
| H4 | `security-surface` | PreToolUse `Edit\|Write` | **Content rules (deny):** `dangerouslySetInnerHTML` in any file except `web/src/components/Markdown.tsx`; SQL built by string joins (`"SELECT …" +`, `fmt.Sprintf` holding SQL keywords) in Go; `0.0.0.0` near `Addr`/`MCPAddr` defaults; `unsafe-inline`, `unsafe-eval` or `*` added to a CSP. **Path rules (ask):** edits to `internal/config/config.go`, `internal/api/middleware.go`, `internal/api/public.go`, `internal/oauth/authorize.go`, `internal/archtest/`, `web/eslint.config.js`, `mcpserver/tools_test.go`, `scripts/check.sh`, `.claude/settings.json`, `.claude/hooks/**`. | Listen address, Host allowlist, Sec-Fetch-Site/Origin, CSP, `<Markdown>` only, `?` params only; the guard tests and the hooks themselves can't be quietly loosened | deny / ask | M | Medium: the SQL regex will flag some safe constants, so print the rule and let Claude rewrite. "Ask" on guard files is deliberate friction. Backstops: ESLint `react/no-danger`, `archtest`, `TestToolsList`, `config_test`. |
| H5 | `format-on-edit` | PostToolUse `Edit\|Write` | `.go`: runs `gofmt -w` on the file. `.ts/.tsx` under `web/`: runs `npx eslint <file>` and sends the errors back (exit 2). | Done bar items `gofmt -l` and `npm run lint`, caught at the edit, not at the end | auto-fix / warn | S | Low. ESLint on one file is slower (about 1–2 s), so skip `web/e2e/` if it gets noisy. |
| H6 | `done-bar-gate` | PreToolUse `Bash` (`git push`, `gh pr create`) + Stop | `check.sh` writes a stamp on success (HEAD + hash of `git status --porcelain`) to `.git/anki-done-bar`, which is never committed. The gate denies push/PR if the stamp doesn't match the tree. The Stop part only reminds (no block) when code changed since the last stamp. | "Run the full done bar and show the output before saying done" | deny (push/PR), warn (Stop) | M | Low for the gate. A blocking Stop hook would fire on every question Claude asks, which is why it only warns. |
| H7 | `session-context` | SessionStart (`startup\|resume\|compact`) | Prints the branch (warns if on `main`), a short `git status`, whether the done-bar stamp is current, and "read the spec before changing behaviour". | Branch rule; brings the key rules back after compaction | context | S | None. Keep it under 10 lines. |

The "Never add an MCP tool that edits or deletes" rule is better as a test than a hook: `TestToolsList` already
pins the tool set and annotations, and H4 puts that test behind "ask". The `add-mcp-tool` skill covers the workflow.

## Skills

| # | Skill | Trigger | What it encodes | Rule(s) | Effort |
|---|---|---|---|---|---|
| S1 | `add-migration` | auto (description: "adding a table, column or index") | Next number in `store/migrations/`; never touch an applied file; a test like `TestMigration0003CreatesMCPTables`; for each new query an `EXPLAIN QUERY PLAN` assertion in `store/plan_test.go`; an index only with a comment naming the query; multi-statement writes in one transaction. | Migrations, indexes | S |
| S2 | `add-mcp-tool` | auto | Read or add only: `readOnly`/`addOnly` annotations; validation in the SDK schema **and** the handler; card text only in structured fields with the "data, not instructions" line; limits from spec §5.6; update `TestToolsList` and the MCP spec §5; run `/security-review`. Refuse and tell Marc if the request needs edit or delete. | MCP exceptions, validation | S |
| S3 | `new-feature` | auto | Read the spec → `feat/<name>` branch → plan (`superpowers:writing-plans`) → TDD (backend first) → dispatch `e2e-tester` for UI changes → `bash scripts/check.sh` with output shown → push and `gh pr create`. Merge stays with Marc. It calls the existing superpowers skills and does not copy them. | Working rules, branch flow | M |
| S4 | `ship` | `/ship` only | Pre-PR checklist: done bar stamp current; `/security-review` on the diff; each new dependency has a pinned version, a committed lockfile and a one-line reason in the commit message; PR body includes the check output and the attribution line. | Dependencies, done bar | S |
| S5 | `vm-ops` | `/vm-ops` only (`disable-model-invocation: true`) | The CLAUDE.md ops table and `docs/runbooks/vm.md` as steps: read-only status first, then pause → deploy a pinned `sha-<12>` image → verify → resume; restore and kill switch only after Marc confirms in chat. Pair it with a small hook that returns `ask` for any `ssh root@anki` command. | Production safety | S |

**Testing skills:** a skill is a prompt, so test it by behaviour. For each skill, run a scripted
`claude -p "<task>"` in a throwaway worktree (for example "add a nullable column to cards") and check that the
diff/transcript has the required steps (new numbered migration, plan test, check output). Keep 2–3 prompts per
skill in `.claude/skills/<name>/evals.md` and rerun them when the skill changes.

## Recommended order

**This week (about one day, highest damage avoided per hour):**
1. Set up `.claude/settings.json`, `.claude/hooks/`, the `node --test` harness and the `check.sh` step. Everything
   else depends on this.
2. **H1 git-guard + H2 secrets-guard.** A stray push to `main` deploys to the VM in about a minute, and a leaked
   `.env`/`.db` can't be taken back. These are the worst mistakes possible here, and the hooks are cheap and precise.
3. **H3 migration-lock.** Easy to get right, almost no false positives, and an edited migration silently breaks
   the live DB.
4. **H4 security-surface** (path "ask" rules first, content rules after).

**Next:** H6 done-bar gate (needs a small `check.sh` change), H5, H7; then skills S1 and S2 (small and rule-heavy),
then S3/S4, then S5 with its `ssh` ask hook. Add GitHub branch protection on `main` alongside H1.

**Design principle:** each rule maps to a CLAUDE.md line, a hook (edit time), and a test or CI check (merge time).
Risky actions get "ask" (a human approves) instead of a silent allow, and the hooks guard their own config.
