#!/usr/bin/env bash
# Drives leakscan-hook.sh with sample payloads against a stub scanner. Run: bash .claude/hooks/test-leakscan-hook.sh
# Payloads are deliberately literal (SC2016).
# shellcheck disable=SC2016
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
hook="$here/leakscan-hook.sh"
tmp="$(mktemp -d)"
trap 'rm -r "$tmp"' EXIT   # fixed child of mktemp, not a computed target

# Stub: exit 1 if stdin has LEAKME, 2 if STUB_MISSING set, else 0. Records stdin length.
cat > "$tmp/stub.sh" <<'STUB'
#!/usr/bin/env bash
in="$(cat)"
printf '%s' "${#in}" > "${STUB_LOG:-/dev/null}"
[ -n "$STUB_MISSING" ] && { echo "stub: setup missing" >&2; exit 2; }
case "$in" in *LEAKME*) echo "stub: leak found" >&2; exit 1 ;; esac
exit 0
STUB
export ANKI_LEAKSCAN="$tmp/stub.sh"
export STUB_LOG="$tmp/log"

fail=0
# mk <json-expression using $a>: builds payload JSON with node so quoting/unicode are always valid.
mk() { node -e "const a=process.argv[1]; process.stdout.write(JSON.stringify($2))" "$1"; }

check() { # name expected_rc payload [env...]
  local name="$1" want="$2" payload="$3"; shift 3
  local err rc
  err="$(printf '%s' "$payload" | env "$@" bash "$hook" 2>&1 >/dev/null)"; rc=$?
  if [ "$rc" != "$want" ]; then echo "FAIL $name: exit $rc, want $want"; fail=1; return; fi
  case "$err" in *LEAKME*) echo "FAIL $name: stderr leaks the value"; fail=1; return ;; esac
  echo "ok   $name (exit $rc)"
}

# Write/Edit are not scanned any more (the pre-commit hook catches files); a leak in one must pass.
check write-leak-not-our-job 0 "$(mk x '{tool_name:"Write",tool_input:{file_path:"a.md",content:"x LEAKME y"}}')" A=1
check edit-leak-not-our-job 0 "$(mk x '{tool_name:"Edit",tool_input:{file_path:"a",old_string:"a",new_string:"LEAKME"}}')" A=1
check bash-gh-leak 2 "$(mk x '{tool_name:"Bash",tool_input:{command:"gh pr create --title t --body \"LEAKME\""}}')" A=1
check bash-gh-api-chained-leak 2 "$(mk x '{tool_name:"Bash",tool_input:{command:"cd x && gh api repos/o/r/issues -f body=LEAKME"}}')" A=1
check bash-commit-leak 2 "$(mk x '{tool_name:"Bash",tool_input:{command:"git commit -m \"$(cat <<EOF\nmsg LEAKME\nEOF\n)\""}}')" A=1
check bash-git-C-commit-leak 2 "$(mk x '{tool_name:"Bash",tool_input:{command:"git -C d commit -m LEAKME"}}')" A=1
check bash-gh-clean 0 "$(mk x '{tool_name:"Bash",tool_input:{command:"gh pr create --title t --body ok"}}')" A=1
check bash-other-ignored 0 "$(mk x '{tool_name:"Bash",tool_input:{command:"echo LEAKME; ls"}}')" A=1
check bash-git-status-ignored 0 "$(mk x '{tool_name:"Bash",tool_input:{command:"git status LEAKME"}}')" A=1
check other-tool-ignored 0 "$(mk x '{tool_name:"Read",tool_input:{file_path:"LEAKME"}}')" A=1
check multiline-quotes-unicode-clean 0 "$(mk x '{tool_name:"Bash",tool_input:{command:"gh issue create --body \"l1\n\\\"q\\\" é中😀\nl3\""}}')" A=1
check multiline-quotes-unicode-leak 2 "$(mk x '{tool_name:"Bash",tool_input:{command:"gh issue create --body \"l1\n\\\"q\\\" é中\nLEAKME\nl4\""}}')" A=1
# Large file (3 MB, leak on the last line) goes through a body file, so it never hits argv limits.
node -e 'process.stdout.write("abcdefghij".repeat(300000)+"\nLEAKME\n")' > "$tmp/big-leak.txt"
node -e 'process.stdout.write("abcdefghij".repeat(300000)+"\n")' > "$tmp/big-clean.txt"
tmpw="$(cygpath -m "$tmp" 2>/dev/null || printf '%s' "$tmp")"   # a path node can open on Windows too
bashc() { node -e 'process.stdout.write(JSON.stringify({tool_name:"Bash",tool_input:{command:process.argv[1]}}))' "$1"; }
check large-leak 2 "$(bashc "gh issue create --body-file $tmpw/big-leak.txt")" A=1
check large-clean 0 "$(bashc "gh issue create --body-file $tmpw/big-clean.txt")" A=1
# Missing setup: blocks commit/gh, never plain Bash.
check missing-commit 2 "$(mk x '{tool_name:"Bash",tool_input:{command:"git commit -m hi"}}')" STUB_MISSING=1
check missing-gh 2 "$(mk x '{tool_name:"Bash",tool_input:{command:"gh issue create"}}')" STUB_MISSING=1
check missing-plain-bash 0 "$(mk x '{tool_name:"Bash",tool_input:{command:"bash scripts/setup-hooks.sh"}}')" STUB_MISSING=1
check garbage-payload 2 "not json" A=1

