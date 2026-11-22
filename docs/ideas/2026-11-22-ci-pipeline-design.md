# Idea: a deliberate CI/CD pipeline

Status: **draft**, awaiting review; not scheduled.

## Why

CI grew one check at a time, alongside the features. Every check runs in a single `check` job, which runs
`scripts/check.sh` from start to finish. That job is by far the slowest part of every run, and the `image`
job (the deploy) has to wait for it. The repo is about to go public, so the pipeline should be easy for a
reader to follow and should give clear, fast feedback.

## 1. Rename "done bar"

"Done bar" is a term made up for this project, and newcomers won't recognise it. Replace it everywhere:
CLAUDE.md, spec §9, the `check.sh` header, the workflow step name, and agent and memory notes.
Some candidates:

- **full check**: plain, and it matches `check.sh`
- **merge gate**: says what it is for
- **pre-merge checks**: the most literal

## 2. Run only the checks a change needs

Split the single job by area, and choose which jobs run from the paths a PR changes:

| Area | Triggered by | Checks |
|---|---|---|
| Go | `server/**` | gofmt, vet, staticcheck, tests, govulncheck |
| Web | `web/**` | lint, typecheck, unit tests, build, npm audit |
| E2E | `server/**`, `web/**` | Playwright (Chromium and WebKit) |
| Shell and VM | `scripts/**`, `.githooks/**`, `.claude/hooks/**`, `deploy/**` | shellcheck, VM script tests, leakscan tests |
| Docs only | `**/*.md` and nothing else | none (the leak scan still runs) |

Points to settle:

- **Required checks.** If a job is skipped by a workflow-level `paths:` filter, a required check never
  reports, and the PR stays blocked. The usual fix is a small "changes" job that works out which areas a PR
  touched (a few lines of `git diff`, no third-party action), then a final job that sums up the results and
  is the only required check.
- **What always runs.** The secrets scan runs on every change. Pushes to `main`, manual runs and the weekly
  schedule probably run everything, so a deploy never depends on path guessing.
- **Local parity.** `check.sh` stays the single command to run before merging. It could take an area
  argument so CI and local runs share the same steps.
- **Setup cost.** Installing Playwright with `--with-deps` runs apt on every run, even when the browsers are
  cached. Measure each step's time before and after, so the change shows a real saving.

## 3. Review the pipeline as a whole

Before the repo goes public, read the pipeline end to end, as a reviewer would, and write down the intent
behind it in a short spec:

- what each stage guarantees and why it is there: feedback on PRs, gating the deploy, the scheduled scans;
- the feedback-time budget for a PR, and how the pipeline stays within it;
- the supply-chain stance: pinned actions, no third-party actions where a script will do, least-privilege
  tokens;
- how deploys are promoted, paused and rolled back, linking the existing deploy spec and runbook rather than
  repeating them.

The aim is a pipeline whose design reads as deliberate. Speed is only part of it.
