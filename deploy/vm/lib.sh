#!/usr/bin/env bash
# Shared helpers for the anki-app VM scripts (spec §7). Sourced by them, never run on its own.
# The paths can be overridden by the environment, for tests.
#
# Functions here are often called as `if f; then`, where bash ignores `set -e` inside them, so every
# step that can fail checks its own result.
# shellcheck disable=SC2034 # variables are used by the scripts that source this file

ANKI_ETC=${ANKI_ETC:-/etc/anki-app}
ANKI_VAR=${ANKI_VAR:-/var/lib/anki-app}
ANKI_LOCK=${ANKI_LOCK:-/run/anki-update.lock}
ANKI_REPO=${ANKI_REPO:-ghcr.io/marccowan/anki-app}
HEALTH_URL=${HEALTH_URL:-http://127.0.0.1:8080/api/health}
HEALTH_TIMEOUT=${HEALTH_TIMEOUT:-60}
HEALTH_INTERVAL=${HEALTH_INTERVAL:-2}

DATA="$ANKI_VAR/data"
DEPLOY="$ANKI_VAR/deploy"

# log <msg>: one line on stderr (the journal), prefixed with the script name.
log() {
  printf '%s: %s\n' "${0##*/}" "$*" >&2
}

# read_image_env: sets IMAGE and PREVIOUS from image.env; empty when the file or a line is absent.
read_image_env() {
  local key value
  IMAGE=""
  PREVIOUS=""
  if [ ! -f "$DEPLOY/image.env" ]; then
    return 0
  fi
  while IFS='=' read -r key value || [ -n "$key" ]; do
    case $key in
      IMAGE) IMAGE=$value ;;
      PREVIOUS) PREVIOUS=$value ;;
    esac
  done <"$DEPLOY/image.env"
}

# write_image_env <image> <previous>: replaces image.env in one step (temp file, then mv).
write_image_env() {
  local tmp="$DEPLOY/image.env.tmp"
  printf 'IMAGE=%s\nPREVIOUS=%s\n' "$1" "$2" >"$tmp" || return 1
  chmod 0644 "$tmp" || return 1
  mv -f "$tmp" "$DEPLOY/image.env" || return 1
}

# image_revision <image>: prints the image's org.opencontainers.image.revision label, or "" if it
# has none.
image_revision() {
  local info
  info=$(docker image inspect "$1") || return 1
  jq -r '.[0].Config.Labels["org.opencontainers.image.revision"] // ""' <<<"$info" || return 1
}

# predeploy_backup <image>: takes a pre-deploy backup with <image> while the app is stopped (§7.5
# step 4) and prints the backup's host path. The container prints its own path (/data/…) as the last
# stdout line; its log lines go to stderr. Fails if the backup fails or the file isn't there.
predeploy_backup() {
  local out last host
  out=$(docker run --rm --network none --read-only --cap-drop ALL --user 65532:65532 \
    -e TZ=Europe/London -v "$DATA:/data" "$1" backup --pre-deploy) || return 1
  last=$(printf '%s\n' "$out" | tail -n 1)
  if ! [[ $last =~ ^/data/backups/pre-deploy/anki-app-[0-9]{8}T[0-9]{6}Z\.db$ ]]; then
    log "backup printed an unexpected path: $last"
    return 1
  fi
  host="$DATA/${last#/data/}"
  if [ ! -f "$host" ]; then
    log "backup file is missing: $host"
    return 1
  fi
  printf '%s\n' "$host"
}

# restore_db <file>: puts <file> in place as the database (§7.5 step 7.2). The app must be stopped.
# If it fails before the mv, the database is unchanged and the half-made copy is removed.
restore_db() {
  local db="$DATA/anki-app.db"
  if ! cp "$1" "$db.restoring" || ! chown 65532:65532 "$db.restoring" ||
    ! chmod 0600 "$db.restoring" || ! mv -f "$db.restoring" "$db"; then
    rm -f "$db.restoring"
    return 1
  fi
  rm -f "$db-wal" "$db-shm" || return 1
}

# health_check <want>: 0 once /api/health answers 200 with .version == <want>, within HEALTH_TIMEOUT
# seconds, asking every HEALTH_INTERVAL seconds. An empty <want> (no revision label) never passes.
health_check() {
  local want=$1 body code version deadline
  body=$(mktemp) || return 1
  deadline=$((SECONDS + HEALTH_TIMEOUT))
  while :; do
    code=$(curl -s --max-time 5 -o "$body" -w '%{http_code}' -H 'Host: 127.0.0.1' "$HEALTH_URL") ||
      code=000
    if [ "$code" = 200 ] && [ -n "$want" ]; then
      version=$(jq -r '.version // ""' "$body" 2>/dev/null) || version=""
      if [ "$version" = "$want" ]; then
        rm -f "$body"
        return 0
      fi
    fi
    if [ "$SECONDS" -ge "$deadline" ]; then
      rm -f "$body"
      return 1
    fi
    sleep "$HEALTH_INTERVAL"
  done
}
