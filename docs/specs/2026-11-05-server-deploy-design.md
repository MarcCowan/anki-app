# Server deployment: design spec

- **Date:** 2026-11-05
- **Status:** Accepted. Marc agreed the decisions in conversation on 2026-11-03 and 2026-11-05.
- **Owner:** Marc (sole user)
- **Extends:** [2026-11-02-flashcards-design.md](2026-11-02-flashcards-design.md), the main spec, and replaces its
  §12 (Windows laptop deployment). Section numbers in this file are its own. "Main §n" means a section of the
  main spec.

## 1. Purpose and success criteria

The app moves off the laptop onto a free Google Cloud VM, so it no longer depends on the laptop being on. Changes
should reach it quickly and safely, because updates will be frequent for a while.

Success means:

1. **Merge to `main` and push → live in about 5–10 minutes**, with no manual step. CI takes most of that. The VM
   picks up a new image within about a minute.
2. **A failed deploy rolls itself back.** That includes the database, if the new version migrated it.
3. **At most one day of reviews is lost** in a disaster. Backups follow Marc's retention scheme (§4.3) and a copy
   is kept off the VM.
4. **The laptop is not needed**, except for development.
5. **Nothing on the VM is reachable from the public internet.** The web UI is reachable on the tailnet only, as
   it is today.
6. **Marc can see from his phone** which version is live and when the last backup ran.
7. **Free:** only free tiers are used, and a $1 budget alert catches anything that isn't free.
   *Update 2026-11-07:* not quite free. The ephemeral external IPv4 is charged at $0.005/hour (free tier: 1 hour a
   month), about $3.65 a month. Marc accepted that cost. Everything else stays on free tiers, and the budget alert
   is $5 (still 50% and 100%), so the expected IP charge doesn't alert every month (§7.1, §12).

## 2. Scope

**In scope:**
- In-app backups with retention, and a `backup` subcommand.
- A migration guard: refuse to start on a newer schema.
- `GET /api/health`, and a version and last-backup line on the Settings page.
- The Docker image, plus a local smoke script.
- The GitHub Actions workflow, pushing to GHCR.
- VM provisioning: a runbook plus an idempotent setup script.
- The pull-based updater, the restore script and the off-site backup copy.
- The one-time cutover from the laptop.
- Removing the Windows deploy scripts.

**Out of scope:**
- The MCP connector. Bookmarked in `docs/ideas/2026-11-05-mcp-connector.md`. Tailscale Funnel stays off until it
  exists. *Update 2026-11-08:* now designed in
  [2026-11-08-mcp-connector-design.md](2026-11-08-mcp-connector-design.md), which puts only its own paths on Funnel
  port 8443; the web UI stays tailnet-only. *Update 2026-11-14:* Claude only connects on port 443, so the
  connector's Funnel moved to 443 and the web UI's `tailscale serve` moved from 443 to 8443 (still tailnet-only).
  The diagram and steps below show the new port.
- Staging environments and blue/green deploys.
- Push notifications for failed deploys.
- Point-in-time recovery (Litestream).
- Infrastructure-as-code for GCP. Console steps are written down in the runbook instead.
- Running more than one replica.

## 3. Architecture

```
 laptop (dev)                    GitHub                               GCP e2-micro "anki" (us-east1-b, Debian 12)
 ───────────                     ──────                               ─────────────────────────────────────────────
 feat/* → check.sh → merge ──push──► Actions (push to main only)
                                    1. check.sh (the done bar)
                                    2. docker build → GHCR
                                       :main and :sha-<12>  ◄──pull every 60 s── anki-update.timer → anki-update
                                                                                   │ backup, swap, health check, rollback
                                                                                   ▼
 iPhone / laptop ──tailnet HTTPS──────────────────────────────────►  tailscale serve :8443 → 127.0.0.1:8080
                                                                     anki-app container (host network, non-root)
                                                                       /data/anki-app.db
                                                                       /data/backups/{daily,pre-deploy}
                                                                     anki-offsite.timer ──► GCS bucket (append-only)
```

- **One container, one process, one SQLite file.** The unchanged rules of main §3 still hold.
- **Host networking.** The app keeps its default listen address `127.0.0.1:8080` (a CLAUDE.md rule).
  `--network host` makes that the VM's own loopback, and `tailscale serve` forwards to it exactly as it does on
  the laptop today. No ports are published.
