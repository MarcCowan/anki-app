#!/usr/bin/env bash
# Local container smoke test (deploy spec ยง5.2). Not part of check.sh. Needs Docker running.
# Exits non-zero on the first failure.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
# Git Bash (MSYS) would rewrite container paths such as /tmp and /data into Windows paths.
export MSYS_NO_PATHCONV=1
step() { printf '\n==== %s ====\n' "$*"; }

image="anki-app:smoke"
vol="anki-smoke-$(date -u +%Y%m%dT%H%M%SZ)"
ctr="$vol"
version="$(cd "$root" && git describe --always --dirty)"

# Remove the container and volume by their exact names.
cleanup() {
  docker stop "$ctr" >/dev/null 2>&1 || true
  docker rm "$ctr" >/dev/null 2>&1 || true
  docker volume rm "$vol" >/dev/null 2>&1 || true
}
trap cleanup EXIT

step "docker build ($version)"
cd "$root"
docker build --build-arg "VERSION=$version" -t "$image" .

step "create volume $vol"
docker volume create "$vol" >/dev/null

step "run container"
docker run -d --name "$ctr" \
  --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges \
  -e ADDR=0.0.0.0:8080 -e ALLOWED_HOSTS=127.0.0.1,localhost \
  -v "$vol:/data" -p 127.0.0.1:18081:8080 "$image" >/dev/null

step "health reports version $version"
health=""
for _ in $(seq 1 30); do
  if health="$(curl -fsS http://127.0.0.1:18081/api/health 2>/dev/null)"; then break; fi
  health=""
  sleep 1
done
if [ -z "$health" ]; then
  echo "health endpoint never answered"
  docker logs "$ctr" || true
  exit 1
fi
echo "$health"
case "$health" in
  *"\"version\":\"$version\""*) ;;
  *) echo "expected version $version"; exit 1 ;;
esac

step "process runs as uid 65532"
if ! procs="$(docker top "$ctr" -o pid,uid 2>/dev/null)"; then
  procs="$(docker top "$ctr" -o pid,user)"  # busybox ps has no uid column
fi
echo "$procs"
# Every data row (skip the header) must be uid 65532, and there must be at least one.
if ! printf '%s\n' "$procs" | awk 'NR>1 { n++; if ($2 != "65532") bad=1 } END { exit (n>0 && !bad) ? 0 : 1 }'; then
  echo "container process is not uid 65532"
  exit 1
fi

step "backup writes under daily/"
# Mirror the VM updater (spec ง4.5/ง7.5): stop the server, then run the one-shot backup hardened.
docker stop "$ctr" >/dev/null
out="$(docker run --rm --network none --read-only --cap-drop ALL --user 65532:65532   -e TZ=Europe/London -v "$vol:/data" "$image" backup)"
last="$(printf '%s\n' "$out" | tail -n 1)"
echo "$last"
if ! printf '%s\n' "$last" | grep -Eq '^/data/backups/daily/anki-app-[0-9]{8}T[0-9]{6}Z\.db$'; then
  echo "unexpected backup path"
  exit 1
fi

echo "SMOKE PASSED"
