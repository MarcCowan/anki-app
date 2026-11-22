#!/usr/bin/env bash
# Tests for anki-offsite (spec §7.7). Sourced by run.sh, after harness.sh.
# shellcheck disable=SC2317,SC2329 # test functions are called by name from the runner
# shellcheck source=harness.sh
. "$(dirname "${BASH_SOURCE[0]}")/harness.sh"

BUCKET=anki-app-backups-0a1b2c3d

given_offsite() {
  printf 'BUCKET=%s\n' "$BUCKET" >"$ANKI_ETC/offsite.env"
  mkdir -p "$DATA/backups/daily"
  printf 'x\n' >"$DATA/backups/daily/anki-app-20261105T030000Z.db"
}

run_offsite() { run_script "$OFFSITE"; }

offsite_never_deletes() {
  given_offsite
  run_offsite
  expect_exit 0
  expect_log "gcloud storage rsync --recursive"
  expect_log "$DATA/backups gs://$BUCKET/backups"
  [ "$(count_log "gcloud ")" = 1 ] || fail "want exactly one gcloud call"
  expect_no_log "--delete-unmatched-destination-objects"
}

# A half-written backup (<name>.db.partial, renamed only once complete) must never reach the
# append-only bucket, where the VM could not delete it again.
offsite_skips_partials() {
  given_offsite
  run_offsite
  expect_exit 0
  expect_log 'gcloud storage rsync --recursive --exclude .*\.partial$ '
}

offsite_missing_env() {
  mkdir -p "$DATA/backups/daily"
  run_offsite
  expect_exit 1
  expect_log "offsite.env"
  expect_no_log "gcloud"
}

offsite_invalid_bucket() {
  given_offsite
  printf 'BUCKET=gs://%s/x\n' "$BUCKET" >"$ANKI_ETC/offsite.env"
  run_offsite
  expect_exit 1
  expect_no_log "gcloud"
}

offsite_gcloud_failure() {
  given_offsite
  export STUB_GCLOUD_FAIL=1
  run_offsite
  expect_exit 1
}

# Before the first backup (a fresh VM) there's nothing to copy; that's not an error.
offsite_no_backups_yet() {
  printf 'BUCKET=%s\n' "$BUCKET" >"$ANKI_ETC/offsite.env"
  run_offsite
  expect_exit 0
  expect_no_log "gcloud"
}

tests+=(
  offsite_never_deletes
  offsite_skips_partials
  offsite_missing_env
  offsite_invalid_bucket
  offsite_gcloud_failure
  offsite_no_backups_yet
)
