#!/usr/bin/env bash
# Runs the project's checks (spec §9), by group. Which groups a change needs: the table in CLAUDE.md
# (CI decides the same way, from the path filters in .github/workflows/main.yml).
#
#   bash scripts/check.sh                   every group (same as --all)
#   bash scripts/check.sh backend frontend  only these groups
#   bash scripts/check.sh --serial ...      one group at a time, output streamed (the default in CI)
#
# With more than one group, the groups run side by side: each group's output goes to a log file, a line is printed
# as each group finishes, and a summary at the end shows the tail of every failing log. Exits non-zero if any
# group failed.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
all_groups=(backend frontend e2e scripts vm workflows)

usage() {
  echo "usage: bash scripts/check.sh [--all] [--serial] [group ...]"
  echo "groups: ${all_groups[*]}"
}

step() { printf '\n==== %s ====\n' "$*"; }

# ---- groups (each runs in its own subshell with set -euo pipefail) ----

# Runs shellcheck wherever it is installed; CI must have it. (A comment that starts with the word "shellcheck" is
# read as a shellcheck directive.)
run_shellcheck() {
  if command -v shellcheck >/dev/null 2>&1; then
    shellcheck -x -P SCRIPTDIR "$@"
  elif [ -n "${CI:-}" ]; then
    echo "shellcheck not installed, but CI is set: it must run in CI"
    return 1
  else
    echo "shellcheck not installed: skipped"
  fi
}

group_backend() {
  cd "$root/server"
  step "gofmt -l (must be empty)"
  local unformatted
  unformatted="$(gofmt -l .)"
  if [ -n "$unformatted" ]; then
    echo "$unformatted"
    return 1
  fi
  step "go vet ./..."
  go vet ./...
  step "staticcheck ./..."
  go tool staticcheck ./...
  step "go test ./..."
  go test ./...
  step "govulncheck ./..."
  go tool govulncheck ./...
}

group_frontend() {
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
}

group_e2e() {
  cd "$root/web"
  step "npm run e2e"
  npm run e2e
}