- **Pull, not push.** The VM checks GHCR for a new `:main` image every minute. GitHub never connects to the VM and
  holds no credential for it.

## 4. App changes

### 4.1 Configuration

One new variable is added to main §3.1:

| Variable | Default | Purpose |
|---|---|---|
| `BACKUP_DIR` | `backups` next to `DB_PATH`, i.e. `filepath.Join(filepath.Dir(DB_PATH), "backups")` | Root of the backup folders. |

The backup schedule is fixed in code: first check at or after **03:00 local time**, using `TZ`. There is no
setting for it.

### 4.2 Backup writer (`internal/backup`)

- **Layout:**
  - `BACKUP_DIR/daily/` holds scheduled backups.
  - `BACKUP_DIR/pre-deploy/` holds backups taken before a deploy or a restore.
  - The writer creates both folders with mode `0700`.
- **File names:** `anki-app-YYYYMMDDTHHMMSSZ.db`, using the UTC time the backup was taken. Names sort by time.
  - Only files whose names match `^anki-app-\d{8}T\d{6}Z\.db$` count as backups.
  - Any other file in those folders is ignored and never deleted.
- **Writing a backup:**
  0. Remove any leftover `anki-app-<timestamp>.db.partial` files in that folder, each by its exact name.
     `VACUUM INTO` refuses a target that already exists, and a crash can leave one behind.
  1. Run `VACUUM INTO ?` (a bound parameter) to a sibling file `<name>.partial`. This gives a consistent,
     compact single file while the app keeps running. WAL changes are included and no `-wal`/`-shm` file is
     needed.
  2. Open the `.partial` file read-only and run `PRAGMA quick_check`. The result must be `ok`.
  3. Fsync the file, then rename it to `<name>`.
  4. On any failure, remove that one `.partial` file by its exact name and return the error.
- **Deletion:** only ever `os.Remove` on single files the retention rules selected, inside the two known folders.
  No recursive deletes and no computed directory deletes.
- **Logging:** the backup name, size and duration only.

### 4.3 Retention (a pure function, test-first)

`Keep(backups []time.Time, now time.Time, loc *time.Location) (keep, drop []time.Time)` applies to `daily/` only.

Each backup's **local date** is its time in `loc` (Europe/London), and its **age** is the number of calendar days
between that date and today's local date. A backup is kept if **any** of these is true:

| Tier | Rule | About how many |
|---|---|---|
| Daily | age ≤ 6, i.e. today and the 6 days before | 7 |
| Weekly | age ≤ 31, and it is the newest backup of its Monday–Sunday week. That is the Sunday backup whenever one exists. | ~4 |
| Monthly | age ≤ 92, and it is the newest backup of its calendar month | ~3 |
| Quarterly | age ≤ 366, and it is the newest backup of its calendar quarter | ~4 |

- **Why "newest of the period" rather than "exactly Sunday":** a missed day doesn't leave a hole. If the VM was
  down on Sunday, Saturday's backup stands in for the week.
- **Count:** with one backup a day, the tiers overlap and the total stays at or below **18**. A test simulates two
  years of daily backups and checks that the count never exceeds 20.
- **Pre-deploy backups:** `pre-deploy/` keeps the newest **10**. They're pruned after each pre-deploy backup.

### 4.4 Scheduler

- A goroutine started by `serve` checks at startup and then every 15 minutes.
- **The rule** is a pure function, tested with the fake clock: back up if the local time is at or after 03:00 and
  no backup in `daily/` has today's local date.
- This catches up after downtime and a restart. It never takes two scheduled backups on one day.
- After a successful backup, it applies retention (§4.3).
- **Failures are logged at `ERROR` and the app keeps running.** The Settings line (§4.6) shows a stale backup
  time, so a failure can be seen from the phone.
- The scheduler stops when the server shuts down (context cancel). A backup in progress either finishes or leaves
  only a `.partial` file, which the next run removes (§4.2 step 0).

### 4.5 `anki-app backup [--pre-deploy]`

- **What it does:** opens the store, writes one backup (to `daily/`, or to `pre-deploy/` with the flag), applies
  that folder's retention, and prints the new file's absolute path on stdout.
