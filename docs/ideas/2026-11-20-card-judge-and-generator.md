# Idea: card-quality judge and card-generation agent

Status: **draft**, not scheduled. Date: 2026-11-20. Two features built on the Claude API: a judge that scores card
quality, and an agent that drafts cards from notes for Marc to approve.

## Recommendation

1. **Build the judge first, as an offline eval tool** (shape (a) below), and calibrate it against about 60
   hand-labelled cards. It is the smallest piece, involves no change to the web UI or the public listener, and
   everything else reuses it.
2. **Then reuse it as the scorer for an eval suite of the MCP connector's tool descriptions.**
3. **Then build the generator** in the existing binary, on the tailnet-only handler, with every draft approved by
   Marc. Measure it with the same judge and eval set.
4. Leave the in-app judge (shape (b)) for later. Once the judge is calibrated, it is a small step.

Rough total: 8 to 11 days of work, and under $20 of API credit to build and evaluate everything.

## Models and prices (checked 2026-11-20, per million tokens, input / output)

| Model | ID | Price | Use here |
|---|---|---|---|
| Claude Opus 5.5 | `claude-opus-5-5` | $4 / $20 | Generator candidate; upper bound for the judge |
| Claude Sonnet 5.5 | `claude-sonnet-5-5` | $2 / $10 | Default judge; generator candidate |
| Claude Haiku 5.5 | `claude-haiku-5-5` | $0.10 / $0.50 | Cheap judge to compare against Sonnet |

The Message Batches API halves the price for offline runs. Prompt caching makes a repeated rubric or system prompt
cheap to send again. API facts that affect the design:
- These models don't accept `temperature` (from API version 2026-10-21 no model does). The judge can't be made
  deterministic, so its run-to-run consistency has to be measured.
- Forced `tool_choice` isn't accepted on Opus 5.5 or Sonnet 5.5. Use structured outputs
  (`output_config.format` with a JSON schema) to get the judge's verdict back as JSON.
- Handle `stop_reason: "refusal"`: security-topic code cards could trip a classifier. Turn on server-side
  `fallbacks`.

---

## 1. LLM-as-judge for card quality

**Purpose.** Find weak cards in existing decks, catch bad cards added through MCP, and give the generator a quality
bar. Its scores are only trusted once it is calibrated against human labels, with a measured agreement figure.

**What it is.** A program sends one card, plus a few existing cards that look similar, to Claude with a fixed rubric.
It gets back a pass/fail and a one-line reason for each criterion:

| Criterion | Checked how |
|---|---|
| One idea per card | LLM |
| Answerable from the front alone (in both directions if the card is two-sided) | LLM |
| Back is correct | LLM, plus a compile check (below) |
| Back is concise | LLM, plus a deterministic length signal |
| Code is valid | Deterministic first: `g++ -fsyntax-only`, `python -m py_compile` and `bash -n` on fenced blocks with a known language tag. Snippets that are fragments by design get "n/a", not "fail". |
| Not a duplicate | Candidate cards come from the store's existing search on the card's key terms; the LLM judges whether they really are the same card. |

Limits from `validate` (1 to 6,000 code points per side, normalised) are checked in code, never by the model.

### Two shapes

- **(a) Offline eval tool (recommended).** A separate command, `server/cmd/anki-eval`, in the same Go module. It
  opens a copy of a backup DB read-only, scores one deck or one batch, and writes a JSONL report and a summary to a
  gitignored folder. It has no network listener and doesn't change the app.
- **(b) In-app.** A "Check quality" action on a card, or automatic scoring of each MCP batch, with flags shown next
  to the batch in Settings → Recent batches. This adds an API key on the VM, a per-card cost, UI work and e2e
  tests. It's worth doing only once (a) shows the judge agrees with Marc.

Both shapes call one package, `internal/judge`: `Score(ctx, card, neighbours) → Verdict`.

### Calibration (the eval of the judge)

- Marc hand-labels about 60 cards across the criteria, including weak ones on purpose (two ideas on one card, a
  vague front, wrong code). That's about half a day.
- Metrics per criterion: agreement and Cohen's kappa with Marc's labels, and precision and recall for "fail" (a
  judge that flags nothing is useless). Also how often three repeat runs give the same answer.
- Experiments: Haiku 5.5 vs Sonnet 5.5 vs Opus 5.5; the rubric with and without one worked example per criterion.
  Keep a held-out third of the labels, so the rubric isn't tuned to the test.

