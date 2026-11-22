#!/usr/bin/env bash
# Offline tests for scripts/bump-gitleaks.sh, using fake fixtures in a mktemp dir.
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bump="$root/scripts/bump-gitleaks.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

now=1800000000   # fixed clock; "days ago" below is relative to this
iso() { date -u -d "@$((now - $1 * 86400))" +%Y-%m-%dT%H:%M:%SZ; }

cat > "$tmp/releases.json" <<JSON
[
  {"tag_name":"v9.9.9","draft":false,"prerelease":false,"published_at":"$(iso 3)"},
  {"tag_name":"v9.8.0-rc1","draft":false,"prerelease":true,"published_at":"$(iso 20)"},
  {"tag_name":"v9.7.1","draft":false,"prerelease":false,"published_at":"$(iso 30)"},
  {"tag_name":"v9.7.0","draft":false,"prerelease":false,"published_at":"$(iso 60)"}
]
JSON

h() { printf '%064d' "$1"; }
# mksums <ver>: the three real assets plus a noise line
mksums() {
  printf '%s  gitleaks_%s_linux_x64.tar.gz\n%s  gitleaks_%s_windows_x64.zip\n%s  gitleaks_%s_darwin_arm64.tar.gz\n%s  gitleaks_%s_linux_arm64.tar.gz\n' \
    "$(h 1)" "$1" "$(h 2)" "$1" "$(h 3)" "$1" "$(h 4)" "$1"
}
mkpin() {
  cat > "$tmp/pin" <<PIN
# header one
# header two
GITLEAKS_VERSION=9.0.0
GITLEAKS_SHA256_LINUX_X64=$(h 7)
GITLEAKS_SHA256_WINDOWS_X64=$(h 8)
GITLEAKS_SHA256_DARWIN_ARM64=$(h 9)
PIN
  cp "$tmp/pin" "$tmp/pin.orig"
}

export GITLEAKS_RELEASES_JSON="$tmp/releases.json"
export GITLEAKS_VERSION_FILE="$tmp/pin"
export GITLEAKS_CHECKSUMS_FILE="$tmp/sums"
export GITLEAKS_NOW="$now"

fails=0
ok() { echo "ok: $1"; }
bad() { echo "FAIL: $1"; fails=$((fails + 1)); }
run() { bash "$bump" "$@" >"$tmp/out" 2>&1; }

# 1. picks 9.7.1 (9.9.9 too new, 9.8.0-rc1 prerelease), rewrites all three hashes, keeps the header
mkpin; mksums 9.7.1 > "$tmp/sums"
if run; then
  # shellcheck source=/dev/null
  . "$tmp/pin"
  if [ "$GITLEAKS_VERSION" = 9.7.1 ] && [ "$GITLEAKS_SHA256_LINUX_X64" = "$(h 1)" ] \
     && [ "$GITLEAKS_SHA256_WINDOWS_X64" = "$(h 2)" ] && [ "$GITLEAKS_SHA256_DARWIN_ARM64" = "$(h 3)" ] \
     && grep -q '^# header two$' "$tmp/pin" && grep -q '9.0.0 -> 9.7.1' "$tmp/out"; then
    ok "picks newest eligible release, rewrites hashes, keeps header"
  else bad "wrong content after bump"; cat "$tmp/pin"; fi
else bad "bump failed"; cat "$tmp/out"; fi

# 2. missing asset: fails, file untouched
mkpin; mksums 9.7.1 | grep -v windows > "$tmp/sums"
if run; then bad "missing asset should fail"
elif cmp -s "$tmp/pin" "$tmp/pin.orig" && grep -q 'windows_x64.zip is missing' "$tmp/out"; then ok "missing asset"
else bad "missing asset: file changed or message wrong"; fi

# 3. malformed hash: fails, file untouched
mkpin
printf '%s  gitleaks_9.7.1_linux_x64.tar.gz\n%s  gitleaks_9.7.1_windows_x64.zip\n%s  gitleaks_9.7.1_darwin_arm64.tar.gz\n' \
  "$(h 1)" "$(h 2)" "tooshort" > "$tmp/sums"
if run; then bad "malformed hash should fail"
elif cmp -s "$tmp/pin" "$tmp/pin.orig" && grep -q 'not 64 hex' "$tmp/out"; then ok "malformed hash"
else bad "malformed hash: file changed or message wrong"; fi

# 4. --check: update available -> 10; current -> 0; pin newer than candidate -> 0
mkpin
run --check; rc=$?
if [ "$rc" -eq 10 ] && grep -q 'candidate: 9.7.1' "$tmp/out"; then ok "--check reports update (10)"; else bad "--check update (rc $rc)"; fi
sed -i 's/^GITLEAKS_VERSION=.*/GITLEAKS_VERSION=9.7.1/' "$tmp/pin"
run --check; rc=$?
if [ "$rc" -eq 0 ]; then ok "--check current (0)"; else bad "--check current (rc $rc)"; fi
sed -i 's/^GITLEAKS_VERSION=.*/GITLEAKS_VERSION=9.9.9/' "$tmp/pin"
run --check; rc=$?
if [ "$rc" -eq 0 ]; then ok "--check pin newer than candidate (0)"; else bad "--check newer pin (rc $rc)"; fi

# 5. explicit version skips release picking
mkpin; mksums 9.7.0 > "$tmp/sums"
if run 9.7.0 && grep -q '^GITLEAKS_VERSION=9.7.0$' "$tmp/pin"; then ok "explicit version"; else bad "explicit version"; fi

if [ "$fails" -eq 0 ]; then echo "bump-gitleaks tests passed"; else echo "$fails failure(s)"; exit 1; fi
