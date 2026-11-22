#!/usr/bin/env bash
# Updates the pinned gitleaks in scripts/gitleaks.version. Does NOT commit: open a PR yourself.
#   bash scripts/bump-gitleaks.sh            newest non-prerelease published at least 14 days ago
#   bash scripts/bump-gitleaks.sh 8.31.0     a specific version
#   bash scripts/bump-gitleaks.sh --check    print the candidate; exit 0 if the pin is current, 10 if an update exists
# Test overrides: GITLEAKS_RELEASES_JSON, GITLEAKS_CHECKSUMS_FILE, GITLEAKS_VERSION_FILE, GITLEAKS_NOW (epoch seconds).
# Optional GH_TOKEN/GITHUB_TOKEN raises the API rate limit.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
vfile="${GITLEAKS_VERSION_FILE:-$root/scripts/gitleaks.version}"
min_age_days=14

check=0
want=""
for a in "$@"; do
  case "$a" in
    --check) check=1 ;;
    -*) echo "bump-gitleaks: unknown option $a"; exit 2 ;;
    *) want="${a#v}" ;;
  esac
done

command -v jq >/dev/null 2>&1 || { echo "bump-gitleaks: jq is required"; exit 2; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# shellcheck source=gitleaks.version
old="$(. "$vfile" && echo "$GITLEAKS_VERSION")"

if [ -z "$want" ]; then
  releases="${GITLEAKS_RELEASES_JSON:-}"
  if [ -z "$releases" ]; then
    releases="$tmp/releases.json"
    auth=()
    token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
    if [ -n "$token" ]; then auth=(-H "Authorization: Bearer $token"); fi
    code="$(curl -sS -o "$releases" -w '%{http_code}' "${auth[@]}" \
      -H 'Accept: application/vnd.github+json' \
      'https://api.github.com/repos/gitleaks/gitleaks/releases?per_page=50')" || code=000
    case "$code" in
      200) ;;
      403|429) echo "bump-gitleaks: GitHub API rate limit or access refused (HTTP $code). Wait, or set GH_TOKEN (e.g. GH_TOKEN=\$(gh auth token))."; exit 3 ;;
      *) echo "bump-gitleaks: GitHub API returned HTTP $code"; exit 3 ;;
    esac
  fi
  now="${GITLEAKS_NOW:-$(date +%s)}"
  want="$(jq -r --argjson now "$now" --argjson age "$min_age_days" '
      [ .[] | select((.draft | not) and (.prerelease | not)
          and ((.published_at | fromdateiso8601) <= ($now - $age * 86400))) ]
      | map(.tag_name | ltrimstr("v")) | map(select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$")))
      | .[]' "$releases" | sort -V | tail -n 1)"
  if [ -z "$want" ]; then
    echo "bump-gitleaks: no non-prerelease gitleaks release at least $min_age_days days old found"
    exit 3
  fi
fi

if ! printf '%s' "$want" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "bump-gitleaks: bad version '$want'"; exit 2
fi

if [ "$check" = 1 ]; then
  echo "pinned: $old"
  echo "candidate: $want"
  newest="$(printf '%s\n%s\n' "$old" "$want" | sort -V | tail -n 1)"
  if [ "$want" != "$old" ] && [ "$newest" = "$want" ]; then
    echo "update available"
    exit 10
  fi
  echo "pin is current"
  exit 0
fi

sums="${GITLEAKS_CHECKSUMS_FILE:-}"
if [ -z "$sums" ]; then
  sums="$tmp/checksums.txt"
  curl -fsSL -o "$sums" "https://github.com/gitleaks/gitleaks/releases/download/v${want}/gitleaks_${want}_checksums.txt" \
    || { echo "bump-gitleaks: could not download the checksums file for v$want"; exit 3; }
fi

# Asset names must match scripts/setup-hooks.sh.
hash_for() {
  local asset="$1" h
  h="$(tr -d '\r' < "$sums" | awk -v a="$asset" '$2 == a { print $1 }')"
  if [ -z "$h" ]; then echo "bump-gitleaks: $asset is missing from the checksums file" >&2; return 1; fi
  if [ "$(printf '%s\n' "$h" | wc -l)" -ne 1 ]; then echo "bump-gitleaks: $asset appears more than once" >&2; return 1; fi
  if ! printf '%s' "$h" | grep -Eq '^[0-9a-fA-F]{64}$'; then echo "bump-gitleaks: hash for $asset is not 64 hex characters" >&2; return 1; fi
  printf '%s' "$h" | tr 'A-F' 'a-f'
}
linux="$(hash_for "gitleaks_${want}_linux_x64.tar.gz")" || exit 1
windows="$(hash_for "gitleaks_${want}_windows_x64.zip")" || exit 1
darwin="$(hash_for "gitleaks_${want}_darwin_arm64.tar.gz")" || exit 1

# Rewrite into a temp file first: a failure above leaves the pin untouched.
awk -v v="$want" -v l="$linux" -v w="$windows" -v d="$darwin" '
  /^GITLEAKS_VERSION=/ { print "GITLEAKS_VERSION=" v; next }
  /^GITLEAKS_SHA256_LINUX_X64=/ { print "GITLEAKS_SHA256_LINUX_X64=" l; next }
  /^GITLEAKS_SHA256_WINDOWS_X64=/ { print "GITLEAKS_SHA256_WINDOWS_X64=" w; next }
  /^GITLEAKS_SHA256_DARWIN_ARM64=/ { print "GITLEAKS_SHA256_DARWIN_ARM64=" d; next }
  { print }' "$vfile" > "$tmp/new.version"
cat "$tmp/new.version" > "$vfile"

echo "gitleaks: $old -> $want"
echo "Next: commit on a feat/ branch and open a PR; CI will exercise the new binary."