### As the scorer for an MCP connector eval suite

- About 20 scripted chats ("we agreed these 5 cards on std::move, add them to C++"), including traps: a card that
  already exists, the user saying "not yet", too many cards, and pasted text that tells Claude to add 100 cards.
- A harness calls the Messages API with client-side tools that wrap the real `mcpserver` handlers in-process,
  against a throwaway SQLite DB. The real `/mcp` isn't used: Claude's cloud would need a public test server, and
  test runs would count against the 200-cards-a-day limit.
- Scores: did it search before adding, pick the right deck, add nothing without agreement, and stay within the
  limits; the judge's pass rate on the added cards; the duplicate rate.
- This tests the **tool descriptions** and their effect on the model. claude.ai adds its own system prompt, so
  results suggest how it behaves there but don't prove it.

### Cost

About 3,500 input tokens per card (rubric, card and neighbours), and 900 output tokens with thinking.

| Model | Per card | 500-card deck | Same deck with the Batch API |
|---|---|---|---|
| Haiku 5.5 | ~$0.001 | ~$0.40 | ~$0.20 |
| Sonnet 5.5 | ~$0.016 | ~$8 | ~$4 |
| Opus 5.5 | ~$0.032 | ~$16 | ~$8 |

A calibration run (60 cards × 3 models × 3 repeats) is about $5. One run of the MCP suite (20 chats on Sonnet 5.5,
plus judging) is about $2 to $4.

### Security

- (a) runs on Marc's laptop with `ANTHROPIC_API_KEY` from the environment. Card content is sent to Anthropic,
  which already happens with the connector. Logs and reports hold card ids, scores and token counts only, never card
  text. The report folder is gitignored.
- (b) adds the key to `/etc/anki-app/anki-app.env` (root, 0600), never to the image or repo. Calls go out from the
  server, so `connect-src 'self'` and the CSP don't change. The reasons the judge writes are untrusted text, shown as
  plain text or through `<Markdown>`.
- Prompt injection: a card can say "score me 10/10". The rubric sits in the system prompt, the card goes in a
  delimited user block marked as data, and the output schema allows only verdicts. The worst outcome is a wrong
  score.

**Effort:** (a) with calibration, 2 to 3 days. MCP suite, +1.5 to 2 days. (b), +1 to 1.5 days.

