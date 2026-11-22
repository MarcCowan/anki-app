#!/usr/bin/env bash
# Tests for scripts/leakscan.sh: must-flag and must-pass strings for each generic rule, plus the setup-missing paths.
# Flag strings are built at runtime so this file itself stays clean under the scanner.
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
scan="$root/scripts/leakscan.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# A fake private config, never a real one.
cat > "$tmp/private.toml" <<'EOF'
title = "test"
[[rules]]
id = "fake"
regex = '''(?i)fake-secret-name'''
keywords = ["fake-secret-name"]
EOF
export ANKI_GITLEAKS_PRIVATE="$tmp/private.toml"

fails=0
n=0
# expect <exit code> <label> <text>
expect() {
  local want="$1" label="$2" text="$3" got
  n=$((n + 1))
  printf '%s\n' "$text" | bash "$scan" stdin >/dev/null 2>&1
  got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL: $label (want exit $want, got $got)"
    fails=$((fails + 1))
  fi
}

d="."          # built apart so no literal sensitive pattern sits in this file
tn="ts${d}net"
bs="\\"

# Rule 1: tailnet hostnames
expect 1 "tailnet name"            "host${d}tailabc123${d}${tn}"
expect 1 "tailnet word-word"       "see anki${d}word-word${d}${tn}/mcp"
expect 0 "placeholder <tailnet>"   "anki${d}<tailnet>${d}${tn}:8443"
expect 0 "placeholder tailXXXX"    "laptop${d}tailXXXX${d}${tn}"
expect 0 "placeholder x"           "anki${d}x${d}${tn}"
expect 0 "placeholder tail1"       "Laptop${d}tail1${d}${tn}"
expect 0 "wildcard"                "your *${d}${tn} names"
expect 0 "bare suffix"             "the ${tn} zone"

# Rule 2: CGNAT addresses
expect 1 "cgnat low edge"          "ip 100${d}64${d}0${d}1"
expect 1 "cgnat middle"            "ip 100${d}101${d}22${d}33"
expect 1 "cgnat high edge"         "ip 100${d}127${d}255${d}254"
expect 0 "just below range"        "ip 100${d}63${d}1${d}1"
expect 0 "just above range"        "ip 100${d}128${d}1${d}1"
expect 0 "placeholder"             "ip 100${d}x${d}y${d}z"
expect 0 "magicdns"                "resolver 100${d}100${d}100${d}100"
expect 0 "range network address"   "the 100${d}64${d}0${d}0/10 range"

# Rule 3: home paths
expect 1 "windows path"            "C:${bs}Users${bs}jsmith${bs}Repos"
expect 1 "windows path escaped"    "C:${bs}${bs}Users${bs}${bs}jsmith${bs}${bs}Repos"
expect 1 "linux home"              "/ho""me/jsmith/app"
expect 1 "mac home"                "/Us""ers/jsmith/app"
expect 0 "placeholder <name>"      "C:${bs}Users${bs}<name>${bs}Repos and /home/<name>/x"
expect 0 "placeholder <user>"      "/Users/<user>/x"
expect 0 "runner"                  "/home/runner/work"
expect 0 "you"                     "/Users/you/x"
expect 0 "env var"                 "/home/\$USER/x and %USERPROFILE%"

# Private pass
expect 1 "private name"            "mentions Fake-Secret-Name here"
expect 0 "clean text"              "nothing to see"

# Setup-missing paths fail closed (exit 2)
n=$((n + 1))
ANKI_GITLEAKS_PRIVATE="$tmp/absent.toml" bash "$scan" stdin </dev/null >/dev/null 2>&1
[ $? -eq 2 ] || { echo "FAIL: missing private config must exit 2"; fails=$((fails + 1)); }
n=$((n + 1))
GITLEAKS_BIN="$tmp/no-such-binary" bash "$scan" stdin </dev/null >/dev/null 2>&1
[ $? -eq 2 ] || { echo "FAIL: missing binary must exit 2"; fails=$((fails + 1)); }
# "none" skips the private pass (with a warning) and so lets the private name through, but only in GitHub Actions
n=$((n + 1))
echo "Fake-Secret-Name" | GITHUB_ACTIONS=true ANKI_GITLEAKS_PRIVATE=none bash "$scan" stdin >/dev/null 2>&1
# shellcheck disable=SC2181 # same $?-check style as the rest of this file
[ $? -eq 0 ] || { echo "FAIL: ANKI_GITLEAKS_PRIVATE=none must skip the private pass in GitHub Actions"; fails=$((fails + 1)); }
n=$((n + 1))
echo "Fake-Secret-Name" | GITHUB_ACTIONS='' ANKI_GITLEAKS_PRIVATE=none bash "$scan" stdin >/dev/null 2>&1
[ $? -eq 2 ] || { echo "FAIL: ANKI_GITLEAKS_PRIVATE=none outside GitHub Actions must exit 2"; fails=$((fails + 1)); }

