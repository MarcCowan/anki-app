# Public repo: guardrails, then a squashed cutover

Status: done 2026-11-22 (reviewed 2026-11-22)

We're moving development to a public GitHub repo. The current private repo keeps the old history (renamed and
archived), and the public one starts from a single squashed commit of `main`.

The guardrails come first, so they're already part of that first public commit. The new repo starts private, gets
its settings and a green CI run, and only then turns public, with the public-only protections added straight
after. Order matters because a leak in a public PR stays in `refs/pull/*` for good: local checks are the real
defence, and CI is the backstop.

## What we're protecting

Real secrets (passphrase, tokens, `.env`, `.db`) already stay out of the repo by design. The gap is
**infrastructure identifiers**: the tailnet name, Tailscale IPs, the GCP project ID, the bucket name, machine names
and local user paths. They tend to get in when real command output is pasted into a doc. That's how the old tailnet
name reached the runbook. The same can happen in commit messages, PR descriptions and comments, which no git hook
sees.

## Guardrails

### 1. Gitleaks, with generic rules in the repo and specific ones outside it

- `.gitleaks.toml` extends gitleaks' default rules (real secrets) with **generic** rules that need no real values:
  - any `*.ts.net` hostname that isn't one of our placeholders (`<tailnet>`, `tailXXXX`, `anki.x.ts.net`, …), which
    also covers any future tailnet rename;
  - Tailscale addresses in `100.64.0.0/10`, apart from the `100.x.y.z` placeholder and MagicDNS's
    `100.100.100.100`;
  - real user home paths (`C:\Users\…`, `/home/…`, `/Users/…`).
- The **private** list of actual identifiers lives outside the repo, at `~/.config/anki-app/gitleaks-private.toml`
  (format in `scripts/gitleaks-private.example.toml`). Committing it would publish exactly what it protects.
- `scripts/leakscan.sh` is the one entry point for every layer. It runs gitleaks twice, once per config, and always
  with `--redact`, so a hit never prints the value. Modes: `staged`, `stdin`, `range <log-opts>`, `history`.
- Gitleaks is pinned to one exact version in `scripts/gitleaks.version`, with SHA-256 hashes from the release's
  checksums file. `scripts/setup-hooks.sh` downloads and verifies it into `.tools/` (git-ignored).

### 2. Git hooks (`.githooks/`, turned on by `scripts/setup-hooks.sh`)

- `pre-commit` scans the staged changes, and `commit-msg` scans the message.
- Both **fail closed**: with no gitleaks binary or no private list, the commit is refused, with a hint on how to fix
  it.
- `scripts/check.sh` fails if `core.hooksPath` isn't `.githooks` (skipped in CI), so the done bar shows the hooks
  are on.

### 3. Claude Code hook (`.claude/settings.json`)

A `PreToolUse` hook on Bash and PowerShell passes `gh` commands (PR, issue and comment text, including `--body-file`
contents), `git commit`/`tag`/`notes` and `curl` to the GitHub API through `leakscan.sh stdin`, and blocks on a hit.
It finds these commands anywhere in the command line, so `bash -c` and other wrappers count. It also blocks a body
file it can't read yet (missing, created earlier in the same command, named by a variable, a folder, over 5 MB, or
piped stdin). The fix is to write the file first, then run the command on its own with a literal path. It also blocks
the ways around the git hooks (`--no-verify`, `core.hooksPath` overrides, scanner env overrides). It's the only layer
that sees PR and issue text. File writes aren't scanned here: a leak in a file is harmless until it's committed, and
the pre-commit hook catches it then. Other shell commands go straight through, so `setup-hooks.sh` still runs on a
fresh machine.

### 4. CI (`.github/workflows/main.yml`)

- A new `secrets` job runs the same pinned binary and wrapper:
  - a PR scans its own commits;
  - a push to `main` scans the pushed range;
  - a weekly scheduled run scans the full history.
- The private list comes from the Actions secret `GITLEAKS_PRIVATE_RULES`. It's written to a temp file and never
  echoed. When the secret is absent (fork PRs), the job warns and runs the generic rules only.
