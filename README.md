# anki-app

A personal spaced-repetition flashcard app built for cards that contain code. Cards are Markdown with
syntax-highlighted code blocks; scheduling uses FSRS with Again / Hard / Good / Easy. One Go binary serves a
JSON API and the embedded React app, and stores everything in one SQLite file. It runs in a Docker container on a
small Google Cloud VM and is reached from an iPhone over Tailscale. The full design is in
[docs/specs/2026-11-02-flashcards-design.md](docs/specs/2026-11-02-flashcards-design.md); the deployment is in
[docs/specs/2026-11-05-server-deploy-design.md](docs/specs/2026-11-05-server-deploy-design.md).

This repo starts from a single squashed commit. The earlier development history is kept in a private archive.

## Prerequisites

- Go (current stable) and Node.js LTS (22 or newer).
- Git Bash on Windows, for the scripts in `scripts/`.
- Tailscale, to reach the VM (the app and SSH).

On Windows: `winget install --id GoLang.Go -e`, `winget install --id OpenJS.NodeJS.LTS -e`,
`winget install --id Tailscale.Tailscale -e`. Then install the web dependencies once with `cd web && npm ci`.

## Build and run

```bash
bash scripts/build.sh          # builds web/ into the binary: server/anki-app(.exe)
server/anki-app serve          # http://localhost:8080
```

The database is `anki-app.db` in the current directory unless `DB_PATH` says otherwise. `server/anki-app version`
prints the commit the binary was built from.

## Deploy

Merge to `main` and push. CI runs the done bar, builds the Docker image and pushes it to GHCR
(`ghcr.io/marccowan/anki-app`, tags `:main` and `:sha-<12>`). The VM checks for a new `:main` every minute and
deploys it: it takes a pre-deploy backup, swaps the container, and checks that `GET /api/health` reports the new
version within 60 seconds. If it doesn't, it rolls back to the previous image, restores the database from that
backup, and never retries that build.

The app runs as `ghcr.io/marccowan/anki-app` under systemd on the VM `anki`, reached at
`https://anki.<tailnet>.ts.net:8443` through `tailscale serve`. It still listens only on 127.0.0.1. The full runbook
(GCP setup, host setup, cutover, operations, token renewal, restore drill) is
[docs/runbooks/vm.md](docs/runbooks/vm.md).

## Operations

All of these run on the VM as root (`ssh root@anki`).

| Task | How |
|---|---|
| Status | `systemctl status anki-app`, `cat /var/lib/anki-app/deploy/image.env`, `systemctl list-timers 'anki-*'` |
| Stop the app (kill switch) | `systemctl stop anki-app`. A new image or a reboot starts it again; to keep it off, also pause deploys and `systemctl disable anki-app`. |
| Start it again | `systemctl start anki-app` (plus `systemctl enable anki-app` if you disabled it) |
| Pause automatic deploys | `touch /etc/anki-app/paused`; resume with `rm /etc/anki-app/paused` |
| Deploy a specific build | Pause first, then `systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-update ghcr.io/marccowan/anki-app:sha-<12>` |
| Restore a backup | `systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-restore --yes <file>`. The state it replaces is saved to `pre-deploy/` first. |
| Off-site copy now | `systemctl start anki-offsite.service` |
| Logs | `journalctl -u anki-app`, `-u anki-update`, `-u anki-offsite` |
| Stop the whole VM | Cloud Shell: `gcloud compute instances stop anki --zone=us-east1-b` |

To undo a bad version that passed its health check, revert the commit on `main` and push. Never re-run an older
GitHub Actions run: it re-pushes `:main` with old code.

## Development

Two shells:

```bash
cd server && go run ./cmd/anki-app serve
cd web && npm run dev          # open the URL Vite prints; it forwards /api to :8080
```

`bash scripts/check.sh` runs the full done bar: gofmt, vet, staticcheck, Go tests, govulncheck, ESLint,
typecheck, Vitest, the production build, `npm audit --omit=dev`, shellcheck, the VM script tests (need `jq`),
and the Playwright smoke tests at iPhone 16 and 1440×900 sizes. `bash scripts/docker-smoke.sh` builds the image
and checks it locally (needs Docker). The first Playwright run needs `cd web && npx playwright install chromium webkit`.

Once per clone, run `bash scripts/setup-hooks.sh`: it installs the pinned gitleaks (SHA-256 checked) and turns on the
git hooks that scan commits for leaked infrastructure names. Commits are refused until
`~/.config/anki-app/gitleaks-private.toml` exists (start from `scripts/gitleaks-private.example.toml`).

To update gitleaks, run `bash scripts/bump-gitleaks.sh`, commit on a `feat/` branch and open a PR; its CI exercises
the new binary.

## Import the Cpp deck

The cleanup task (spec §10) turns the AnkiApp export into `data/cpp.json`. Once that file exists:

```bash
server/anki-app import data/cpp.json
```

It creates the deck and all its cards as New, in one transaction. It refuses if a deck with that name already
exists, and any invalid card (empty, or a side over 6,000 characters) aborts the whole import. The format:

```json
{
  "deck": "Cpp",
  "cards": [
    { "front": "<markdown>", "back": "<markdown>" }
  ]
}
```

## Phone access

The iPhone, the laptop and the VM are on the same tailnet. On the iPhone, open `https://anki.<tailnet>.ts.net:8443`
in Safari, log in with the passphrase, then Share → **Add to Home Screen**. It opens full screen. `tailscale serve`
on the VM forwards HTTPS from the tailnet to 127.0.0.1:8080, so the web UI isn't exposed on the internet. Only the
Claude connector's own paths are public, on port 443 (runbook §9).

Sessions are stored in the database, so restoring a backup logs the phone out; log in again.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `ADDR` | `127.0.0.1:8080` | Listen address. Never bind to all interfaces. |
| `DB_PATH` | `anki-app.db` | SQLite file. |
| `ALLOWED_HOSTS` | `localhost,127.0.0.1` | Comma-separated `Host` allowlist. Add the tailnet name. |
| `PASSPHRASE` | unset | Turns on passphrase mode (at least 16 characters). |
| `TZ` | system zone | Time zone for the day rollover and the backup schedule, an IANA name, e.g. `America/New_York`. |
| `BACKUP_DIR` | `backups` next to `DB_PATH` | Where the app writes `daily/` and `pre-deploy/` backups. |

On the VM these are set in `/etc/anki-app/anki-app.env` (root, 0600); `DB_PATH` and `BACKUP_DIR` come from the
container's `/data` volume.

## Backup

- **Daily:** the app takes one at or after 03:00 (in `TZ`) and thins old ones: dailies for a week, then weekly,
  monthly and quarterly, up to a year. `GET /api/health` and the bottom of Settings show the last one.
- **Pre-deploy:** the updater takes one before every deploy and every restore; the newest 10 are kept.
- **Off-site:** at 04:00 the VM copies both folders to a GCS bucket it can add to but not delete from.
- **By hand:** `anki-app backup [--pre-deploy]` writes one consistent file (the WAL folded in) and prints its
  path. Each backup is a single `.db` file that opens on its own.

The quarterly restore drill, opening a bucket backup on the laptop, is in the runbook (section 8).

## License

Source-available under the [PolyForm Strict License 1.0.0](LICENSE.md): you may read the code and use it for
personal or other non-commercial purposes, but not modify, redistribute or sell it. Not an open-source licence.
