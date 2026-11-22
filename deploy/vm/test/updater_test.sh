#!/usr/bin/env bash
# Tests for anki-update (spec §7.5). Sourced by run.sh, after harness.sh.
# shellcheck disable=SC2317,SC2329 # test functions are called by name from the runner
# shellcheck source=harness.sh
. "$(dirname "${BASH_SOURCE[0]}")/harness.sh"

paused_without_arg_does_nothing() {
  given_current; given_new
  touch "$ANKI_ETC/paused"
  run_update
  expect_exit 0
  expect_no_log "docker pull"
  expect_no_log "systemctl"
}

paused_ignored_with_explicit_arg() {
  given_current; given_new
  touch "$ANKI_ETC/paused"
  run_update "$REPO:main"
  expect_exit 0
  expect_log "deployed newrev"
  expect_image_env "$NEW" "$OLD"
}

lock_held_exits_0() {
  given_current; given_new
  export STUB_FLOCK_EXIT=1
  run_update
  expect_exit 0
  expect_log "flock -n 9"
  expect_no_log "docker pull"
}

same_digest_exits_0() {
  given_current
  export STUB_DIGEST=sha256:old111 STUB_REVISION=oldrev
  run_update
  expect_exit 0
  expect_log "docker pull $REPO:main"
  expect_no_log "systemctl stop"
}

bad_digest_skipped_without_arg() {
  given_current; given_new
  printf '%s\n' "$NEW" >"$DEPLOY/bad-digests"
  run_update
  expect_exit 0
  expect_no_log "systemctl stop"
  expect_image_env "$OLD" "$PREV"
}

bad_digest_deployed_with_explicit_arg() {
  given_current; given_new
  printf '%s\n' "$NEW" >"$DEPLOY/bad-digests"
  run_update "$REPO:main"
  expect_exit 0
  expect_log "deployed newrev"
  expect_image_env "$NEW" "$OLD"
}

pull_failure_leaves_app_running() {
  given_current; given_new
  export STUB_PULL_FAIL=1
  run_update
  expect_exit 1
  expect_no_log "systemctl stop"
  expect_image_env "$OLD" "$PREV"
  expect_content "$STUB_STATE/running" "$OLD"
}

happy_path() {
  given_current; given_new
  export STUB_HEALTH_FAIL_FIRST=2 HEALTH_TIMEOUT=10
  run_update
  expect_exit 0
  expect_order "docker pull $REPO:main" "deploying newrev" "systemctl stop anki-app" \
    "$(backup_cmd "$OLD")" "systemctl start anki-app" "curl" "deployed newrev" \
    "docker image prune -f --filter until=168h"
  expect_log "-H Host: 127.0.0.1 http://127.0.0.1:8080/api/health"
  [ "$(count_log "curl ")" = 3 ] || fail "want 3 health requests (2 refused, then 200)"
  expect_image_env "$NEW" "$OLD"
  expect_order "deployed newrev" "systemctl is-enabled --quiet anki-app"
  expect_no_log "systemctl enable"
  [ -f "$DATA/backups/pre-deploy/anki-app-20261105T150001Z.db" ] || fail "no pre-deploy backup"
}

backup_failure_restarts_old() {
  given_current; given_new
  export STUB_BACKUP_FAIL=1
  run_update
  expect_exit 1
  expect_order "systemctl stop anki-app" "$(backup_cmd "$OLD")" "systemctl start anki-app"
  expect_image_env "$OLD" "$PREV"
  expect_content "$STUB_STATE/running" "$OLD"
}

backup_missing_file_restarts_old() {
  given_current; given_new
  export STUB_BACKUP_NOFILE=1
  run_update
  expect_exit 1
  expect_order "systemctl stop anki-app" "$(backup_cmd "$OLD")" "systemctl start anki-app"
  expect_image_env "$OLD" "$PREV"
  expect_content "$STUB_STATE/running" "$OLD"
}

rollback_restores_predeploy_backup() {
  given_current; given_new unhealthy
  export STUB_APP_WRITES=new222
  run_update
  expect_exit 1
  local backup="$DATA/backups/pre-deploy/anki-app-20261105T150001Z.db"
  expect_content "$backup" "original"
  expect_content "$DATA/anki-app.db" "original"
  expect_absent "$DATA/anki-app.db-wal"
  expect_absent "$DATA/anki-app.db-shm"
  expect_absent "$DATA/anki-app.db.restoring"
  expect_log "chown 65532:65532 $DATA/anki-app.db.restoring"
  if [ "$(uname -s)" = Linux ]; then
    [ "$(stat -c %a "$DATA/anki-app.db")" = 600 ] || fail "restored database is not mode 0600"
  fi
  expect_image_env "$OLD" "$PREV"
  expect_content "$DEPLOY/bad-digests" "$NEW"
  expect_content "$STUB_STATE/running" "$OLD"
  expect_order "systemctl start anki-app" "systemctl stop anki-app" "systemctl start anki-app"
  expect_no_log "MANUAL ATTENTION NEEDED"
}

rollback_unhealthy_exits_2() {
  given_current; given_new unhealthy
  export STUB_HEALTH_STATUS_old111=503
  run_update
  expect_exit 2
  expect_log "MANUAL ATTENTION NEEDED"
  expect_image_env "$OLD" "$PREV"
  expect_content "$DEPLOY/bad-digests" "$NEW"
}

# The old version comes back and is healthy, but on the bad version's database: that needs a human.
rollback_restore_failure_exits_2() {
  given_current; given_new unhealthy
  export STUB_CHOWN_FAIL=1
  run_update
  expect_exit 2
  expect_log "MANUAL ATTENTION NEEDED"
  expect_image_env "$OLD" "$PREV"
  expect_content "$DEPLOY/bad-digests" "$NEW"
  expect_content "$STUB_STATE/running" "$OLD"
  expect_absent "$DATA/anki-app.db.restoring"
}