- **Usage text:** `anki-app [serve] | anki-app import <file.json> | anki-app backup [--pre-deploy] | anki-app version`.
- **Who calls it:** the updater (§7.5) and the restore script (§7.6) call it with the *currently deployed* image
  while the app is stopped. The cutover (§9) calls it on the laptop.

### 4.6 Health endpoint and Settings line

- **`GET /api/health`** returns `200 {"version": "<main.version>", "lastBackupAt": <UTC ms of the newest daily
  backup> | null}`.
  - It runs `SELECT 1` first. If that fails it returns `503` with the usual error shape, code
    `unavailable`.
  - Like `GET /api/auth`, it is **exempt from passphrase auth**. The Host allowlist still applies.
  - It reveals nothing beyond the version and a timestamp, and the UI is on the tailnet only.
- **Settings page:** a muted line at the bottom: `Version 3f2a9c1d0b7e · Last backup 5 Nov, 03:00`.
  - The backup time is shown in the device's local time. It says `Last backup: none yet` when there isn't one.
  - The data is read from `/api/health`. If that request fails, the line is hidden.
  - This is a UI change, so the `e2e-tester` agent covers it at iPhone and desktop sizes.

### 4.7 Migration guard

- `store.Open` refuses to start if `PRAGMA user_version` is **greater** than the highest embedded migration. The
  error is `database schema v<N> is newer than this build (v<M>)`.
- **Why:** if an older image is ever started on a migrated database, it fails loudly instead of running on a
  schema it doesn't know. The updater's rollback (§7.5) restores the pre-deploy database together with the old
  image, so the two stay in step.

### 4.8 API table addition (main §6)

| Method & path | Body → Response |
|---|---|
| `GET /api/health` | → `{version, lastBackupAt}` or 503 `unavailable` |

## 5. Container image

### 5.1 `Dockerfile` (repository root, multi-stage)

| Stage | Base (pinned **by digest**) | Does |
|---|---|---|
| `web` | `node:26-bookworm-slim` | `npm ci` in `web/`, then `npm run build`. Vite writes to `server/internal/webui/dist/` (main §3). |
| `build` | `golang:1.27.4-bookworm` | `go mod download`, copies in `dist/` from `web`, then `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}"`. `modernc.org/sqlite` is pure Go, so static linking just works. |
| final | `gcr.io/distroless/static-debian12:nonroot` | Contains only `/anki-app`. `USER 65532:65532`, `ENTRYPOINT ["/anki-app"]`, `CMD ["serve"]`. |

- **Final-stage environment:** `DB_PATH=/data/anki-app.db` and `BACKUP_DIR=/data/backups`. `ADDR` keeps its
  code default.
- **Time zones:** the zone database is already compiled in (`time/tzdata` in `internal/config`), so the image
  needs no zoneinfo files.
- **Labels:**
  - `org.opencontainers.image.revision=${VERSION}`, which the updater reads to know which version to expect.
  - `org.opencontainers.image.source=https://github.com/MarcCowan/anki-app`, which links the GHCR package to the
    private repo.
- **Version:** CI passes `VERSION` = the first 12 characters of the commit SHA. Local builds pass
  `git describe --always --dirty`.
- **Base image updates:** the digests are bumped by hand in a normal `feat/` branch.
- **`.dockerignore`** excludes `.git`, `node_modules`, `web/dist`, `server/internal/webui/dist/*`, `*.db*`,
  `.env*`, test output, `.claude`, `.superpowers` and `docs`.

### 5.2 `scripts/docker-smoke.sh` (local, optional, not part of `check.sh`)

1. Build the image tagged `anki-app:smoke`.
2. Create a uniquely named volume, `anki-smoke-<UTC timestamp>`.
3. Run the image detached with `--read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges`. For
   this test only, set `ADDR=0.0.0.0:8080` and `ALLOWED_HOSTS=127.0.0.1,localhost`, and publish
   `127.0.0.1:18081:8080`. Docker Desktop's host networking isn't reliable, and this is a test override, not a
   change to the default.
4. Check that:
   - `/api/health` returns the expected `version`;
   - the process runs as uid 65532;
   - `docker run --rm … backup` writes a file under `daily/`.
