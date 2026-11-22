#!/usr/bin/env bash
# Tests for anki-restore (spec §7.6). Sourced by run.sh, after harness.sh.
# shellcheck disable=SC2317,SC2329 # test functions are called by name from the runner
# shellcheck source=harness.sh
. "$(dirname "${BASH_SOURCE[0]}")/harness.sh"

# given_backup_file: $CHOSEN is a backup to restore (a SQLite header, then a marker).
given_backup_file() {
  CHOSEN="$T/in/anki-app-20261101T030000Z.db"
  mkdir -p "$T/in"
  printf 'SQLite format 3 chosen\n' >"$CHOSEN"
}

PRE_RESTORE_NAME=anki-app-20261105T150001Z.db

run_restore() { run_script "$RESTORE" "$@"; }

restore_requires_yes_or_confirmation() {
  given_current; given_backup_file
  echo n >"$T/stdin"
  run_restore "$CHOSEN"
  expect_exit 1
  expect_no_log "systemctl stop"
  expect_no_log "docker run"
  expect_content "$DATA/anki-app.db" "original"
}

restore_confirmed_with_y() {
  given_current; given_backup_file
  echo y >"$T/stdin"
  run_restore "$CHOSEN"
  expect_exit 0
  expect_content "$DATA/anki-app.db" "SQLite format 3 chosen"
}

restore_happy_path() {
  given_current; given_backup_file
  : >"$DATA/anki-app.db-wal"
  : >"$DATA/anki-app.db-shm"
  run_restore --yes "$CHOSEN"
  expect_exit 0
  expect_order "systemctl stop anki-app" "$(backup_cmd "$OLD")" \
    "chown 65532:65532 $DATA/anki-app.db.restoring" "systemctl start anki-app" \
    "-H Host: 127.0.0.1 http://127.0.0.1:8080/api/health" "restored $CHOSEN"
  expect_log "docker image inspect $OLD"
  expect_content "$DATA/backups/pre-deploy/$PRE_RESTORE_NAME" "original"
  expect_log "pre-restore backup: $DATA/backups/pre-deploy/$PRE_RESTORE_NAME"
  expect_content "$DATA/anki-app.db" "SQLite format 3 chosen"
  expect_content "$CHOSEN" "SQLite format 3 chosen"
  expect_absent "$DATA/anki-app.db-wal"
  expect_absent "$DATA/anki-app.db-shm"
  expect_absent "$DATA/anki-app.db.restoring"
  expect_absent "$DATA/anki-app.db.restore-source"
  expect_content "$STUB_STATE/running" "$OLD"
  expect_image_env "$OLD" "$PREV"
}

# The health target is the current image's revision label: any other version is a failure.
restore_health_failure_prints_backup() {
  given_current; given_backup_file
  export STUB_HEALTH_VERSION_old111=otherrev
  run_restore --yes "$CHOSEN"
  expect_exit 1
  expect_order "systemctl start anki-app" "health check failed" \
    "$DATA/backups/pre-deploy/$PRE_RESTORE_NAME"
  expect_content "$DATA/anki-app.db" "SQLite format 3 chosen"
}

restore_missing_file() {
  given_current
  run_restore --yes "$T/no-such-file.db"
  expect_exit 1
  expect_no_log "systemctl"
  expect_no_log "docker"
}

restore_not_a_database() {
  given_current
  printf 'hello\n' >"$T/notes.txt"
  run_restore --yes "$T/notes.txt"
  expect_exit 1
  expect_no_log "systemctl"
  expect_content "$DATA/anki-app.db" "original"
}

restore_nothing_deployed() {
  given_backup_file
  run_restore --yes "$CHOSEN"
  expect_exit 1
  expect_no_log "systemctl"
}

restore_lock_held() {
  given_current; given_backup_file
  export STUB_FLOCK_EXIT=1
  run_restore --yes "$CHOSEN"
  expect_exit 1
  expect_no_log "systemctl"
  expect_content "$DATA/anki-app.db" "original"
}

# Never restore without a backup of the current database: start the app again unchanged.
restore_backup_failure_restarts() {
  given_current; given_backup_file
  export STUB_BACKUP_FAIL=1
  run_restore --yes "$CHOSEN"
  expect_exit 1
  expect_order "systemctl stop anki-app" "$(backup_cmd "$OLD")" "systemctl start anki-app"
  expect_no_log "chown"
  expect_content "$DATA/anki-app.db" "original"
  expect_content "$STUB_STATE/running" "$OLD"
  expect_absent "$DATA/anki-app.db.restore-source"
}

# The copy fails before the swap: the database is unchanged, so the app starts again on it.
restore_copy_failure_restarts() {
  given_current; given_backup_file
  export STUB_CHOWN_FAIL=1
  run_restore --yes "$CHOSEN"
  expect_exit 1
  expect_content "$DATA/anki-app.db" "original"
  expect_absent "$DATA/anki-app.db.restoring"
  expect_order "chown" "systemctl start anki-app" "$DATA/backups/pre-deploy/$PRE_RESTORE_NAME"
}

# The swap happened but the old -wal can't be removed (here: it's a folder): leave the app stopped.
restore_wal_removal_failure_exits_2() {
  given_current; given_backup_file
  mkdir "$DATA/anki-app.db-wal"
  run_restore --yes "$CHOSEN"
  expect_exit 2
  expect_log "MANUAL ATTENTION NEEDED"
  expect_log "pre-restore backup: $DATA/backups/pre-deploy/$PRE_RESTORE_NAME"
  [ "$(count_log "systemctl start")" = 0 ] || fail "started with a stale -wal"
  expect_content "$DATA/anki-app.db" "SQLite format 3 chosen"
  rmdir "$DATA/anki-app.db-wal"
}

restore_without_db_skips_backup() {
  given_current; given_backup_file
  rm "$DATA/anki-app.db"
  run_restore --yes "$CHOSEN"
  expect_exit 0
  expect_no_log "docker run"
  expect_content "$DATA/anki-app.db" "SQLite format 3 chosen"
}

restore_usage() {
  given_current
  run_restore
  expect_exit 1
  expect_log "usage: anki-restore"
  run_restore --yes a.db b.db
  expect_exit 1
}

tests+=(
  restore_requires_yes_or_confirmation
  restore_confirmed_with_y
  restore_happy_path
  restore_health_failure_prints_backup
  restore_missing_file
  restore_not_a_database
  restore_nothing_deployed
  restore_lock_held
  restore_backup_failure_restarts
  restore_copy_failure_restarts
  restore_wal_removal_failure_exits_2
  restore_without_db_skips_backup
  restore_usage
)
