#!/usr/bin/env bash
# Shared test helpers for the VM scripts, sourced by run.sh (never run on its own). Sets up PATH with
# the stubs in stubs/ (docker, systemctl, curl, flock, chown, gcloud), and provides assertions and the
# runner. Each test is a function run in a subshell with a fresh temp folder.
# shellcheck disable=SC2034 # variables are used by the test files

# Each test file sources this file too (so shellcheck can follow it); load it only once.
if [ -n "${HARNESS_LOADED:-}" ]; then
  return 0
fi
HARNESS_LOADED=1

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VM="$(dirname "$here")"
UPDATE="$VM/anki-update"
RESTORE="$VM/anki-restore"
OFFSITE="$VM/anki-offsite"

# Windows (Git Bash) only: jq installed by WinGet is a native exe that isn't on PATH and writes CRLF.
winget_jq() {
  local f
  for f in "${HOME:-}"/AppData/Local/Microsoft/WinGet/Packages/jqlang.jq_*/jq.exe; do
    if [ -f "$f" ]; then
      printf '%s\n' "$f"
      return 0
    fi
  done
  return 1
}

jq_needs_wrapper() {
  local v
  if ! command -v jq >/dev/null 2>&1; then return 0; fi
  v=$(jq --version)
  [[ $v == *$'\r' ]]
}

jq_available() {
  command -v jq >/dev/null 2>&1 || winget_jq >/dev/null
}

# setup_path: makes a temp root for this run, with the stubs and (if needed) a jq wrapper on PATH.
setup_path() {
  local jq_exe
  run_root=$(mktemp -d)
  mkdir "$run_root/bin"
  if jq_needs_wrapper; then
    if ! jq_exe=$(winget_jq); then
      echo "jq not installed" >&2
      exit 1
    fi
    # shellcheck disable=SC2016 # "$@" belongs to the wrapper
    printf '#!/usr/bin/env bash\nexec "%s" -b "$@"\n' "$jq_exe" >"$run_root/bin/jq"
    chmod +x "$run_root/bin/jq"
  fi
  export PATH="$here/stubs:$run_root/bin:$PATH"
}

REPO=ghcr.io/marccowan/anki-app
OLD="$REPO@sha256:old111"
PREV="$REPO@sha256:prev000"
NEW="$REPO@sha256:new222"

# ---- assertions ----------------------------------------------------------------------------------

fail() {
  echo "    FAIL: $*"
  echo "    --- log ---"
  sed 's/^/    | /' "$LOG"
  exit 1
}

# run_script <script> [args...]: runs it with stdin from $T/stdin (empty unless a test wrote it),
# stdout to $T/stdout, stderr appended to the log; sets RC.
run_script() {
  local script=$1
  shift
  [ -f "$T/stdin" ] || : >"$T/stdin"
  set +e
  "$script" "$@" <"$T/stdin" >"$T/stdout" 2>>"$LOG"
  RC=$?
  set -e
}

run_update() { run_script "$UPDATE" "$@"; }

expect_exit() { [ "$RC" = "$1" ] || fail "exit $RC, want $1"; }

expect_log() { grep -qF -- "$1" "$LOG" || fail "log lacks: $1"; }

expect_no_log() {
  if grep -qF -- "$1" "$LOG"; then fail "log has: $1"; fi
}

count_log() { grep -cF -- "$1" "$LOG" || true; }

# expect_order <text>...: each text appears in the log after the previous one.
expect_order() {
  local after=0 p n
  for p in "$@"; do
    n=$(tail -n +"$((after + 1))" "$LOG" | grep -nF -- "$p" | head -n 1 | cut -d: -f1 || true)
    [ -n "$n" ] || fail "log lacks (after line $after): $p"
    after=$((after + n))
  done
}

expect_content() { [ "$(cat "$1")" = "$2" ] || fail "$1 is '$(cat "$1")', want '$2'"; }

expect_absent() { [ ! -e "$1" ] || fail "$1 exists"; }

expect_image_env() {
  expect_content "$DEPLOY/image.env" "$(printf 'IMAGE=%s\nPREVIOUS=%s' "$1" "$2")"
}

backup_cmd() {
  printf 'docker run --rm --network none --read-only --cap-drop ALL --user 65532:65532 -e TZ=Europe/London -v %s:/data %s backup --pre-deploy' \
    "$DATA" "$1"
}

# ---- fixtures ------------------------------------------------------------------------------------

# given_current: old111 (revision oldrev) is deployed, enabled, running and healthy, with a database.
given_current() {
  printf 'IMAGE=%s\nPREVIOUS=%s\n' "$OLD" "$PREV" >"$DEPLOY/image.env"
  printf 'original\n' >"$DATA/anki-app.db"
  printf '%s\n' "$OLD" >"$STUB_STATE/running"
  : >"$STUB_STATE/enabled"
  export STUB_REVISION_old111=oldrev STUB_HEALTH_STATUS_old111=200 STUB_HEALTH_VERSION_old111=oldrev
}

# given_new [healthy|unhealthy]: :main resolves to new222 with revision newrev.
given_new() {
  export STUB_DIGEST=sha256:new222 STUB_REVISION=newrev
  if [ "${1:-healthy}" = healthy ]; then
    export STUB_HEALTH_STATUS_new222=200 STUB_HEALTH_VERSION_new222=newrev
  else
    export STUB_HEALTH_STATUS_new222=503 STUB_HEALTH_VERSION_new222=newrev
  fi
}

# ---- runner --------------------------------------------------------------------------------------

tests=()

run_one() {
  set -e
  T=$(mktemp -d "$run_root/$1.XXXXXX")
  export ANKI_ETC="$T/etc" ANKI_VAR="$T/var" ANKI_LOCK="$T/anki-update.lock"
  export STUB_LOG="$T/log" STUB_STATE="$T/state" HEALTH_TIMEOUT=0 HEALTH_INTERVAL=0
  DATA="$ANKI_VAR/data"
  DEPLOY="$ANKI_VAR/deploy"
  LOG="$STUB_LOG"
  mkdir -p "$ANKI_ETC" "$DATA" "$DEPLOY" "$STUB_STATE"
  : >"$LOG"
  "$1"
}

# run_tests: runs every function named in $tests; exit status 0 only if all pass.
run_tests() {
  local t rc passed=0 failed=0
  for t in "${tests[@]}"; do
    set +e
    (run_one "$t") >"$run_root/$t.out" 2>&1
    rc=$?
    set -e
    if [ "$rc" = 0 ]; then
      echo "ok   $t"
      passed=$((passed + 1))
    else
      echo "FAIL $t"
      cat "$run_root/$t.out"
      failed=$((failed + 1))
    fi
  done
  echo "$passed passed, $failed failed (temp files in $run_root)"
  [ "$failed" = 0 ]
}
