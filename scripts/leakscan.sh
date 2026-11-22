#!/usr/bin/env bash
# Leak scan: gitleaks run twice, with the repo's .gitleaks.toml and with the private list outside the repo.
#   leakscan.sh staged               staged changes (pre-commit)
#   leakscan.sh stdin                text on stdin (commit messages, Claude Code hook)
#   leakscan.sh range <log-opts...>  commits, e.g. origin/main..HEAD
#   leakscan.sh history              all commits
# Exit: 0 clean, 1 leak found, 2 setup missing or scan error (fail closed).
# Env: GITLEAKS_BIN (binary, default .tools/gitleaks/), ANKI_GITLEAKS_PRIVATE (private config path, default
# ~/.config/anki-app/gitleaks-private.toml; "none" skips that pass, honoured only when GITHUB_ACTIONS=true, for CI on
# fork PRs without the secret). Found values are never printed (--redact), and the private pass prints nothing of
# gitleaks' own output. In range and history modes, merge diffs (-m) and commit messages are scanned too.
# Known old hits in pre-public history are suppressed by fingerprint in .gitleaksignore (empty at cutover).
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
mode="${1:-}"
[ $# -gt 0 ] && shift

usage() { echo "usage: leakscan.sh staged | stdin | range <git-log-opts> | history" >&2; exit 2; }
case "$mode" in
  staged | stdin | history) [ $# -eq 0 ] || usage ;;
  range) [ $# -ge 1 ] || usage ;;
  *) usage ;;
esac

bin="${GITLEAKS_BIN:-}"
if [ -z "$bin" ]; then
  for c in "$root/.tools/gitleaks/gitleaks" "$root/.tools/gitleaks/gitleaks.exe"; do
    [ -x "$c" ] && bin="$c" && break
  done
fi
if [ -z "$bin" ] || [ ! -x "$bin" ]; then
  echo "leakscan: gitleaks not found. Fix: bash scripts/setup-hooks.sh" >&2
  exit 2
fi

private="${ANKI_GITLEAKS_PRIVATE:-$HOME/.config/anki-app/gitleaks-private.toml}"
if [ "$private" = "none" ]; then
  if [ "${GITHUB_ACTIONS:-}" != "true" ]; then
    echo "leakscan: ANKI_GITLEAKS_PRIVATE=none is only allowed in GitHub Actions. Fix: point it at your private config (bash scripts/setup-hooks.sh)" >&2
    exit 2
  fi
  echo "leakscan: WARNING: private pass skipped (ANKI_GITLEAKS_PRIVATE=none). Only for CI on fork PRs." >&2
elif [ ! -f "$private" ]; then
  echo "leakscan: private config missing at $private. Fix: copy scripts/gitleaks-private.example.toml there and put your real names in it (see bash scripts/setup-hooks.sh)" >&2
  exit 2
else
  # An empty or still-fake file would pass everything silently.
  if ! grep -q '^[[:space:]]*\[\[rules\]\]' "$private"; then
    echo "leakscan: private config has no [[rules]]. Fix: copy scripts/gitleaks-private.example.toml and put your real names in it" >&2
    exit 2
  fi
  if grep -qiE 'example-tailnet|my-project-123456|example-backup-bucket|fake-laptop' "$private"; then
    echo "leakscan: private config still has the example's fake values. Fix: replace them with your real names" >&2
    exit 2
  fi
fi

input=""
if [ "$mode" = stdin ]; then
  input="$(mktemp)"
  trap 'rm -f "$input"' EXIT
  cat > "$input"
fi

# 3 = leak found (set via --exit-code), 0 = clean, anything else = the scan itself failed.
# The private pass must never show gitleaks' output (a broken regex can echo values), so it prints one line only.
quiet=(--log-level error --no-color)
ignore=(--gitleaks-ignore-path "$root/.gitleaksignore")

# gl <label> <config> <kind: diff|msg> : one gitleaks run
gl() {
  local config="$2" kind="$3"
  shift 3
  local common=(--config "$config" --redact --no-banner --exit-code 3 "${quiet[@]}" "${ignore[@]}")
  cd "$root" || return 2
  local run
  if [ "$kind" = msg ]; then
    run=("$bin" stdin "${common[@]}")
  else
    case "$mode" in
      staged)  run=("$bin" git "${common[@]}" --pre-commit --staged .) ;;
      stdin)   run=("$bin" stdin "${common[@]}") ;;
      range)   run=("$bin" git "${common[@]}" --log-opts "-m $*" .) ;;
      history) run=("$bin" git "${common[@]}" --log-opts "-m --all" .) ;;
    esac
  fi
  if [ "$kind" = msg ]; then
    case "$mode" in
      range)   git log --format=%B "${logargs[@]}" 2>/dev/null | "${run[@]}" ;;
      history) git log --format=%B --all 2>/dev/null | "${run[@]}" ;;
    esac
  elif [ "$mode" = stdin ]; then
    "${run[@]}" < "$input"
  else
    "${run[@]}"
  fi
  return $?
}

run_pass() {
  local label="$1" config="$2" rc kind worst=0
  shift 2
  local kinds=(diff)
  case "$mode" in range | history) kinds=(diff msg) ;; esac
  for kind in "${kinds[@]}"; do
    if [ "$label" = private ]; then
      gl "$label" "$config" "$kind" "$@" >/dev/null 2>&1
    else
      gl "$label" "$config" "$kind" "$@"
    fi
    rc=$?
    case "$rc" in
      0) ;;
      3) echo "leakscan: LEAK found by the $label rules in $kind (values redacted)" >&2; [ "$worst" -lt 1 ] && worst=1 ;;
      *) echo "leakscan: gitleaks failed (exit $rc) in the $label pass ($kind)" >&2; worst=2 ;;
    esac
  done
  return "$worst"
}

logargs=()
[ "$mode" = range ] && read -r -a logargs <<< "$*"

status=0
run_pass repo "$root/.gitleaks.toml" "$@"
rc=$?
[ "$rc" -gt "$status" ] && status=$rc
if [ "$private" != "none" ]; then
  run_pass private "$private" "$@"
  rc=$?
  [ "$rc" -gt "$status" ] && status=$rc
fi
exit "$status"