rollback_stop_failure_exits_2() {
  given_current; given_new unhealthy
  export STUB_STOP_FAIL_AT=2
  run_update
  expect_exit 2
  expect_log "MANUAL ATTENTION NEEDED"
  expect_no_log "chown"
  expect_content "$DEPLOY/bad-digests" "$NEW"
}

# The current image is deployed but there's no database: there's nothing to back up or restore.
rollback_without_db() {
  given_current; given_new unhealthy
  rm "$DATA/anki-app.db"
  run_update
  expect_exit 1
  expect_no_log "docker run"
  expect_no_log "chown"
  expect_order "systemctl start anki-app" "systemctl stop anki-app" "systemctl start anki-app" \
    "rolled back to oldrev"
  expect_image_env "$OLD" "$PREV"
  expect_content "$DEPLOY/bad-digests" "$NEW"
  expect_content "$STUB_STATE/running" "$OLD"
  expect_no_log "MANUAL ATTENTION NEEDED"
}

stop_failure_restarts_old() {
  given_current; given_new
  export STUB_STOP_FAIL_AT=1
  run_update
  expect_exit 1
  expect_order "systemctl stop anki-app" "systemctl start anki-app"
  expect_no_log "docker run"
  expect_image_env "$OLD" "$PREV"
  expect_content "$STUB_STATE/running" "$OLD"
}

image_env_write_failure_restarts_old() {
  given_current; given_new
  mkdir "$DEPLOY/image.env.tmp"   # the temp file can't be written
  run_update
  expect_exit 1
  expect_order "systemctl stop anki-app" "$(backup_cmd "$OLD")" "could not write image.env" \
    "systemctl start anki-app"
  [ "$(count_log "systemctl start")" = 1 ] || fail "want one start (the old version)"
  expect_image_env "$OLD" "$PREV"
  expect_content "$STUB_STATE/running" "$OLD"
}

# Every successful deploy makes sure the app starts after a reboot, not just the first one.
deploy_enables_service_if_disabled() {
  given_current; given_new
  rm "$STUB_STATE/enabled"
  run_update
  expect_exit 0
  expect_order "deployed newrev" "systemctl is-enabled --quiet anki-app" "systemctl enable anki-app"
}

# The new version is serving, so a failed enable is a warning, not "not deployed".
enable_failure_still_deployed() {
  given_new
  export STUB_ENABLE_FAIL=1
  run_update
  expect_exit 0
  expect_order "deployed newrev" "systemctl enable anki-app" "could not enable anki-app"
  expect_log "docker image prune"
  expect_image_env "$NEW" ""
}

# Acceptance test 5 (spec §11.3) deploys distroless: no revision label, so WANT is empty. Even an
# answer of 200 with an empty version must not count as healthy.
empty_revision_label_rolls_back() {
  given_current
  local dist=gcr.io/distroless/static-debian12@sha256:dist333
  export STUB_HEALTH_STATUS_dist333=200 STUB_HEALTH_VERSION_dist333=""
  run_update "$dist"
  expect_exit 1
  expect_order "docker pull $dist" "systemctl stop anki-app" "$(backup_cmd "$OLD")" \
    "systemctl start anki-app" "systemctl stop anki-app" "systemctl start anki-app"
  expect_image_env "$OLD" "$PREV"
  expect_content "$DEPLOY/bad-digests" "$dist"
  expect_content "$DATA/anki-app.db" "original"
  expect_content "$STUB_STATE/running" "$OLD"
}

first_deploy_enables_service() {
  given_new
  run_update
  expect_exit 0
  expect_no_log "docker run"
  expect_order "systemctl start anki-app" "deployed newrev" "systemctl enable anki-app"
  expect_image_env "$NEW" ""
}

first_deploy_failure_stops() {
  given_new unhealthy
  run_update
  expect_exit 1
  expect_order "systemctl start anki-app" "systemctl stop anki-app"
  [ "$(count_log "systemctl start")" = 1 ] || fail "started again after a failed first deploy"
  expect_no_log "docker run"
  expect_no_log "chown"
  expect_no_log "systemctl enable"
  expect_image_env "" ""
  expect_absent "$STUB_STATE/running"
}

# A database without a current image (e.g. a first deploy that failed after creating it): the only
# image that can take the backup is the new one.
no_image_with_db_backs_up_with_new_image() {
  given_new
  printf 'original\n' >"$DATA/anki-app.db"
  run_update
  expect_exit 0
  expect_order "systemctl stop anki-app" "$(backup_cmd "$NEW")" "systemctl start anki-app"
  [ -f "$DATA/backups/pre-deploy/anki-app-20261105T150001Z.db" ] || fail "no pre-deploy backup"
}

tests+=(
  paused_without_arg_does_nothing
  paused_ignored_with_explicit_arg
  lock_held_exits_0
  same_digest_exits_0
  bad_digest_skipped_without_arg
  bad_digest_deployed_with_explicit_arg
  pull_failure_leaves_app_running
  happy_path
  backup_failure_restarts_old
  backup_missing_file_restarts_old
  rollback_restores_predeploy_backup
  rollback_unhealthy_exits_2
  rollback_restore_failure_exits_2
  empty_revision_label_rolls_back
  first_deploy_enables_service
  first_deploy_failure_stops
  no_image_with_db_backs_up_with_new_image
  rollback_stop_failure_exits_2
  rollback_without_db
  stop_failure_restarts_old
  image_env_write_failure_restarts_old
  deploy_enables_service_if_disabled
  enable_failure_still_deployed
)
