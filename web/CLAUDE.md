# web (React + TypeScript)

- TypeScript strict + noUncheckedIndexedAccess. No `any`, no `@ts-ignore` without a comment explaining why.
- `src/api/` is the only place that calls `fetch`. Its types mirror the API table in spec §6.
- `<Markdown>` is the only HTML renderer (markdown-it + highlight.js + DOMPurify). Never use dangerouslySetInnerHTML
  elsewhere; ESLint's `react/no-danger` enforces this.
- No inline scripts and no inline styles that need CSP changes. No external fonts, CDNs, or analytics.
- Count characters in code points (`[...s].length`), matching the server.
- Accessibility: real <button>s, tap targets of at least 44px, keyboard shortcuts on the study screen, visible focus.
- Device-only display settings live in localStorage behind a small wrapper with try/catch and defaults.
- Check layout with Playwright before saying UI work is done: dispatch `e2e-tester` (see the root CLAUDE.md).
  Suites in `e2e/`: `smoke` (happy paths, CSP), `layout` (each page at iPhone 16 and 1440px), `breakpoints`
  (600px and 900px lines, desktop project only). Shared helpers in `e2e/helpers.ts`.
- TypeScript is pinned to 6.0.x because typescript-eslint does not support 7.x yet; ESLint is pinned to 9.x for eslint-plugin-react.
- Checks: `bash scripts/check.sh frontend e2e` from the repo root (the `frontend` group is `npm run lint`,
  `npm run typecheck`, `npm test`, `npm run build`, `npm audit --omit=dev`).