5. Clean up: stop the container and `docker volume rm` the volume **by its exact name**.

Exits non-zero on the first failure, in the style of `check.sh`.

## 6. CI: `.github/workflows/main.yml`

- **Triggers:** `push` to `main`, and `workflow_dispatch` (the manual "Run workflow" button). Feature branches
  don't run CI. Marc runs `check.sh` locally before merging (CLAUDE.md), and CI repeats it as the gate before an
  image is built.
- **Concurrency:** one group, `main`, with `cancel-in-progress: true`. The newest commit includes everything
  before it.
- **Supply chain:** every action is **pinned to a full commit SHA**, with the version in a trailing comment.
  Top-level `permissions: contents: read`.

**Job `check`** (`ubuntu-24.04`, 25-minute timeout):
1. Checkout.
2. `actions/setup-go` with `go-version-file: server/go.mod`, using the module cache.
3. `actions/setup-node` with Node 26, using the npm cache.
4. `npm ci` in `web/`.
5. Restore the Playwright browser cache, keyed by the `@playwright/test` version in the lockfile, then run
   `npx playwright install --with-deps chromium webkit`. The iPhone 15 project uses WebKit.
6. `bash scripts/check.sh`.

**Job `image`** (needs `check`; `permissions: contents: read, packages: write`):
1. Checkout, set up Buildx, and log in to `ghcr.io` with `GITHUB_TOKEN`.
2. Build and push for `linux/amd64` only.
   - Tags: `ghcr.io/marccowan/anki-app:main` and `:sha-<12>`.
   - Build argument `VERSION`, using the Buildx GitHub Actions cache.
3. Prune old images: delete untagged versions and keep the newest **10** `sha-*` versions. This uses
   `actions/delete-package-versions`, pinned by SHA. It keeps storage well under the free private-package quota.

**Cost:** about 10 minutes per run on Linux, so 2,000 free minutes is about 200 merges a month. A failed run
deploys nothing, and GitHub emails Marc about failed runs by default.

**Shell linting:** `check.sh` gains a `shellcheck` step over `scripts/*.sh` and `deploy/vm/*`. Locally it prints
`shellcheck not installed: skipped` and continues. The CI runner has shellcheck installed, so it always runs
there.

## 7. The VM

### 7.1 Resources (created by hand; the steps are in `docs/runbooks/vm.md`)

- **Project:** a new GCP project. Its billing account has a **$1 budget alert**, emailing at 50% and 100%.
  *Update 2026-11-07:* the alert is **$5**: the external IPv4 costs about $3.65 a month (§12), and a $1 alert
  would fire every month.
- **VM `anki`:**
  - `e2-micro`, zone `us-east1-b` (a free-tier region), image Debian 12.
  - 30 GB `pd-standard` boot disk, Shielded VM on.
  - Ephemeral external IPv4. It's needed for outbound traffic only: Tailscale, GHCR, apt and GCS.
- **Service account `anki-vm`:**
  - No project-level roles. Access scope `cloud-platform`, so IAM alone decides what it can do.
  - **The default Compute service account is not used**, because it has Editor on the whole project.
- **Firewall:** delete the default network's ingress rules (`default-allow-ssh`, `-rdp`, `-icmp`, `-internal`).
  GCP's implicit deny then blocks **all** inbound traffic.
- **Bucket `anki-app-backups-<random suffix>`:**
  - `us-east1`, Standard class, uniform bucket-level access, public access prevention enforced.
  - Lifecycle rule: delete objects older than **400 days**.
  - `anki-vm` gets `roles/storage.objectCreator` and `roles/storage.objectViewer` on this bucket only. It
    **cannot delete or overwrite**, so a compromised VM can't destroy the off-site copies.

### 7.2 Host setup: `deploy/vm/setup.sh` (idempotent, run as root over Tailscale SSH)

- **Packages:**
  - Docker Engine from Docker's apt repository.
  - Tailscale.
  - `jq` and `unattended-upgrades`. Security updates are automatic, with reboots allowed at 04:30 Europe/London.
  - The Google Cloud CLI ships with GCE Debian images. If it's missing, install it.