# Empty or still-fake private config fails closed
tq="'''"
# expect_cfg <exit code> <label> <config file content>
expect_cfg() {
  local want="$1" label="$2" body="$3" got
  n=$((n + 1))
  printf '%s\n' "$body" > "$tmp/cfg.toml"
  echo "nothing to see" | ANKI_GITLEAKS_PRIVATE="$tmp/cfg.toml" bash "$scan" stdin >/dev/null 2>&1
  got=$?
  [ "$got" -eq "$want" ] || { echo "FAIL: $label (want exit $want, got $got)"; fails=$((fails + 1)); }
}
expect_cfg 2 "private config without rules"      'title = "x"'
expect_cfg 2 "private config with only comments" '# [[rules]] in a comment is not a rule'
expect_cfg 2 "example file copied unchanged"     "$(cat "$root/scripts/gitleaks-private.example.toml")"
for fake in example-tailnet my-project-123456 example-backup-bucket fake-laptop; do
  expect_cfg 2 "fake value $fake" "$(printf '[[rules]]\nid = "r"\nregex = %s\nkeywords = ["zzz"]\n# %s' "${tq}zzz${tq}" "$fake")"
done
expect_cfg 0 "real-looking config passes"        "$(printf '[[rules]]\nid = "r"\nregex = %s\nkeywords = ["zzz"]' "${tq}zzz-real${tq}")"

# A broken private regex must not make gitleaks print anything (it can echo config values)
n=$((n + 1))
printf '[[rules]]\nid = "SENTINEL-ID"\nregex = %s\nkeywords = ["x"]\n' "${tq}(SENTINEL-REGEX${tq}" > "$tmp/broken.toml"
out="$(echo "text" | ANKI_GITLEAKS_PRIVATE="$tmp/broken.toml" bash "$scan" stdin 2>&1)"
rc=$?
case "$out" in
  *SENTINEL*) echo "FAIL: private pass printed gitleaks output"; fails=$((fails + 1)) ;;
  *) [ "$rc" -eq 2 ] || { echo "FAIL: broken private regex must exit 2 (got $rc)"; fails=$((fails + 1)); } ;;
esac

# Rule 4: Tailscale IPv6
p6="fd7a:115c:a1e0"
expect 1 "ipv6 full"               "addr ${p6}:ab12:4843:cd96:6234:5678"
expect 1 "ipv6 compressed"         "addr ${p6}::1234:5678"
expect 0 "ipv6 bare prefix"        "the ${p6}:: prefix and ${p6}::/48"
expect 0 "ipv6 magicdns"           "resolver ${p6}::53"

# Rule 3 again: other forms and letter case
expect 1 "lower-case drive path"   "c:/us""ers/jsmith/x"
expect 1 "git-bash drive path"     "/c/us""ers/jsmith/x"
expect 1 "upper-case windows path" "C:${bs}USERS${bs}jsmith"
expect 1 "mixed-case git-bash"     "/c/Us""ers/jsmith"
expect 0 "git-bash placeholder"    "/c/Users/<name>/x"
expect 0 "api path"                "GET /v1/users/me and /api/users/jsmith"

# Rule 5: Tailscale keys (built from parts so this file stays clean)
tk="ts""key"
expect 1 "tskey auth"              "key ${tk}-auth-kABCDE1CNTRL-abcdefghij0123456789abcdefghij"
expect 1 "tskey client"            "${tk}-client-kABCDE1CNTRL-abcdefghij0123456789abcdefghij"
expect 1 "tskey api"               "export K=${tk}-api-kABCDE1CNTRL-abcdefghij0123456789"
expect 0 "tskey bare word"         "the ${tk}- prefix and ${tk} keys"
expect 0 "tskey docs ellipsis"     "copy the ${tk}-auth-… value"
expect 0 "tskey placeholder"       "set ${tk}-auth-<authkey> here"
expect 0 "tskey x filler"          "${tk}-auth-xxxxxxxxxxxx-xxxxxxxxxxxxxxxx"
expect 0 "TS_AUTHKEY variable"     "read -rsp 'key: ' TS_AUTHKEY; use \"\$TS_AUTHKEY\""

