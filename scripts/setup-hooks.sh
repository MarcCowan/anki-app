#!/usr/bin/env bash
# Installs the pinned gitleaks into .tools/gitleaks/ (SHA-256 checked) and turns on the git hooks.
# Idempotent. Run once per clone: bash scripts/setup-hooks.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=gitleaks.version
. "$root/scripts/gitleaks.version"

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)
    asset="gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"; want="$GITLEAKS_SHA256_LINUX_X64"; exe=gitleaks ;;
  MINGW*|MSYS*|CYGWIN*)
    asset="gitleaks_${GITLEAKS_VERSION}_windows_x64.zip"; want="$GITLEAKS_SHA256_WINDOWS_X64"; exe=gitleaks.exe ;;
  Darwin-arm64)
    asset="gitleaks_${GITLEAKS_VERSION}_darwin_arm64.tar.gz"; want="$GITLEAKS_SHA256_DARWIN_ARM64"; exe=gitleaks ;;
  *)
    echo "setup-hooks: unsupported platform $(uname -s)-$(uname -m)"; exit 1 ;;
esac

dest="$root/.tools/gitleaks"
bin="$dest/$exe"
stamp="$dest/version"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

if [ -x "$bin" ] && [ "$(cat "$stamp" 2>/dev/null || true)" = "$GITLEAKS_VERSION" ]; then
  echo "gitleaks $GITLEAKS_VERSION already installed"
else
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  url="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/${asset}"
  echo "downloading $url"
  curl -fsSL -o "$tmp/$asset" "$url"
  got="$(sha256 "$tmp/$asset")"
  if [ "$got" != "$want" ]; then
    echo "setup-hooks: SHA-256 MISMATCH for $asset (expected $want, got $got). Not installing."
    exit 1
  fi
  mkdir -p "$tmp/x"
  case "$asset" in
    *.tar.gz) tar -xzf "$tmp/$asset" -C "$tmp/x" ;;
    *.zip)
      if command -v unzip >/dev/null 2>&1; then
        unzip -q "$tmp/$asset" -d "$tmp/x"
      else
        zip_win="$(cygpath -w "$tmp/$asset")"
        out_win="$(cygpath -w "$tmp/x")"
        # Paths travel in env vars, so a quote in a path cannot break the PowerShell command.
        # shellcheck disable=SC2016 # $env:... is expanded by PowerShell, not bash
        ZIP_WIN="$zip_win" OUT_WIN="$out_win" powershell.exe -NoProfile -Command 'Expand-Archive -LiteralPath $env:ZIP_WIN -DestinationPath $env:OUT_WIN -Force'
      fi ;;
  esac
  mkdir -p "$dest"
  cp "$tmp/x/$exe" "$bin"
  chmod +x "$bin"
  echo "$GITLEAKS_VERSION" > "$stamp"
  echo "installed gitleaks $("$bin" version) at .tools/gitleaks/$exe"
fi

# Hook scripts must be executable, on disk and in the index (Windows checkouts lose the bit).
for h in "$root"/.githooks/*; do
  chmod +x "$h"
  git -C "$root" update-index --chmod=+x -- ".githooks/$(basename "$h")" 2>/dev/null || true
done
git -C "$root" config core.hooksPath .githooks
echo "git hooks enabled (core.hooksPath=.githooks)"

private="${ANKI_GITLEAKS_PRIVATE:-$HOME/.config/anki-app/gitleaks-private.toml}"
if [ ! -f "$private" ]; then
  echo
  echo "NEXT STEP: create $private"
  echo "  mkdir -p \"$(dirname "$private")\" && cp scripts/gitleaks-private.example.toml \"$private\""
  echo "Then replace the fake values with your real tailnet name, project ID, bucket, machine names and email."
  echo "Until that file exists, commits are refused (the scan fails closed)."
fi
