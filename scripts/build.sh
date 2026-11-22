#!/usr/bin/env bash
# Build the web app into server/internal/webui/dist, then the Go binary,
# stamped with the commit (`anki-app version` prints it).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
exe=""
if [ "${OS:-}" = "Windows_NT" ]; then exe=".exe"; fi
version="$(git -C "$root" describe --always --dirty)"

(cd "$root/web" && npm run build)
(cd "$root/server" && go build -ldflags "-X main.version=$version" -o "anki-app$exe" ./cmd/anki-app)
echo "built server/anki-app$exe ($version)"