group_scripts() {
  step "shellcheck (scripts, git hooks, Claude Code hooks)"
  run_shellcheck "$root"/scripts/*.sh "$root"/.githooks/* "$root"/.claude/hooks/*.sh
  step "leakscan tests"
  bash "$root/scripts/test-leakscan.sh"
  step "leakscan Claude Code hook tests"
  bash "$root/.claude/hooks/test-leakscan-hook.sh"
  step "bump-gitleaks tests"
  bash "$root/scripts/test-bump-gitleaks.sh"
}

group_vm() {
  step "shellcheck (deploy/vm)"
  local f files=()
  for f in "$root"/deploy/vm/* "$root"/deploy/vm/test/* "$root"/deploy/vm/test/stubs/*; do
    if [ -f "$f" ] && head -n 1 "$f" | grep -Eq '^#!.*bash'; then
      files+=("$f")
    fi
  done
  run_shellcheck "${files[@]}"
  step "vm script tests"
  local vm_tests="$root/deploy/vm/test/run.sh"
  if bash "$vm_tests" --jq-available; then
    bash "$vm_tests"
  elif [ -n "${CI:-}" ]; then
    echo "jq not installed, but CI is set: the vm script tests must run in CI"
    return 1
  else
    echo "vm script tests: jq not installed: skipped"
  fi
}

group_workflows() {
  step "actionlint"
  go -C "$root/server" tool actionlint "$root"/.github/workflows/*.yml
}

# ---- arguments ----

serial=""
[ -n "${CI:-}" ] && serial=1
wanted=()
for arg in "$@"; do
  case "$arg" in
    --all) wanted=("${all_groups[@]}") ;;
    --serial) serial=1 ;;
    -h|--help) usage; exit 0 ;;
    *)
      known=""
      for g in "${all_groups[@]}"; do [ "$g" = "$arg" ] && known=1; done
      if [ -z "$known" ]; then
        echo "unknown group: $arg"
        usage
        exit 2
      fi
      wanted+=("$arg")
      ;;
  esac
done
[ ${#wanted[@]} -eq 0 ] && wanted=("${all_groups[@]}")

# Keep the fixed order and drop duplicates.
selected=() skipped=()
for g in "${all_groups[@]}"; do
  hit=""
  for w in "${wanted[@]}"; do [ "$w" = "$g" ] && hit=1; done
  if [ -n "$hit" ]; then selected+=("$g"); else skipped+=("$g"); fi
done

# ---- preflight ----

step "git hooks enabled (core.hooksPath)"
if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  echo "skipped (GitHub Actions)"
elif [ "$(git -C "$root" config core.hooksPath || true)" != ".githooks" ]; then
  echo "core.hooksPath is not .githooks. Fix: bash scripts/setup-hooks.sh"
  exit 1
else
  echo "ok"
fi

echo "running: ${selected[*]}"
[ ${#skipped[@]} -gt 0 ] && echo "not run: ${skipped[*]}"

results="$(mktemp -d "${TMPDIR:-/tmp}/anki-check.XXXXXX")"

# Runs one group and writes "<exit code> <seconds>" to $results/<group>.status. The group runs in a background
# subshell so that set -e stays in force inside it (a subshell in an if or || context would turn it off).
run_group() {
  local g="$1" log="${2:-}" start=$SECONDS rc=0
  if [ -n "$log" ]; then
    ( set -euo pipefail; "group_$g" ) >"$log" 2>&1 &
  else
    ( set -euo pipefail; "group_$g" ) &
  fi
  wait "$!" || rc=$?
  echo "$rc $((SECONDS - start))" >"$results/$g.status"
  return 0
}

# A lane runs its groups one after another. backend, frontend and e2e share one lane: the Vite build replaces the
# directory the Go binary embeds, so it must not run while Go compiles.
lane_of() {
  case "$1" in
    backend|frontend|e2e) echo app ;;
    *) echo "$1" ;;
  esac
}

if [ -n "$serial" ] || [ ${#selected[@]} -eq 1 ]; then
  for g in "${selected[@]}"; do run_group "$g"; done
else
  echo "logs: $results"
  lanes=()
  for g in "${selected[@]}"; do
    l="$(lane_of "$g")"
    case " ${lanes[*]} " in *" $l "*) ;; *) lanes+=("$l") ;; esac
  done
  pids=()
  for l in "${lanes[@]}"; do
    (
      for g in "${selected[@]}"; do
        [ "$(lane_of "$g")" = "$l" ] || continue
        run_group "$g" "$results/$g.log"
        read -r rc secs <"$results/$g.status"
        if [ "$rc" -eq 0 ]; then echo "  done: $g PASS ${secs}s"; else echo "  done: $g FAIL ${secs}s"; fi
      done
    ) &
    pids+=("$!")
  done
  for p in "${pids[@]}"; do wait "$p" || true; done
fi

# ---- summary ----

step "summary"
failed=()
for g in "${selected[@]}"; do
  rc=1 secs=0
  [ -f "$results/$g.status" ] && read -r rc secs <"$results/$g.status"
  if [ "$rc" -eq 0 ]; then
    printf 'PASS  %-10s %4ss\n' "$g" "$secs"
  else
    failed+=("$g")
    printf 'FAIL  %-10s %4ss' "$g" "$secs"
    [ -f "$results/$g.log" ] && printf '  log: %s' "$results/$g.log"
    printf '\n'
  fi
done
for g in "${skipped[@]}"; do printf 'skip  %s\n' "$g"; done

for g in "${failed[@]}"; do
  if [ -f "$results/$g.log" ]; then
    printf '\n---- %s: last 40 lines ----\n' "$g"
    tail -n 40 "$results/$g.log"
  fi
done

if [ ${#failed[@]} -gt 0 ]; then
  printf '\nCHECKS FAILED: %s\n' "${failed[*]}"
  exit 1
fi
if [ ${#skipped[@]} -eq 0 ]; then
  printf '\nALL CHECKS PASSED\n'
else
  printf '\nCHECKS PASSED: %s\n' "${selected[*]}"
fi
