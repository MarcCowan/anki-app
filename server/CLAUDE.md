# server (Go)

- Standard library `net/http` routing only; no web framework.
- Package boundaries: SQL lives only in `internal/store`; go-fsrs is imported only in `internal/srs`;
  handlers in `internal/api` are thin (decode → validate → call → encode). `internal/archtest` enforces the first two.
- Multi-statement writes run in one transaction.
- Time comes from the injected clock, never `time.Now()` directly in logic. Store times as UTC unix ms.
- Errors: wrap with `fmt.Errorf("...: %w", err)`; map them to API error codes in one place; never return
  internal error text to clients.
- Pass `context.Context` through; no package-level mutable state.
- Every connection: `PRAGMA foreign_keys=ON`, WAL, busy_timeout (set in one place in `store.Open`).
- Count text length in code points (`utf8.RuneCountInString`), matching the frontend.
- Checks: `gofmt -l .` (empty), `go vet ./...`, `go tool staticcheck ./...`, `go test ./...`, `go tool govulncheck ./...`.
