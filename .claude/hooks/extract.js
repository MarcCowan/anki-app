// Reads a Claude Code PreToolUse payload on stdin; prints a one-line verdict, then the text to scan.
//   SKIP          nothing to scan
//   SCAN          followed by the text to scan
//   BLOCK <why>   a command that would bypass the guardrails (blocked without scanning)
//   UNREAD        a file whose contents would be sent can't be read now (blocked: it could hold anything)
//   ERR           payload unparseable
// Only Bash and PowerShell commands are scanned; files written by Write/Edit are caught by the pre-commit hook.
// gh, git commit/tag/notes and GitHub API calls are found anywhere in the command, so `bash -c "gh ..."`, `pwsh
// -Command`, `cmd /c`, `node -e` and a full path to gh.exe are covered too. Local-only paths are dropped before
// scanning (localpaths.js). When unsure, scan: a false block costs a retry, a miss publishes the value.
// No dependencies: node is already required by web/.
const fs = require("fs");
const os = require("os");
const { withoutLocalPaths } = require("./localpaths.js");
const chunks = [];
process.stdin.on("data", (c) => chunks.push(c));
process.stdin.on("end", () => {
  let p;
  try {
    p = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    process.stdout.write("ERR\n");
    return;
  }
  const t = p.tool_name;
  const i = p.tool_input || {};
  const s = (v) => (typeof v === "string" ? v : "");
  let parts = null;
  if (t === "Bash" || t === "PowerShell") {
    const c = s(i.command);
    const why = bypass(c);
    if (why) {
      process.stdout.write("BLOCK " + why + "\n");
      return;
    }
    if (scannable(c)) {
      const files = referencedFiles(c);
      if (files === null) {
        process.stdout.write("UNREAD\n");
        return;
      }
      parts = [withoutLocalPaths(c, t === "PowerShell"), ...files];
    }
  }
  if (parts === null) {
    process.stdout.write("SKIP\n");
    return;
  }
  process.stdout.write("SCAN\n" + parts.join("\n") + "\n");
});

// A program name, anywhere: not part of a longer word or file name (ghost, gh-pages, .github are not gh).
const W = "(?:^|[^\\w.-])";
const GIT = W + "git(?:\\.exe)?[\"']?\\s[^;&|\\n]*";
const GH = new RegExp(W + "gh(?:\\.exe)?(?=[\\s\"'`);]|$)");
const COMMIT = new RegExp(GIT + "\\bcommit\\b");
const TAG = new RegExp(GIT + "\\btag\\b[^;&|\\n]*\\s(?:-[A-Za-z]*[maF]\\b|--message|--annotate|--file)");
const NOTES = new RegExp(GIT + "\\bnotes\\b");
const API = /api\.github\.com/i; // curl, wget, Invoke-RestMethod, fetch: whatever the client
function scannable(c) {
  return [GH, COMMIT, TAG, NOTES, API].some((r) => r.test(c));
}

// Commands that would switch the guardrails off. Reported as a block; the text is never scanned or printed.
const GITN = (verb) => new RegExp(GIT + "\\b" + verb + "\\b[^;&|\\n]*\\s-[A-Za-z]*n[A-Za-z]*(?:[\\s\"']|$)");
const NO_N = [GITN("commit"), GITN("push")];
function bypass(c) {
  if (/--no-verify/.test(c)) return "a command with --no-verify skips the git hooks";
  if (NO_N.some((r) => r.test(c))) return "git commit or push with -n skips the git hooks";
  if (/core\.hooksPath/i.test(c)) return "changing core.hooksPath turns the git hooks off";
  if (/\b(ANKI_GITLEAKS_PRIVATE|GITLEAKS_BIN|ANKI_LEAKSCAN)\s*=/.test(c)) return "overriding the leak scanner settings";
  if (/\bSKIP\s*=/.test(c)) return "SKIP= skips hooks";
  // PowerShell's other ways to set or clear them: Set-Item/Remove-Item env:X, [Environment]::SetEnvironmentVariable('X', ...)
  if (/\benv:(ANKI_GITLEAKS_PRIVATE|GITLEAKS_BIN|ANKI_LEAKSCAN|SKIP)\b/i.test(c)) return "overriding the leak scanner settings";
  if (/SetEnvironmentVariable\s*\(\s*["'](ANKI_GITLEAKS_PRIVATE|GITLEAKS_BIN|ANKI_LEAKSCAN|SKIP)["']/i.test(c))
    return "overriding the leak scanner settings";
  return "";
}

// Expands what a shell would before reading the file: $VAR, ${VAR}, $env:VAR, a leading ~, and Git Bash /c/... paths
// on Windows. null when it can't: an unset variable (perhaps set earlier in the same command) or a $(...).
function resolvePath(f) {
  let ok = true;
  const env = (n) => {
    const v = process.env[n];
    if (!v) ok = false;
    return v ?? "";
  };
  f = f.replace(/\$env:([A-Za-z_]\w*)/gi, (_m, n) => env(n));
  f = f.replace(/\$\{([A-Za-z_]\w*)\}|\$([A-Za-z_]\w*)/g, (_m, a, b) => env(a || b));
  if (!ok || f.includes("$")) return null;
  if (f === "~" || f.startsWith("~/") || f.startsWith("~\\")) f = (process.env.HOME || os.homedir()) + f.slice(1);
  if (process.platform === "win32") {
    const m = /^\/([a-zA-Z])\/(.*)$/.exec(f);
    if (m) f = m[1] + ":/" + m[2];
  }
  return f;
}

// Contents of the files that go into the command (--body-file, --notes-file, --file, -F, -F key=@file), found
// anywhere in it, wrappers included. null when one can't be read now: missing (perhaps created earlier in the same
// command), not a file, over 5 MB, or stdin (`-`) that isn't a heredoc or here-string in the command itself.
function referencedFiles(c) {
  const out = [];
  const re = /(?:^|[\s"'])(?:--body-file|--notes-file|--file|-F)(?:\s+|=)("[^"]*"|'[^']*'|[^\s"';&|)]+)/g;
  let m;
  while ((m = re.exec(c)) !== null) {
    let f = m[1].replace(/^(["'])(.*)\1$/, "$2");
    const at = f.indexOf("=@");
    if (at >= 0) f = f.slice(at + 2);
    else if (f.includes("=")) continue; // gh api -F key=value: a field, scanned with the command
    if (f === "-") {
      if (/<<|@['"]/.test(c)) continue; // the text is in the command, so it's scanned with it
      return null;
    }
    const path = resolvePath(f);
    if (path === null) return null;
    try {
      const st = fs.statSync(path);
      if (!st.isFile() || st.size > 5 * 1024 * 1024) return null;
      out.push(fs.readFileSync(path, "utf8"));
    } catch {
      return null;
    }
  }
  return out;
}