- `check` also runs on PRs. The `image` job's condition also checks the event type, so a scheduled run or a PR can
  never push `:main`, the tag the VM auto-deploys.

### 5. CLAUDE.md rule

Never write real identifiers into files, commit messages, PR or issue text, and redact command output before quoting
it (added with this plan).

## Cutover

1. The guardrails are merged, `bash scripts/check.sh` passes, and the private list is filled in on the laptop.
2. **Scan the private repo's full history** with both configs. Every hit is either fixed on `main` or confirmed to
   exist only in old history, which the squash leaves behind.
3. **Build the candidate:**
   - `git archive main` into a fresh repo;
   - add a README line saying earlier history is in a private repo (`LICENSE.md`, PolyForm Strict 1.0.0, is
     already in `main`: source visible, personal and non-commercial use only, no changes or redistribution);
   - empty `.gitleaksignore`: its fingerprints only point at old history;
   - check the Dockerfile's `org.opencontainers.image.source` points at the public repo (it already names
     `anki-app`);
   - make one "Initial public release" commit with the noreply author.
   A full-history scan of the candidate must come back with 0 hits. Built on Windows, the extracted files lose their
   executable bit, so the new repo records every script as `100644` and the VM-script tests fail on CI with
   "Permission denied". Restore the modes from `main` before committing (`git update-index --chmod=+x` for each
   `100755` entry of `git ls-tree -r main`), and check that `git ls-tree -r` of the candidate matches `main` apart
   from the README and `.gitleaksignore`.
4. **GitHub, still private:** rename the private repo to `anki-app-archive`, then create a new **private** `anki-app`.
5. **Settings that work on a private repo**, before anything is pushed:
   - the `GITLEAKS_PRIVATE_RULES` Actions secret;
   - **GHCR**, in the `anki-app` package settings: it stays **Private**, the new repo gets Actions **Admin** access
     and the archive loses it. The image name and the VM's pull token don't change. Write is enough to push, but the
     `image` job also prunes old versions, and deleting package versions from a workflow needs Admin (without it
     GitHub answers "Package not found"). The old repo had Admin implicitly, because it created the package.
6. **Push the candidate** to the private repo. CI on `main` must go green (`check`, `secrets`, `image`), and the VM
   picks up the new `:main` (`journalctl -u anki-update`). It's the same tree that's live now, so nothing visible
   changes.
7. **Flip to public.** First scan everything else the flip publishes, with both configs: every ref (including
   Dependabot's `refs/pull/*`, which appear within minutes of the first push), PR titles, bodies and comments, and
   the logs of every Actions run so far (`gh run view <id> --log`). Then, straight away and with nothing pushed in
   between (these need a public repo on a free account):
   - a ruleset on `main`: PRs required with 0 approvals (a sole maintainer can't approve their own PR), `check`
     and `secrets` required, no force-push or deletion, **no bypass list**. Docs-only PRs wait for CI too;
   - secret scanning and push protection on;
   - approval required for workflows from outside contributors.
8. **Archive the old repo and re-point local clones.** Run `git remote set-url origin`, then `git fetch` and
   `git reset --hard origin/main` on a clean `main`, since the two histories don't share commits. Open Dependabot PRs
   stay behind in the archive; Dependabot opens them again in the new repo.
9. **Shake-down:** a small PR through the ruleset and the full pipeline, then watch the VM pick up the new `:main`.

**If something leaks after publishing:** make the repo private first, then fix it. A leak inside a PR needs GitHub
Support to purge the refs. Most of these identifiers are low-impact on their own (the tailnet name is in public
certificate logs anyway), but every hit gets treated as real.

## Residual risk

- Regexes miss things phrased in new ways. The CLAUDE.md rule and review cover that.
- Images aren't scanned, so screenshots with real hostnames stay out of the repo.
- The private list only knows what it names. It needs updating whenever the infrastructure changes (a new tailnet
  name, a new bucket).