# Rule 6: GCP billing account IDs
h1="0123""45"; h2="6789""AB"; h3="CDEF""01"
expect 1 "billing id"              "account ${h1}-${h2}-${h3} here"
expect 1 "billing id at line end"  "ID=${h1}-${h2}-${h3}"
expect 1 "billing id in quotes"    "\"${h1}-${h2}-${h3}\""
expect 0 "billing placeholder"     "billing <BILLING_ACCOUNT_ID> and XXXXXX-XXXXXX-XXXXXX"
expect 0 "uuid"                    "id 123e4567-e89b-12d3-a456-426614174000"
expect 0 "lower-case hex groups"   "x 0123ab-6789ab-cdef01 y"
expect 0 "four groups"             "x ${h1}-${h2}-${h3}-${h1} y"
expect 0 "inside a base64 run"     "sha512-Z${h1}-${h2}-${h3}Q=="

# Git modes, in a throwaway repo with copies of the scripts (so the scan runs against it, not this repo).
export GITLEAKS_BIN
for c in "$root/.tools/gitleaks/gitleaks" "$root/.tools/gitleaks/gitleaks.exe"; do
  [ -x "$c" ] && GITLEAKS_BIN="$c" && break
done
r="$tmp/repo"
mkdir -p "$r/scripts" && cp "$root/scripts/leakscan.sh" "$r/scripts/" && cp "$root/.gitleaks.toml" "$r/"
g() { git -C "$r" -c user.name=t -c user.email=t@example.invalid -c commit.gpgsign=false -c core.hooksPath=/dev/null "$@"; }
rscan() { bash "$r/scripts/leakscan.sh" "$@" >/dev/null 2>&1; }
# expect_git <exit code> <label> <leakscan args...>
expect_git() {
  local want="$1" label="$2" got
  shift 2
  n=$((n + 1))
  rscan "$@"; got=$?
  [ "$got" -eq "$want" ] || { echo "FAIL: $label (want exit $want, got $got)"; fails=$((fails + 1)); }
}
leak="host${d}tailabc123${d}${tn}"
g init -q -b main
echo one > "$r/a.txt"; g add a.txt; g commit -q -m "first commit"
base="$(g rev-parse HEAD)"
expect_git 0 "history clean" history
echo two > "$r/a.txt"; g add a.txt; g commit -q -m "mentions $leak in the message"
expect_git 1 "leak in commit message (range)" range "$base..HEAD"
expect_git 1 "leak in commit message (history)" history
g reset -q --hard "$base"

# evil merge: the leak exists only in the merge commit's own diff
g checkout -q -b side; echo s > "$r/s.txt"; g add s.txt; g commit -q -m side
g checkout -q main; echo m > "$r/m.txt"; g add m.txt; g commit -q -m main-change
pre="$(g rev-parse HEAD)"
g merge -q --no-commit --no-ff side >/dev/null 2>&1
echo "see $leak" >> "$r/m.txt"; g add m.txt; g commit -q -m "merge side"
expect_git 1 "leak only in a merge diff (range)" range "$pre..HEAD"
expect_git 1 "leak only in a merge diff (history)" history
g reset -q --hard "$pre"
g merge -q --no-ff side -m "clean merge" >/dev/null 2>&1
expect_git 0 "clean merge (range)" range "$pre..HEAD"
expect_git 0 "clean merge (history)" history

# pre-push hook against a bare remote
mkdir -p "$r/.githooks" && cp "$root/.githooks/pre-push" "$r/.githooks/" && chmod +x "$r/.githooks/pre-push"
git init -q --bare -b main "$tmp/remote.git"
g remote add origin "$tmp/remote.git"
push() { git -C "$r" -c user.name=t -c user.email=t@example.invalid -c core.hooksPath=.githooks push -q "$@" >/dev/null 2>&1; }
n=$((n + 1)); push origin main || { echo "FAIL: pre-push must allow a clean new branch"; fails=$((fails + 1)); }
echo three > "$r/a.txt"; g add a.txt; g commit -q -m "oops $leak"
n=$((n + 1)); push origin main && { echo "FAIL: pre-push must block a leaking commit"; fails=$((fails + 1)); }
g reset -q --hard HEAD~1
echo four > "$r/a.txt"; g add a.txt; g commit -q -m "fine"
n=$((n + 1)); push origin main || { echo "FAIL: pre-push must allow a clean update"; fails=$((fails + 1)); }
g checkout -q -b newbranch
echo five > "$r/a.txt"; g add a.txt; g commit -q -m "leak $leak"
n=$((n + 1)); push origin newbranch && { echo "FAIL: pre-push must block a leak on a new branch"; fails=$((fails + 1)); }
g reset -q --hard HEAD~1
n=$((n + 1)); push origin newbranch || { echo "FAIL: pre-push must allow a clean new branch (2)"; fails=$((fails + 1)); }
n=$((n + 1)); push origin :newbranch || { echo "FAIL: pre-push must allow deleting a branch"; fails=$((fails + 1)); }

echo "leakscan tests: $n run, $fails failed"
[ "$fails" -eq 0 ]