- **Tuning:** a 1 GB swap file, because 1 GB of RAM is tight while pulling images. journald capped at 500 MB.
- **Directories, users and units:**
  - Creates the folders in §7.3 with the right owners and modes.
  - Installs the scripts into `/opt/anki-app/bin/` and the systemd units into `/etc/systemd/system/`, then
    enables the timers.
- **Not done by the script; manual steps in the runbook:**
  - `tailscale up --ssh --hostname anki`, then **turning off key expiry** for the node in the Tailscale admin
    console. Otherwise the VM drops off the tailnet after 180 days.
  - `tailscale serve --bg --https=8443 http://127.0.0.1:8080` (443 since 2026-11-14 belongs to the Claude connector).
  - `docker login ghcr.io` with a **classic PAT** that only has `read:packages`, expiring after 1 year. Marc
    sets a reminder to renew it.
  - Writing `/etc/anki-app/anki-app.env`.
- **Script updates:** the scripts and units live in the repo under `deploy/vm/`. They change rarely and are **not**
  updated by the image pull. After a change, re-run `setup.sh` on the VM.

### 7.3 Files on the VM

| Path | Owner, mode | Holds |
|---|---|---|
| `/etc/anki-app/anki-app.env` | root, `0600` | `PASSPHRASE` (generated, ≥ 16 characters), `ALLOWED_HOSTS=anki.<tailnet>.ts.net,127.0.0.1,localhost`, `TZ=Europe/London` |
| `/etc/anki-app/paused` | root | If it exists, the timer-driven updater does nothing |
| `/opt/anki-app/bin/` | root, `0755` | `anki-update`, `anki-restore`, `anki-offsite` |
| `/var/lib/anki-app/data/` | `65532:65532`, `0700` | `anki-app.db` (+`-wal`/`-shm`), `backups/daily/`, `backups/pre-deploy/`. Mounted at `/data`. |
| `/var/lib/anki-app/deploy/image.env` | root, `0644` | `IMAGE=<repo@sha256:…>` and `PREVIOUS=<repo@sha256:…>` |
| `/var/lib/anki-app/deploy/bad-digests` | root, `0644` | Digests that failed their health check, one per line |

### 7.4 `anki-app.service`

```ini
[Unit]
Description=anki-app
Requires=docker.service
After=docker.service network-online.target

[Service]
EnvironmentFile=/var/lib/anki-app/deploy/image.env
ExecStartPre=-/usr/bin/docker rm -f anki-app
ExecStart=/usr/bin/docker run --rm --name anki-app --network host \
  --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges --memory 384m \
  --env-file /etc/anki-app/anki-app.env -v /var/lib/anki-app/data:/data \
  --log-driver journald ${IMAGE}
ExecStop=/usr/bin/docker stop -t 15 anki-app
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

The app logs go to journald (`journalctl -u anki-app`). Card content is never logged (main §7). Stopping sends
SIGTERM, and the app shuts down gracefully within its 10-second timeout.

### 7.5 The updater: `anki-update [image-ref]` (bash; `anki-update.timer` runs it every minute)

1. **Lock and gate:** take `flock -n /run/anki-update.lock`, and exit 0 if another run holds it. With no argument,
   exit 0 if `/etc/anki-app/paused` exists. An explicit argument is a manual deploy and ignores the pause.
2. **Pull:** `docker pull` the ref (default `ghcr.io/marccowan/anki-app:main`). Read its repository digest `NEW`
   and its `revision` label `WANT`.
3. **Nothing new:**
   - If `NEW` equals the current `IMAGE`, exit 0.
   - With no argument, if `NEW` is listed in `bad-digests`, exit 0 too, so a broken image isn't retried every
     minute.
4. **Stop and back up:** log `deploying <WANT>`, then `systemctl stop anki-app`.
   - If a database exists, take a **pre-deploy backup with the current image**:
     `docker run --rm --network none --read-only --cap-drop ALL --user 65532:65532 -e TZ=Europe/London
     -v /var/lib/anki-app/data:/data $IMAGE backup --pre-deploy`. Keep the printed path.
   - If the backup fails: start the old version again, log the error and exit 1. **Never deploy without a
     backup.**
5. **Swap:** write `image.env` with `IMAGE=$NEW` and `PREVIOUS=<old>` (temp file, then `mv`), then
   `systemctl start anki-app`.
6. **Health check:** for up to **60 seconds**, every 2 seconds, request `http://127.0.0.1:8080/api/health` with
   `Host: 127.0.0.1`. It passes when the status is `200` and `.version == WANT`.
   - **Pass:** log `deployed <WANT>`, run `docker image prune -f --filter until=168h`, exit 0.