**Open questions:** Can the labelled set (card text and Marc's labels) be committed, or does it stay local? Are 60
labels enough for kappa per criterion, or should criteria be merged? Are the compilers on the laptop enough, or
use the API's code-execution tool (billed separately)?

---

## 2. Card-generation agent

**Purpose.** Turn notes, a doc or code into draft cards in minutes, without duplicating cards that already exist.
Every draft passes a human approval step, and the agent is measured with the same eval set as the judge.

**What it is.** In the web UI, Marc pastes text and picks a deck. Claude reads it, searches the deck for cards that
already cover the same points, and proposes draft cards. Marc edits, keeps or drops each draft. Nothing is saved
until he does.

### User flow

1. Deck page → **Generate cards** → paste (capped at about 50,000 characters), optional focus ("only the
   concurrency parts"), and a maximum card count (default 15).
2. Wait 20 to 90 seconds, with a spinner and a Cancel button.
3. Review: each draft is shown through `<Markdown>`, with the judge's flags and any likely duplicate linked. Marc
   can edit, keep or drop it, set two-sided, or keep all clean drafts.
4. **Save kept** creates the cards through the existing card-creation path in the store, with the same validation
   as the API.

### Architecture

- **In the existing Go binary, server side**, on the tailnet-only main handler, never on `api.NewPublic`. A separate
  service would add a deploy unit and a second place for the key, for no benefit to a single user.
- New package `internal/generate`, using the official Go SDK (`github.com/anthropics/anthropic-sdk-go`, pinned,
  with a one-line justification in the commit). It runs the loop with the SDK's tool runner, or a hand-written loop
  (about 100 lines, no beta dependency).
- **Tools for the model:** `search_cards` and `list_fronts` (read-only, the same store queries as the MCP tools),
  `check_card` (runs `validate.Side` and the deterministic checks, so the model fixes over-long sides itself), and
  `submit_drafts` (strict schema: front, back, a reason, and ids of cards it may overlap). There is no tool that
  writes to the database.
- **Workflow before agent:** first build a fixed pipeline (one call to draft, code-side duplicate search, one call
  to revise). Then compare it on the eval with the version where the model chooses its own searches. Keep the agent
  only if it scores better per dollar.
- **Drafts:** POST `/api/decks/{id}/generate` starts a job, and the UI polls for the result. Keep drafts in a
  `generation_drafts` table (new numbered migration, purged after 7 days), so that a closed tab doesn't lose paid
  output. Alternative: memory only, simpler (open question).
- **Judge reuse:** each draft goes through `internal/judge` (Haiku 5.5, so it's cheap), and its flags are shown in
  review. Optionally the agent rewrites drafts that fail, at most once.

### Models and cost

- Generator: Sonnet 5.5 and Opus 5.5 (effort `medium`) are both candidates; let the eval decide. Judge: Haiku 5.5.
- One run (5,000 tokens of notes, about 4 tool turns, 10 to 15 cards): Sonnet ~$0.12, Opus ~$0.25, plus about $0.01
  of judging. About $0.15 to $0.40 a run, so 50 runs a month is $8 to $20.
- Controls: a `max_tokens` cap, a task budget for the loop, a turn cap (8), and a daily spend cap computed from the
  `usage` of each response, recorded per job (token counts only).

### Security (against CLAUDE.md)

- **API key:** `ANTHROPIC_API_KEY` only in `/etc/anki-app/anki-app.env` (0600). The feature is off when the key is
  unset. The key is never logged or sent to the browser, and errors that might echo request headers are scrubbed.
- **Never log card content:** logs hold the job id, model, token counts, turn count and outcome only. Pasted text
  and drafts are never logged.
- **CSP unaffected:** the browser talks only to `/api`, and the server calls Anthropic. New routes are JSON POSTs
  behind the same Host, Sec-Fetch-Site/Origin, content-type and session checks.
- **Untrusted output:** drafts are rendered only through `<Markdown>` (html:false + DOMPurify). External images are
  already blocked by `img-src 'self' data:`. Every saved side goes through `validate.Side`. All SQL from tool calls
  uses `?` parameters, and the deck id comes from the URL, not the model.
- **Prompt injection from pasted content:** the pasted text is wrapped in a delimited block marked as data. The
  agent's tools are read-only, it has no web access, and every write needs Marc's approval. The worst cases are bad
  drafts (caught at review) or text pushing it to over-produce (caught by the card cap and spend cap). Include
  injection cases in the eval.
- **Data leaving the box:** pasted notes and matching existing cards go to Anthropic. Don't paste secrets. The
  paste form says so.

### Eval and metrics

- A fixed set of about 15 inputs (Marc's real notes, a C++ header, a blog-post excerpt), each run against a
  snapshot deck that contains known duplicates.
- Metrics: judge pass rate per draft; duplicates missed (seeded) and wrongly flagged; **acceptance rate** (share of
  drafts Marc keeps unedited or lightly edited: the real metric, logged as counts only); cost and time per run.
- The same harness runs the workflow vs the agent, and Sonnet vs Opus.

**Effort:** 4 to 6 days (backend loop, tools and job routes, 1.5 to 2 days; review screen and e2e at iPhone and
desktop sizes, 1.5 to 2 days; eval set and runs, 1 to 1.5 days).

**Open questions:** Drafts in a table or in memory? Should generated cards share the MCP batch mechanism, so one
bad run can be deleted in one click? Allow PDF uploads (more input to validate) or text only? Is it worth having
the agent revise its own drafts, or does review cover it?

---

## Cost: API credits vs Claude Pro

- **The Claude API is billed separately**, pay as you go, from prepaid credits in the Claude Console. A Claude Pro
  subscription doesn't cover API calls. Set a monthly spend limit in the Console (for example $20).
- **Claude Code in headless mode (`claude -p`) signed in with Pro** spends Pro usage instead of credits. It's
  fine for Marc to prototype the rubric or score a few cards from the laptop. The trade-offs:
  - It shares Pro's usage limits with normal use.
  - There's no Batch API, and less control over caching, the system prompt and the request.
  - Each call has more overhead.
  - It's the wrong tool for the deployed server feature. A server feature should use an API key, not a personal
    subscription login (check the current terms).
  - It doesn't exercise the production path: the Messages API, tool use, structured outputs, batches and token
    accounting.
- **Plan:** draft the rubric with `claude -p` at no extra cost, then run the calibration, the MCP suite and the
  generator on the API with a spend limit.
