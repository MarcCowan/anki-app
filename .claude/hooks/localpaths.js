// Replaces paths that stay on this machine with placeholders before a command is scanned, so a home folder in a
// `cd` target is not reported as a leak. Only words this reader has followed exactly are touched: it splits the
// command the way the shell would and stops at the first thing it can't follow ($(...), backticks, heredocs,
// PowerShell here-strings, an unclosed quote). Everything from there on is scanned as is. When unsure, scan.
//
// Dropped: the target of cd/pushd/chdir/Set-Location/Push-Location/sl at the start of a command, git's -C <dir>
// (before the subcommand), and the path after --body-file/--notes-file/--file/-F in a gh, git or curl command
// (that file's contents are scanned separately). Quoted text is one word, so a message is never mistaken for a path.

const CD = new Set(["cd", "pushd", "chdir", "set-location", "push-location", "sl"]);
const SENDERS = new Set(["gh", "git", "curl"]);
const FILE_FLAG = /^(--body-file|--notes-file|--file|-F)(?:=(.*))?$/;
const VALUE_FLAG = new Set(["-m", "--message", "-b", "--body", "-t", "--title", "-f", "--raw-field", "--notes"]);
const OPS = " \t\r\n;()&|";

// Words of c with their spans, up to `end`: the position where the reader stopped (c.length when it read it all).
// ps: PowerShell rules (backslash is an ordinary character there; the backtick is its escape, so it stops).
function words(c, ps) {
  const out = [];
  let i = 0;
  while (i < c.length) {
    const ch = c[i];
    if (ch === " " || ch === "\t" || ch === "\r") {
      i++;
      continue;
    }
    if (OPS.includes(ch)) {
      const e = (ch === "&" || ch === "|") && c[i + 1] === ch ? i + 2 : i + 1;
      out.push({ op: true, s: i, e });
      i = e;
      continue;
    }
    const s = i;
    let v = "";
    let stop = false;
    while (i < c.length && !OPS.includes(c[i])) {
      const d = c[i];
      const n = c[i + 1];
      if (d === "`" || (d === "$" && (n === "(" || n === "'")) || (d === "<" && n === "<") || (d === "@" && (n === "'" || n === '"'))) {
        stop = true;
        break;
      }
      if (d === "'") {
        const j = c.indexOf("'", i + 1);
        if (j < 0) {
          stop = true;
          break;
        }
        v += c.slice(i + 1, j);
        i = j + 1;
      } else if (d === '"') {
        let j = i + 1;
        let w = "";
        for (; j < c.length && c[j] !== '"'; j++) {
          if (c[j] === "`" || (c[j] === "$" && c[j + 1] === "(")) break;
          if (c[j] === "\\" && !ps) j++;
          w += c[j] ?? "";
        }
        if (c[j] !== '"') {
          stop = true;
          break;
        }
        v += w;
        i = j + 1;
      } else if (d === "\\" && !ps) {
        v += c[i + 1] ?? "";
        i += 2;
      } else {
        v += d;
        i++;
      }
    }
    if (stop) return { words: out, end: s };
    out.push({ op: false, s, e: i, v });
  }
  return { words: out, end: c.length };
}

const base = (v) => v.replace(/^.*[\\/]/, "").replace(/\.exe$/i, "").toLowerCase();
const isWord = (t) => t && !t.op;

function withoutLocalPaths(c, ps) {
  const { words: w } = words(c, ps);
  const drop = []; // [start, end, replacement]
  let cmd = null; // base name of the current command, null at the start of one
  for (let k = 0; k < w.length; k++) {
    const t = w[k];
    if (t.op) {
      cmd = null;
      continue;
    }
    if (cmd === null) {
      if (/^[A-Za-z_]\w*=/.test(t.v)) continue; // FOO=1 before the command
      cmd = base(t.v);
      if (CD.has(cmd)) {
        let j = k + 1;
        if (isWord(w[j]) && /^-(literal)?path$/i.test(w[j].v)) j++;
        if (isWord(w[j])) drop.push([w[j].s, w[j].e, "<dir>"]);
        k = j;
      } else if (cmd === "git") {
        // global options before the subcommand: -C <dir> is dropped, -c <key=value> is skipped
        for (let j = k + 1; isWord(w[j]) && w[j].v.startsWith("-"); j++) {
          if (w[j].v === "-C" && isWord(w[j + 1])) drop.push([w[j + 1].s, w[j + 1].e, "<dir>"]);
          if (w[j].v === "-C" || w[j].v === "-c") j++;
          k = j;
        }
      }
      continue;
    }
    if (!SENDERS.has(cmd)) continue;
    if (VALUE_FLAG.has(t.v)) {
      if (isWord(w[k + 1])) k++; // its value is sent text, never a flag
      continue;
    }
    const m = FILE_FLAG.exec(t.v);
    if (!m) continue;
    const joined = m[2] !== undefined;
    const f = joined ? m[2] : isWord(w[k + 1]) ? w[k + 1].v : null;
    if (f === null) continue;
    const at = f.indexOf("=@");
    if (at < 0 && f.includes("=")) continue; // gh api -F key=value: a field that gets sent
    const repl = at >= 0 ? f.slice(0, at + 2) + "<file>" : "<file>";
    if (joined) drop.push([t.s, t.e, m[1] + "=" + repl]);
    else drop.push([w[k + 1].s, w[k + 1].e, repl]);
    if (!joined) k++;
  }
  let out = c;
  for (const [s, e, r] of drop.sort((a, b) => b[0] - a[0])) out = out.slice(0, s) + r + out.slice(e);
  return out;
}

module.exports = { withoutLocalPaths, words };