7. **Fail (roll back):**
   1. `systemctl stop anki-app`.
   2. Restore the database: copy the pre-deploy backup to `anki-app.db.restoring`, set it to `65532:65532`
      with mode `0600`, then `mv` it over `anki-app.db`, then `rm -f` exactly `anki-app.db-wal` and
      `anki-app.db-shm`.
   3. Write `IMAGE=<old>`, start the app, and append `NEW` to `bad-digests`.
   4. Health-check the old version. Exit 1 if it passes. If it doesn't, exit 2 and log `MANUAL ATTENTION NEEDED`.

   Anything written in the seconds the bad version ran is lost. That's acceptable.

- **First deploy:** there's no `IMAGE` and no database yet, so step 4 skips the backup, and a failed check means
  stop and exit 1.
  - `setup.sh` installs `anki-app.service` but **doesn't enable it**. With no image yet it would just restart
    in a loop.
  - The first successful deploy runs `systemctl enable anki-app`, so the app starts after a reboot.
- **Rolling back a bad version that passed its health check:** revert the commit on `main` and push. CI deploys
  the revert. **Migrations only go forward.** Undoing one means writing a new migration (main §4.2), never
  restoring an old image on its own. The migration guard (§4.7) enforces this.

### 7.6 Operations

| Task | How |
|---|---|
| Stop the app (kill switch) | `systemctl stop anki-app`. It stays stopped until it's started again. The updater only acts on new images. |
| Pause automatic deploys | `touch /etc/anki-app/paused`. Remove the file to resume. |
| Deploy a specific build | `anki-update ghcr.io/marccowan/anki-app:sha-<12>`. *Update 2026-11-07:* `touch /etc/anki-app/paused` first, or the timer deploys `:main` again within a minute; remove the file to resume. |
| Restore a backup | `anki-restore <file>` |
| Logs | `journalctl -u anki-app`, `journalctl -u anki-update` |

**What `anki-restore <file>` does:**
1. Asks for confirmation (`--yes` skips this) and stops the app.
2. Takes a `pre-deploy` backup of the current database, so a restore is itself undoable.
3. Copies the chosen file into place, as in step 7.2 of the updater.
4. Starts the app and runs the health check.

It accepts a file from `daily/`, from `pre-deploy/`, or downloaded from the bucket.

### 7.7 Off-site copy: `anki-offsite` (`anki-offsite.timer`, daily at 04:00 Europe/London)

- Runs `gcloud storage rsync --recursive /var/lib/anki-app/data/backups gs://<bucket>/backups`, **without**
  `--delete-unmatched-destination-objects`.
- **Append-only:** local retention doesn't spread to the bucket. The bucket keeps every backup until its 400-day
  lifecycle rule removes it. A wiped or compromised VM can't erase the history.
- **Size:** each backup is a VACUUM-compacted copy of a small database, so a year of dailies is a few hundred MB
  at most. That's under the GCS free tier (§12).

## 8. Security (changes to main §7)

- **Threat model:**
  - **(a) Other websites in Marc's browser** and **(b) script inside card content** are unchanged.
  - **(c) The open internet** still has **no way in**: GCP blocks all inbound traffic, Funnel is off, and the
    app listens on loopback only.
  - **(d) New: the supply chain and the cloud account.**
    - Actions pinned by SHA, base images pinned by digest, and lockfiles (main §7.4).
    - `govulncheck` and `npm audit` run on every CI run.
    - The service account can't delete anything, and the budget alert catches unexpected charges.
- **Admin access:** Tailscale SSH only. In an emergency, use the GCP serial console, enabled temporarily from the
  console.
- **Passphrase mode is on** on the VM. `tailscale serve` provides TLS, so the session cookie's `Secure` flag and
  HSTS (main §7.4) apply.
- **Secrets:**
  - The env file is root-only, mode 0600.
  - The GHCR token can only read packages and is held only by the VM's root `docker login`.
  - CI uses nothing but `GITHUB_TOKEN`.
  - No secrets are in the image or the repo.
