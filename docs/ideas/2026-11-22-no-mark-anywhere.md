# Idea: no "Mark" anywhere

Status: **draft**, awaiting review; not scheduled.

## Why

An agent wrote a rule into CLAUDE.md that ended "...go through it with Marc... Mark it reviewed only once he has."
Marc read it and asked, fairly, "Who is 'Mark'?" Nobody. It was the verb, sitting at the start of a sentence right
after his name. But the owner is Marc, with a c, and the repo already has a scanner that stops things it does not
like. Could it stop this too? Marc has not decided *what* should happen when the word turns up, only that something
should.

## Grounding (run on `main`, 2026-11-22)

- `git grep -nw "Mark"` finds **0** whole-word, case-sensitive hits. The tree is clean. The incident was caught by a
  human before it was merged, which is the argument for a gentle check: this is rare, so it must be cheap.
- `git grep -nw "Marc"` finds 66 lines, so the name is spelled right far more often than not.
- `git grep -ni markdown` finds 125 lines, because the renderer is markdown-it and the docs are Markdown. A search
  without word boundaries and case would drown in them. Other near misses in the tree: `MarketData` in card data,
  the shell function `mark()` in a deploy script, `apt-mark`, and "a simple geometric mark" in a plan.

## What the check has to get right

| Text | Should it fire? | How |
|---|---|---|
| `Markdown`, `markdown-it`, `Markov`, `Bookmarked`, `bookmark`, `remark`, `trademark`, `MarketData` | No | Whole word, case-sensitive: `\bMark\b`. None of these is the whole word. |
| lowercase `mark` (verb, noun, `apt-mark`) | No | Case-sensitive. Real names are capitalised; lowercase is almost always the word. |
| `markRead`, a `Mark` type, `Mark()` | Usually no | Skip code files (`*.go`, `*.ts`, `*.tsx`, `*.sh`) and scan prose (`*.md`, commit messages, PR text). A bare `Mark` type is legitimate code. |
| "Mark it reviewed" at the start of a sentence (the original incident) | Ambiguous | A regex cannot tell the verb from the name. Two cheap tells: the verb is followed by an object word (`it`, `them`, `this`, `the`, `as`, `each`, `all`), and a name is followed by `'s`, a punctuation mark or a verb. Use the first as a skip list. Otherwise accept the false positive and make it harmless (see below). |
| The doc you are reading | Must be allowed | Path allowlist for `docs/ideas/2026-11-22-no-mark-anywhere.md`, plus an inline `no-mark: ok` marker for a one-off. |
| A real person named Mark, in a quote or a `Co-authored-by:` trailer | Must be allowed | Skip lines containing `Co-authored-by:` or `no-mark: ok`. Quoting someone is what the marker is for. |

## Options

1. **A gitleaks custom rule.** Add `imposter-mark` to `.gitleaks.toml` with regex `\bMark\b` and the description
   "It's Marc, with a c." The scanner already runs in pre-commit, commit-msg, pre-push, the Claude Code hook and CI, so
   it is one stanza of config with five enforcement points and the path allowlist is built in. The irony is that the
   leak guardrails would now defend against the owner's name being misspelled. The cost: it **blocks** commits over a
   word, including the verb-after-name false positive that started all this, and a blocked agent has to reword. That
   is heavy for a typo.
2. **A CI annotation that never fails.** A workflow step greps the lines a PR adds and prints
   `::warning file=...,line=...::It's Marc, with a c.` for each hit. GitHub shows these inline on the diff. No token,
   no new permissions, nothing to block, and a false positive costs a glance.
3. **A bot comment on PRs.** A small workflow comments "It's Marc with a c" when the diff adds a `Mark`. Funnier, but it
   needs `pull-requests: write`, comments again on every push unless it de-duplicates, and it is noise for a
   single-author repo where the annotation in option 2 already shows up in the same place.
4. **Auto-correct in a pre-commit hook.** Rewrite `Mark` to `Marc` on commit. No: it would "fix" the verb
   ("Marc it reviewed"), a real Mark in a quote or trailer, and code identifiers, and it edits the working tree behind
   the author's back. A hook that changes content has to be right every time, and this one cannot be.
5. **Fix it where it starts: the writer.** Most of the text here is written by an agent. Add a one-line style rule to
   CLAUDE.md ("do not start a sentence with the verb 'mark' right after Marc; write 'flag it reviewed'") and let
   option 2 catch any slip. The incident was a style problem, and a style rule prevents it without a check.

## Recommendation

**Option 2, with the skip list above, plus the one-line rule from option 5.** Annotation, never a failure: it is
visible exactly where Marc reads the diff, it is funny once and silent otherwise, and its worst case is a harmless
extra line. Gitleaks (option 1) is the right tool for secrets but the wrong one for a name, and the same regex can be
promoted to a rule later if a "Mark" ever slips through. Implementation sketch: a `scripts/no-mark.sh` that takes a
diff, plus a test in the style of `scripts/test-leakscan.sh` with one case per row of the table above (the cheap way
to keep the edge cases honest), wired into CI as a non-blocking step. It is not part of `scripts/check.sh`, since
that is the done bar and this must never fail it.