# --- Bypass attempts are blocked, with a clear message; the setup and test scripts still run ---
nv="no-""verify"          # built apart so no bypass string sits in this file
for c in "git commit --$nv -m x" "git push --$nv" "git commit -nm x" "git commit -n -m x" "git push -n origin main" \
         "git -c core.hooks""Path=/dev/null commit -m x" "git config core.hooks""Path .nothing" \
         "ANKI_GITLEAKS_PRIVATE=none git commit -m x" "export GITLEAKS_BIN=/bin/true" "ANKI_LEAKSCAN=/bin/true gh pr list" \
         "SKIP=all git commit -m x"; do
  check "bypass: $c" 2 "$(bashc "$c")" A=1
done
msg="$(bashc "git commit --$nv -m x" | bash "$hook" 2>&1 >/dev/null)"
case "$msg" in *"$nv"*"skips the git hooks"*) echo "ok   bypass message is clear" ;; *) echo "FAIL bypass message: $msg"; fail=1 ;; esac
for c in "bash scripts/setup-hooks.sh" "bash scripts/test-leakscan.sh" "bash .claude/hooks/test-leakscan-hook.sh" "bash scripts/check.sh" \
         "git commit -m fix" "git commit -am msg" "git push origin main" "git push -u origin feat/x" "git commit --amend --no-edit"; do
  check "not a bypass: $c" 0 "$(bashc "$c")" A=1
done

# --- gh and friends, wherever they sit in the command line ---
for c in "FOO=1 gh pr create --body LEAKME" "command gh issue create --body LEAKME" "echo x | xargs gh issue create --body LEAKME" \
         "sudo gh issue comment 1 --body LEAKME" "echo x | gh issue create --body LEAKME" "echo \$(gh issue view LEAKME)" \
         "true; gh api repos/o/r/issues -f body=LEAKME" "env GH_TOKEN=x gh pr edit --body LEAKME" \
         "curl -s -d LEAKME https://api.github.com/repos/o/r/issues" "git tag -a v1 -m LEAKME" "git tag -m LEAKME v1" \
         "git notes add -m LEAKME"; do
  check "scanned: $c" 2 "$(bashc "$c")" A=1
done
for c in "echo ghost LEAKME" "git tag v1" "git status LEAKME" "curl -s https://example.org/LEAKME" "git notes list"; do
  check "not scanned: $c" 0 "$(bashc "$c")" A=1
