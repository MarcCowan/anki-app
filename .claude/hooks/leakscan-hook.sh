#!/usr/bin/env bash
# PreToolUse hook (Bash and PowerShell): scan text Claude is about to put in a `gh` command, `git commit`/`tag`/`notes` or
# a curl to the GitHub API, with scripts/leakscan.sh. Files are not scanned here; the pre-commit hook covers them. Exit 0 allows; exit 2 blocks and stderr goes to Claude.
# Also blocks commands that would bypass the guardrails (--no-verify, core.hooksPath, ...).
# Never prints the scanned text or any match.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="${CLAUDE_PROJECT_DIR:-$(cd "$here/../.." && pwd)}"
scan="${ANKI_LEAKSCAN:-$root/scripts/leakscan.sh}"

if ! command -v node >/dev/null 2>&1; then
  echo "leakscan: node not found on PATH (needed to parse the hook payload). Install Node.js." >&2
  # Cheap raw check so unrelated shell commands (e.g. setup) still work.
  payload="$(cat)"
  if printf '%s' "$payload" | grep -Eq '"tool_name" *: *"(Bash|PowerShell)"' \
     && ! printf '%s' "$payload" | grep -Eq 'gh |git[^"]*(commit|tag|notes)|api\.github\.com|no-verify|hooksPath|GITLEAKS|ANKI_LEAKSCAN|SKIP='; then
    exit 0
  fi
  exit 2
fi

# First line of node's output is the verdict, the rest is the text, streamed to the scanner (not argv).
node "$here/extract.js" | {
  read -r verdict why
  case "$verdict" in
    SKIP) exit 0 ;;
    BLOCK)
      echo "leakscan: BLOCKED: $why. Fix the underlying problem instead of bypassing the leak guardrails." >&2
      exit 91 ;;
    UNREAD) exit 92 ;;
    SCAN) bash "$scan" stdin >/dev/null; exit $? ;;
    *) exit 90 ;;
  esac
}
rc=$?

case "$rc" in
  0) exit 0 ;;
  1)
    echo "leakscan: BLOCKED. The text you were about to send looks like it contains a real identifier or secret." >&2
    echo "Replace real values with placeholders (<tailnet>, <PROJECT_ID>, <BUCKET>, 100.x.y.z) and retry. Never echo or quote the value." >&2
    exit 2 ;;
  2)
    echo "leakscan: setup missing, so this commit or gh call is blocked (fail closed)." >&2
    echo "Fix: run 'bash scripts/setup-hooks.sh'; the private rules live in ~/.config/anki-app/gitleaks-private.toml." >&2
    exit 2 ;;
  91) exit 2 ;;
  92)
    echo "leakscan: BLOCKED: a file whose contents this command would send can't be read yet (missing, created or named" >&2
    echo "by a variable set earlier in the same command, a folder, over 5 MB, or piped stdin), so it can't be scanned." >&2
    echo "Write the file first, then run this command on its own with a literal path to it." >&2
    exit 2 ;;
  90)
    echo "leakscan: could not parse the hook payload; blocking (fail closed)." >&2
    exit 2 ;;
  *)
    echo "leakscan: scanner failed unexpectedly (exit $rc); blocking (fail closed)." >&2
    exit 2 ;;
esac