- **Container:**
  - Non-root (uid 65532), read-only root filesystem, all capabilities dropped, `no-new-privileges`, memory
    limit.
  - **Trade-off:** host networking shares the VM's network namespace. That's acceptable because the container is
    the only service on the VM. It's what lets the app keep its loopback-only default.

## 9. Cutover from the laptop (one time)

1. **Rehearse with an empty database.** Deploy the VM with no database and check that the UI works over
   `https://anki.<tailnet>.ts.net`, using the passphrase. Then run the failure tests in §11.3.
2. **Freeze the laptop:** stop the laptop app with `scripts/kill.sh`. From here the laptop database is read-only.
3. **Back up the laptop database:** build the new binary locally and run
   `DB_PATH=%LOCALAPPDATA%\anki-app\anki-app.db BACKUP_DIR=<fresh temp folder> anki-app backup`. This produces
   one consistent file, with the WAL folded in.
4. **Copy it to the VM:** copy that file to `/tmp` on the VM over Tailscale SSH (`scp`). Then on the VM:
   1. `systemctl stop anki-app`.
   2. Move the file to `/var/lib/anki-app/data/anki-app.db`, set its owner to `65532:65532`, and remove the
      rehearsal database's `-wal`/`-shm` by name.
   3. `systemctl start anki-app`.
