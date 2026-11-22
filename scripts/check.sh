#!/usr/bin/env bash
# The done bar (spec §9). Exits non-zero on the first failure.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"

step() { printf '\n==== %s ====\n' "$*"; }

step "git hooks enabled (core.hooksPath)"
if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  echo "skipped (GitHub Actions)"
elif [ "$(git -C "$root" config core.hooksPath || true)" != ".githooks" ]; then
  echo "core.hooksPath is not .githooks. Fix: bash scripts/setup-hooks.sh"
  exit 1
fi

cd "$root/server"
step "gofmt -l (must be empty)"
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "$unformatted"
  exit 1
fi
step "go vet ./..."
go vet ./...
step "staticcheck ./..."
go tool staticcheck ./...
step "go test ./..."
go test ./...
step "govulncheck ./..."
go tool govulncheck ./...

step "shellcheck"
if command -v shellcheck >/dev/null 2>&1; then
  shell_files=("$root"/scripts/*.sh "$root"/.githooks/* "$root"/.claude/hooks/*.sh)
  if [ -d "$root/deploy/vm" ]; then
    for f in "$root"/deploy/vm/* "$root"/deploy/vm/test/* "$root"/deploy/vm/test/stubs/*; do
      if [ -f "$f" ] && head -n 1 "$f" | grep -Eq '^#!.*bash'; then
        shell_files+=("$f")
      fi
    done
  fi
  shellcheck -x -P SCRIPTDIR "${shell_files[@]}"
elif [ -n "${CI:-}" ]; then
  echo "shellcheck not installed, but CI is set: it must run in CI"
  exit 1
else
  echo "shellcheck not installed: skipped"
fi

step "vm script tests"
vm_tests="$root/deploy/vm/test/run.sh"
if bash "$vm_tests" --jq-available; then
  bash "$vm_tests"
elif [ -n "${CI:-}" ]; then
  echo "jq not installed, but CI is set: the vm script tests must run in CI"
  exit 1
else
  echo "vm script tests: jq not installed: skipped"
fi

step "leakscan tests"
bash "$root/scripts/test-leakscan.sh"

step "leakscan Claude Code hook tests"
bash "$root/.claude/hooks/test-leakscan-hook.sh"

step "bump-gitleaks tests"
bash "$root/scripts/test-bump-gitleaks.sh"

cd "$root/web"
step "npm run lint"
npm run lint
step "npm run typecheck"
npm run typecheck
step "npm test"
npm test
step "npm run build"
npm run build
step "npm audit --omit=dev"
npm audit --omit=dev

if [ -f "$root/web/playwright.config.ts" ]; then
  step "npm run e2e"
  npm run e2e
else
  step "e2e skipped (no web/playwright.config.ts yet)"
fi

step "ALL CHECKS PASSED"
