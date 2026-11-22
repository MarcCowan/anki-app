#!/usr/bin/env bash
# Tests for the VM scripts (spec §7.5–7.7), run against the stubs in stubs/ (docker, systemctl,
# curl, flock, chown, gcloud). The tests are in *_test.sh; the helpers and runner in harness.sh.
# Needs bash and jq. `run.sh --jq-available` only reports whether jq can be found (exit 0/1).
set -euo pipefail

# shellcheck source=harness.sh
. "$(dirname "$0")/harness.sh"

if [ "${1:-}" = --jq-available ]; then
  if jq_available; then exit 0; fi
  exit 1
fi

# shellcheck source=updater_test.sh
. "$here/updater_test.sh"
# shellcheck source=restore_test.sh
. "$here/restore_test.sh"
# shellcheck source=offsite_test.sh
. "$here/offsite_test.sh"

setup_path
run_tests