5. **Check:** the deck list counts match what the laptop showed, and one study review works.
6. **Phone:** remove the old home-screen app (the laptop's hostname) and add the new URL.
7. **Laptop:**
   - `tailscale serve reset` on the laptop.
   - Keep `%LOCALAPPDATA%\anki-app` for a month as a fallback, then delete it by hand.

## 10. Retiring the Windows scripts, and documentation

- **Delete** `scripts/deploy.sh`, `scripts/kill.sh` and `scripts/lib/winproc.sh`. `scripts/build.sh` stays: it
  builds a local binary on Windows or Linux.
- **CLAUDE.md, Commands:**
  - Deploy becomes "merge to `main` and push. CI builds the image, and the VM deploys it about a minute later."
  - Add the operations table from §7.6.
  - Add the runbook path, `docs/runbooks/vm.md`.
  - Add the `docs/ideas/` convention.
  - Remove the kill-switch line.
- **README:** the same deploy summary.
- **Main spec:** §12 already points here (this change). §1 #6, §2, §3, §3.1, §6 and §11 get the edits listed in
  §13.
- **`docs/runbooks/vm.md`** covers:
  - creating the GCP resources (§7.1);
  - first-time host setup (§7.2);
  - the cutover (§9);
  - the operations table (§7.6);
  - renewing the PAT;
  - a backup restore drill: download a bucket backup to the laptop and open it with a local `anki-app serve`.

## 11. Testing and the done bar

The done bar (main §9) is unchanged, with `shellcheck` added (§6).

### 11.1 Go, test-first (fake clock, temporary folders with fresh names)

- **Retention (§4.3):**
  - each tier's boundary (age 6 vs 7, 31 vs 32, 92 vs 93, 366 vs 367);
  - week boundaries Monday to Sunday;
  - month and quarter ends;
  - a missing Sunday (Saturday stands in);
  - a DST change in London (a backup at 00:30 local around the clock change);
  - two simulated years of dailies, with the count at most 20 every day and the expected survivors at chosen
    dates.
- **Scheduler rule (§4.4):** 02:59 vs 03:00; already taken today; catching up after a gap of several days.
- **Writer (§4.2):**
  - the backup opens and holds the same rows, including rows written after the last checkpoint (in the WAL);
  - no `.partial` file is left after a failure;
  - files not matching the name pattern are never deleted;
  - `pre-deploy/` keeps exactly 10.
- **`backup` subcommand:** writes to the right folder, prints the path, rejects unknown arguments.
- **`/api/health`:** returns version and `lastBackupAt` (null and set); is reachable without a session in
  passphrase mode; answers a bad Host with 403; returns 503 when the database is closed.
- **Migration guard:** `user_version = latest + 1` makes `Open` fail with the message from §4.7.
- **Config:** `BACKUP_DIR` default and override.
- **Indexes:** none of these features adds a query that needs an index (main §4.1). `lastBackupAt` comes from file
  names, not SQL.

### 11.2 Web

- Vitest: the Settings version line with and without a backup, and when `/api/health` fails (the line is
  hidden).
- Playwright (via the `e2e-tester` agent): the line is visible at iPhone and desktop sizes, and doesn't overflow.

### 11.3 Deployment (manual acceptance; steps in the runbook)

1. `scripts/docker-smoke.sh` passes locally.
2. The first push to `main` produces `:main` and `:sha-<12>` in GHCR.
3. **First deploy:** the VM picks up `:main`, `/api/health` shows the CI version, and the Settings line shows it on
   the phone.
4. **Normal deploy:** merge a trivial change, and it's live within about 10 minutes of the push. A pre-deploy
   backup exists.
5. **Failed deploy:** `anki-update gcr.io/distroless/static-debian12@sha256:<digest>`, an image with no app in it.
   Expected:
   - the health check fails and the rollback runs;
   - the database matches the pre-deploy backup and the old version is serving again;
   - the digest is listed in `bad-digests`.
6. **Restore:** `anki-restore` a daily backup. The Settings data matches that backup.
7. **Reboot:** `sudo reboot`, and the app comes back by itself.
8. **Off-site copy:** after 04:00, the bucket has the day's backup. Trying to delete it as `anki-vm` is refused.

## 12. Facts to confirm during setup (not verified while writing this)

The runbook marks each of these as a check.

- **GCP free tier:**
  - one `e2-micro` in us-east1/us-central1/us-west1;
  - 30 GB standard persistent disk;
  - 1 GB/month egress from North America;
  - 5 GB-month of Standard storage in Cloud Storage in those regions.
  - **Whether the ephemeral external IPv4 address is charged** for a free-tier VM. If it is, and isn't covered,
    stop and decide; the budget alert is the backstop.
    *Update 2026-11-07:* it is charged. https://cloud.google.com/vpc/network-pricing#ipaddress: $0.005/hour for
    an address in use on a standard VM, with a free tier of 1 hour a month, so about $3.65 a month. Marc decided
    on 2026-11-07 to accept it. The budget alert is $5 (§7.1).
- **GHCR private-package quotas** (storage and data transfer) on the GitHub Free plan.
  - Each deploy pulls only changed layers: about 15–20 MB for the app layer, while the distroless base stays
    cached.
  - If transfer turns out to be metered, that limits how many deploys a month fit.
- **Whether the Google Cloud CLI comes preinstalled** on current GCE Debian 12 images.

## 13. Changes to the main spec (made together with this document)

- **§1 #6:** add that the app now runs on a GCP VM; see this spec.
- **§2:** remove "export/backup commands" and "provisioning the cloud VM" from out of scope, pointing here.
- **§3 tree:** `docs/specs/`, `docs/plans/`, `docs/ideas/`, `docs/runbooks/`, `deploy/vm/` and `Dockerfile`.
- **§3.1:** add the `BACKUP_DIR` row.
- **§6:** add `GET /api/health`.
- **§7:** a pointer to §8 here.
- **§11:** a private GitHub remote, and CI on push to `main`.
- **§12:** replaced by a pointer here. The Windows scripts are removed in the last milestone (§14).

## 14. Delivery order

Each milestone is one `feat/` branch, merged when the done bar passes.

| # | Branch | Contents | Done when |
|---|---|---|---|
| 1 | `feat/backups` | §4 in full: config, writer, retention, scheduler, `backup` subcommand, `/api/health`, Settings line, migration guard | Done bar passes; e2e covers the Settings line |
| 2 | `feat/container-ci` | §5, §6: Dockerfile, `.dockerignore`, `docker-smoke.sh`, `shellcheck` step, workflow | Smoke passes locally; after merge, CI pushes the first image (§11.3 steps 1–2) |
| 3 | `feat/vm` | §7: `deploy/vm/` scripts, units, `setup.sh`, `docs/runbooks/vm.md` | Marc creates the GCP resources; §11.3 steps 3–8 pass on the VM |
| 4 | `feat/cutover` | §9, §10: database move, Windows scripts removed, CLAUDE.md and README updated | The phone uses the VM; the laptop app is off |