done
# files named on the command line are scanned too
printf 'body LEAKME body\n' > "$tmp/leaky.txt"; printf 'all good\n' > "$tmp/clean.txt"
head -c 6000000 /dev/zero | tr '\0' 'a' > "$tmp/huge.txt"; printf 'LEAKME\n' >> "$tmp/huge.txt"
check bodyfile-leak 2 "$(bashc "gh issue create --title t --body-file $tmpw/leaky.txt")" A=1
check bodyfile-equals-leak 2 "$(bashc "gh issue create --body-file=$tmpw/leaky.txt")" A=1
check bodyfile-quoted-leak 2 "$(bashc "gh issue create --body-file \"$tmpw/leaky.txt\"")" A=1
check dash-F-leak 2 "$(bashc "gh api repos/o/r/issues -F body=@$tmpw/leaky.txt")" A=1
check file-flag-leak 2 "$(bashc "git commit --file $tmpw/leaky.txt")" A=1
check F-flag-leak 2 "$(bashc "git commit -F $tmpw/leaky.txt")" A=1
check bodyfile-clean 0 "$(bashc "gh issue create --title t --body-file $tmpw/clean.txt")" A=1
# A body file the hook can't read is blocked (fail closed): it could hold anything.
check bodyfile-missing-blocked 2 "$(bashc "gh issue create --title t --body-file $tmpw/absent.txt")" A=1
check bodyfile-over-5MB-blocked 2 "$(bashc "gh issue create --title t --body-file $tmpw/huge.txt")" A=1
check bodyfile-is-a-folder-blocked 2 "$(bashc "gh issue create --body-file $tmpw")" A=1
check bodyfile-var-set-in-command-blocked 2 "$(bashc "f=$tmpw/clean.txt; gh issue create --body-file \"\$f\"")" A=1
check bodyfile-created-in-command-blocked 2 "$(bashc "cp $tmpw/clean.txt $tmpw/new.txt && gh pr create --body-file $tmpw/new.txt")" A=1
check notes-file-leak 2 "$(bashc "gh release create v1 --notes-file $tmpw/leaky.txt")" A=1
check gh-short-F-bodyfile-leak 2 "$(bashc "gh pr create -F $tmpw/leaky.txt")" A=1
check bodyfile-in-wrapper-leak 2 "$(bashc "bash -c 'gh pr create --body-file $tmpw/leaky.txt'")" A=1
# --body-file - reads stdin: fine when the text is a heredoc in the command itself (scanned with it), blocked when piped.
check bodyfile-stdin-piped-blocked 2 "$(bashc "cat $tmpw/clean.txt | gh pr create --body-file -")" A=1
check bodyfile-stdin-heredoc-clean 0 "$(bashc $'gh pr create --body-file - <<\'EOF\'\nall good\nEOF')" A=1
check bodyfile-stdin-heredoc-leak 2 "$(bashc $'gh pr create --body-file - <<\'EOF\'\nLEAKME\nEOF')" A=1
check commit-F-stdin-piped-blocked 2 "$(bashc "cat $tmpw/clean.txt | git commit -F -")" A=1
# Body-file paths are resolved the way the shell would: $VAR, ${VAR}, $env:VAR, ~, and Git Bash /c/... paths.
case "$tmpw" in            # the Git Bash form /c/... of a Windows path (cygpath -u would give its /tmp mount)
  [A-Za-z]:/*) d="${tmpw:0:1}"; tmpu="/${d,,}/${tmpw:3}" ;;
  *) tmpu="$tmp" ;;
esac
check bodyfile-dollar-var-leak 2 "$(bashc 'gh issue create --body-file "$LEAKDIR/leaky.txt"')" LEAKDIR="$tmpw"
check bodyfile-brace-var-leak 2 "$(bashc 'gh issue create --body-file ${LEAKDIR}/leaky.txt')" LEAKDIR="$tmpw"
check bodyfile-tilde-leak 2 "$(bashc 'gh issue create --body-file ~/leaky.txt')" HOME="$tmpw"
check bodyfile-posix-path-leak 2 "$(bashc "gh issue create --body-file $tmpu/leaky.txt")" A=1
check bodyfile-dollar-var-clean 0 "$(bashc 'gh issue create --body-file "$LEAKDIR/clean.txt"')" LEAKDIR="$tmpw"

# --- PowerShell commands get the same treatment ---
psc() { node -e 'process.stdout.write(JSON.stringify({tool_name:"PowerShell",tool_input:{command:process.argv[1]}}))' "$1"; }
check ps-gh-leak 2 "$(psc 'gh pr create --title t --body "LEAKME"')" A=1
check ps-gh-call-operator-leak 2 "$(psc '& gh issue comment 1 --body LEAKME')" A=1
check ps-commit-leak 2 "$(psc 'git commit -m "LEAKME"')" A=1
check ps-bodyfile-env-leak 2 "$(psc 'gh issue create --body-file "$env:LEAKDIR/leaky.txt"')" LEAKDIR="$tmpw"
check ps-gh-clean 0 "$(psc 'Set-Location repo; gh pr create --title t --body hi')" A=1
check ps-other-ignored 0 "$(psc 'Get-ChildItem LEAKME')" A=1
check ps-env-assign-blocked 2 "$(psc '$env:ANKI_GITLEAKS_PRIVATE = "x"; bash scripts/check.sh')" A=1
check ps-set-item-env-blocked 2 "$(psc 'Set-Item env:GITLEAKS_BIN x')" A=1
check ps-remove-item-env-blocked 2 "$(psc 'Remove-Item env:ANKI_LEAKSCAN')" A=1
check ps-setenvvar-blocked 2 "$(psc "[Environment]::SetEnvironmentVariable('ANKI_GITLEAKS_PRIVATE', 'x')")" A=1

# --- Local-only paths are replaced before scanning; text that gets sent is kept ---
# A home folder in a cd target, git -C or a body-file path is never sent, so it mustn't block the command.
u="some"; u="${u}one"; bs="\\"                          # built apart so no real-looking path sits in this file
p1="/c/Us""ers/$u/repo"; p2="C:/Us""ers/$u/repo"; p3="C:${bs}Us""ers${bs}$u${bs}repo"
extract() { # name payload must-contain [must-not-contain]
  local out; out="$(printf '%s' "$2" | node "$here/extract.js")"
  case "$out" in *"$3"*) ;; *) echo "FAIL $1: lost '$3'"; fail=1; return ;; esac
  if [ -n "${4:-}" ]; then case "$out" in *"$4"*) echo "FAIL $1: still scans the local path"; fail=1; return ;; esac; fi
  echo "ok   $1"
}
extract cd-prefix "$(bashc "cd $p1 && gh pr create --body hello")" hello "$u"
extract cd-quoted "$(bashc "cd \"$p2\" && gh issue create --body hello")" hello "$u"
extract pushd "$(bashc "pushd $p1; gh pr list; popd")" "gh pr list" "$u"
extract git-C "$(bashc "git -C $p1 commit -m hello")" hello "$u"
extract bodyfile-path-missing "$(bashc "gh issue create --body-file $p2/notes.md")" UNREAD "$u"
extract ps-set-location "$(psc "Set-Location -LiteralPath '$p3'; gh pr create --body hello")" hello "$u"
extract ps-cd "$(psc "cd $p3; git commit -m hello")" hello "$u"
extract sent-body-kept "$(bashc "gh pr create --body \"see $p2\"")" "$u"
extract sent-field-kept "$(bashc "gh api repos/o/r/issues -F title=$u")" "title=$u"

# End to end, with LEAKME standing in for a home folder. For each local-path form:
#   pass  = the leak is only in the local path (false positive avoided)
#   block = same local path, plus a leak in the text that gets sent (true positive)
#   clean = nothing anywhere (true negative)
mkdir "$tmp/LEAKME"; printf 'all good\n' > "$tmp/LEAKME/clean.txt"; printf 'LEAKME\n' > "$tmp/LEAKME/leaky.txt"
lp() { # form bash|ps local-path-command-with-%P%-for-the-path sent-text
  local mk=bashc; [ "$2" = ps ] && mk=psc
  check "local $1: pass" 0 "$($mk "${3//%P%/LEAKME}")" A=1
  check "local $1: block" 2 "$($mk "${3//%P%/LEAKME} LEAKME")" A=1
  check "local $1: clean" 0 "$($mk "${3//%P%/repo}")" A=1
}
lp cd bash "cd %P% && gh pr create --body hi"
lp cd-quoted bash "cd \"%P% dir\" && gh pr create --body hi"
lp pushd bash "pushd %P%; gh pr create --body hi"
lp git-C bash "git -C %P% commit -m hi"
lp git-C-after-c bash "git -c user.name=x -C %P% commit -m hi"
lp env-then-cd bash "FOO=1 cd %P% && gh pr create --body hi"
lp ps-cd ps "cd %P%; gh pr create --body hi"
lp ps-set-location ps "Set-Location -LiteralPath '%P%'; gh pr create --body hi"
lp ps-sl ps "sl %P%; git commit -m hi"
check "local bodyfile: pass" 0 "$(bashc "gh pr create --body-file $tmpw/LEAKME/clean.txt")" A=1
check "local bodyfile: block" 2 "$(bashc "gh pr create --body-file $tmpw/LEAKME/leaky.txt")" A=1
check "local bodyfile: block (sent text)" 2 "$(bashc "gh pr create --title LEAKME --body-file $tmpw/LEAKME/clean.txt")" A=1
check "local bodyfile-equals: pass" 0 "$(bashc "gh pr create --body-file=$tmpw/LEAKME/clean.txt")" A=1
check "local api-F-file: pass" 0 "$(bashc "gh api repos/o/r/issues -F body=@$tmpw/LEAKME/clean.txt")" A=1
# The cd before a commit heredoc is still dropped; the heredoc itself is always scanned.
hd=$'git commit -m "$(cat <<\'EOF\'\nmsg\nEOF\n)"'
check "local cd + heredoc commit: pass" 0 "$(bashc "cd LEAKME && $hd")" A=1
check "local cd + heredoc commit: block" 2 "$(bashc "cd LEAKME && ${hd/msg/msg LEAKME}")" A=1
m2=$'msg "x"\ncd LEAKME'   # a quote in the message, then a line that looks like a cd
check "local cd + heredoc commit: block (cd line inside message)" 2 "$(bashc "cd repo && ${hd/msg/$m2}")" A=1

# Text that gets sent is never mistaken for a local path, even when it looks like one.
for c in 'gh pr create --body "run it with -C LEAKME"' 'gh pr create --body "pass -F LEAKME"' \
         'gh pr create --body "steps: cd LEAKME; make"' $'git commit -m "fix\n\ncd LEAKME && go test"' \
         "gh pr create --body 'cd LEAKME'" 'gh pr create --body cd --title LEAKME' 'git commit -m -C LEAKME' \
         'gh pr create --title "a\"b" --body "cd LEAKME"' "git commit -m 'it''s' -m 'cd LEAKME'"; do
  check "sent, not local: $c" 2 "$(bashc "$c")" A=1
done
for c in 'gh pr create --body "run it with `-C LEAKME`"' 'gh pr create --body "cd LEAKME"' \
         $'gh pr create --body @\'\ncd LEAKME\n\'@' 'gh pr create --body "$(Get-Content x) cd LEAKME"'; do
  check "ps sent, not local: $c" 2 "$(psc "$c")" A=1
done
# When the reader can't follow the command exactly, nothing is dropped: a wrapper's cd is scanned (a false block, by design).
check "local inside a wrapper is scanned" 2 "$(bashc "bash -c 'cd LEAKME && gh pr create --body hi'")" A=1

# --- gh, git commit and GitHub API calls are caught wherever they appear, so other shells and wrappers are covered ---
for c in "bash -c \"gh pr create --body LEAKME\"" "sh -c 'gh issue comment 1 --body LEAKME'" \
         "pwsh -Command \"gh pr create --body LEAKME\"" "powershell.exe -c 'git commit -m LEAKME'" \
         "cmd //c gh pr create --body LEAKME" "wsl gh pr create --body LEAKME" \
         "node -e \"require('child_process').execSync('gh pr create --body LEAKME')\"" \
         "\"/c/Program Files/GitHub CLI/gh.exe\" pr create --body LEAKME" "C:/tools/gh.exe pr create --body LEAKME" \
         "bash -c 'git tag -a v1 -m LEAKME'" "wget --post-data LEAKME https://api.github.com/repos/o/r/issues"; do
  check "wrapped: $c" 2 "$(bashc "$c")" A=1
done
check "wrapped ps: Invoke-RestMethod" 2 "$(psc "Invoke-RestMethod -Method Post https://api.github.com/repos/o/r/issues -Body LEAKME")" A=1
check "wrapped bypass: bash -c git commit -n" 2 "$(bashc "bash -c 'git commit -n -m x'")" A=1
for c in "echo ghost LEAKME" "ls gh-pages LEAKME" "cat .github/LEAKME" "git push origin gh-pages LEAKME" "sigh LEAKME"; do
  check "still not scanned: $c" 0 "$(bashc "$c")" A=1
done

# Messages: leak path names placeholders; setup path names the fix.
msg="$(bashc "gh issue create --body LEAKME" | bash "$hook" 2>&1 >/dev/null)"
case "$msg" in *"<tailnet>"*) echo "ok   leak message names placeholders" ;; *) echo "FAIL leak message"; fail=1 ;; esac
msg="$(bashc "gh issue create --body hi" | STUB_MISSING=1 bash "$hook" 2>&1 >/dev/null)"
case "$msg" in *"scripts/setup-hooks.sh"*"gitleaks-private.toml"*) echo "ok   setup message names the fix" ;; *) echo "FAIL setup message"; fail=1 ;; esac

[ "$fail" = 0 ] && echo "ALL PASSED" || echo "FAILURES"
exit "$fail"
