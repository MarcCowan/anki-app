# Runbook: the anki-app VM

How to build, run and recover the GCP VM that serves anki-app. The design is
[docs/specs/2026-11-05-server-deploy-design.md](../specs/2026-11-05-server-deploy-design.md) ("§n" below).

**Where commands run.** Each block says where:

- **Cloud Shell**: https://shell.cloud.google.com (nothing to install on the laptop).
- **Laptop**: Git Bash, in the repo.
- **VM**: as root, after `ssh root@anki` over Tailscale SSH.

**Placeholders.** Replace `<PROJECT_ID>`, `<BUCKET>`, `<BILLING_ACCOUNT_ID>` and `<tailnet>` with your real values.
The tailnet name is the middle part of your `*.ts.net` names, e.g. `tailXXXX`. The web UI's URL will be
`https://anki.<tailnet>.ts.net:8443` (tailnet only). Port 443 is kept for the Claude connector (§9).

**Bootstrap approach (why there is no SSH step).** All default ingress firewall rules are deleted *before* the VM
exists, so nothing can ever connect to it from the internet. To get the first login anyway, the VM is created
with a one-time **startup script** that installs Tailscale and joins the tailnet using a single-use Tailscale
auth key, passed in VM metadata. From then on, all access goes over Tailscale SSH. The script and key are removed
from metadata as soon as the VM shows up on the tailnet (step 1.9). The used key is worthless after that.

---

## 0. Facts to confirm (§12)

Tick each one when you've confirmed it. If one is false, stop and decide before going on. The $5 budget alert
(1.3) is the backstop.

- [x] **e2-micro free tier**: one non-preemptible `e2-micro` a month in `us-east1`, `us-central1` or `us-west1`.
  Confirm at https://cloud.google.com/free/docs/free-cloud-features#compute.
- [x] **Disk free tier**: 30 GB-months of standard persistent disk. Same page.
- [x] **Egress free tier**: 1 GB/month from North America (excluding China and Australia). Same page.
- [x] **Cloud Storage free tier**: 5 GB-months of Standard storage in `us-east1`/`us-central1`/`us-west1`, plus
  5,000 Class A and 50,000 Class B operations a month. Same page, "Cloud Storage" section.
- [x] **Ephemeral external IPv4: charged, and accepted.** https://cloud.google.com/vpc/network-pricing#ipaddress
  charges static and ephemeral external IPv4 addresses in use on a standard VM at **$0.005/hour**. The free tier
  for them is only **1 hour a month** per account, so the VM's address costs about **$3.65 a month**
  (730 hours × $0.005). An ephemeral address only counts while the VM is running; a stopped VM costs nothing for
  it. Marc accepted this cost on 2026-11-07: it's the price of outbound access (Tailscale, GHCR, apt, GCS) without
  Cloud NAT, which costs more. The budget in 1.3 is set to $5 so this expected charge doesn't alert every month.
  **3–4 days after creating the VM**, open Console → Billing → Reports, group by SKU, and check that the external
  IP SKU is the only non-zero cost after savings, at roughly $0.12 a day.
- [x] **Free Trial vs paid billing account**: a Free Trial billing account closes after 90 days or $300 of
  credit, whichever comes first. When it closes, its resources are **stopped**, and later deleted, unless you
  upgrade to a paid account. The free tier itself keeps working after an upgrade. The terms are at
  https://cloud.google.com/free/docs/free-cloud-features#free-trial (not on the network-pricing page). To see
  whether your account is a trial, open Console → **Billing** → **Overview**: a trial shows its remaining credit
  and end date there, with an **Upgrade** button. Upgrade before the trial ends.
- [x] **GHCR quotas on GitHub Free**: Container registry storage and bandwidth are still free. See
  https://docs.github.com/en/billing/concepts/product-billing/github-packages. In November 2026 it said
  "currently free", with a month's notice before any change. If it becomes metered (1 GB transfer/month on Free),
  about 15–20 MB per deploy means roughly 50 deploys a month, which is plenty.
- **Google Cloud CLI on GCE Debian 12**: nothing to do. `setup.sh` installs it if it's missing; you'll see which
  in step 2.1 (`gcloud --version` in its checks).

## 1. GCP resources (§7.1)

### 1.1 Account and billing account (Console)

1. Go to https://console.cloud.google.com and sign in. Accept the terms of service if asked.
2. If you have no billing account yet, start the Free Trial, which creates one. Then go back to the Free Trial
   item in section 0, because it applies to you.

### 1.2 Project, billing link, APIs (Cloud Shell)

```bash
PROJECT_ID="anki-app-$(openssl rand -hex 3)"     # project IDs are global, so add a random suffix
gcloud projects create "$PROJECT_ID" --name="anki-app"
gcloud config set project "$PROJECT_ID"

gcloud billing accounts list                     # note the ACCOUNT_ID, e.g. XXXXXX-XXXXXX-XXXXXX
gcloud billing projects link "$PROJECT_ID" --billing-account=<BILLING_ACCOUNT_ID>

gcloud services enable compute.googleapis.com iam.googleapis.com storage.googleapis.com   # about a minute

# Cloud Shell forgets variables when the session ends. Save them in its persistent home folder:
echo "PROJECT_ID=$PROJECT_ID" > ~/anki-vars
echo "SA=anki-vm@$PROJECT_ID.iam.gserviceaccount.com" >> ~/anki-vars
```

In a later session, run `source ~/anki-vars && gcloud config set project "$PROJECT_ID"` first.

### 1.3 Budget alert: $5, emails at 50% and 100% (Console)

Why $5: the VM's external IPv4 address costs about $3.65 a month (section 0), so a $1 budget would alert every
month and the alert would stop meaning anything. At $5, the 50% email ($2.50) arrives each month around the 20th as
the IP charge builds up, which is a useful "billing still works as expected" sign. The 100% email means something
else is being charged: find it in Billing → Reports.

1. Console → **Billing** → pick the billing account → **Budgets & alerts** → **Create budget**.
2. Name it `anki-app 5`. Time range **Monthly**. Projects: **all**. Services: **all**. Credits: leave the defaults
   ticked. **Next.**
3. Budget type **Specified amount**, target amount **5**. It uses the billing account's currency, so it may be
   £5 rather than $5; either is fine. **Next.**
4. Thresholds: keep **50%** and **100%** of **Actual**, and delete any others (e.g. 90%). Keep **Email alerts to
   billing admins and users** ticked. **Finish.**

While the Free Trial is running, its credit pays for anything that isn't free, including the IP address. That
means the budget, which counts cost after credits, may stay at zero even when something is being charged. The
Billing Reports check in section 0 (3–4 days in) covers that gap.

### 1.4 Firewall: delete the default ingress rules (Cloud Shell)

Do this **before** creating the VM, so it is never reachable from the internet, not even for a minute.

```bash
gcloud compute firewall-rules list               # expect the four default-allow-* rules
gcloud compute firewall-rules delete default-allow-ssh default-allow-rdp default-allow-icmp default-allow-internal --quiet
gcloud compute firewall-rules list               # expect: Listed 0 items.
```

GCP creates the default network and its rules when the Compute API is enabled. If the first `list` comes back
empty, or the delete says a rule isn't found, wait a minute and list again.

With no rules left, GCP's implicit deny blocks **all** inbound traffic. Outbound traffic is still allowed, which
Tailscale, GHCR, apt and GCS need.

### 1.5 Service account `anki-vm`, no project roles (Cloud Shell)

```bash
source ~/anki-vars
gcloud iam service-accounts create anki-vm --display-name="anki VM (bucket roles only)"
# Grant it NO project-level roles. Its only access is the two bucket roles in 1.6. Check:
gcloud projects get-iam-policy "$PROJECT_ID" --flatten="bindings[].members" \
  --filter="bindings.members:$SA" --format="value(bindings.role)"    # expect: no output
```

### 1.6 Bucket, lifecycle rule, bucket-level roles (Cloud Shell)

```bash
source ~/anki-vars
BUCKET="anki-app-backups-$(openssl rand -hex 4)"
echo "BUCKET=$BUCKET" >> ~/anki-vars

gcloud storage buckets create "gs://$BUCKET" --location=us-east1 --default-storage-class=STANDARD \
  --uniform-bucket-level-access --public-access-prevention

cat > lifecycle.json <<'EOF'
{"lifecycle": {"rule": [{"action": {"type": "Delete"}, "condition": {"age": 400}}]}}
EOF
gcloud storage buckets update "gs://$BUCKET" --lifecycle-file=lifecycle.json

gcloud storage buckets add-iam-policy-binding "gs://$BUCKET" --member="serviceAccount:$SA" --role=roles/storage.objectCreator
gcloud storage buckets add-iam-policy-binding "gs://$BUCKET" --member="serviceAccount:$SA" --role=roles/storage.objectViewer
```

Check it:

```bash
gcloud storage buckets describe "gs://$BUCKET" --format="default(location,default_storage_class,uniform_bucket_level_access,public_access_prevention,lifecycle_config)"
gcloud storage buckets get-iam-policy "gs://$BUCKET"   # anki-vm has objectCreator and objectViewer only
echo "$BUCKET"                                        # write this down; it goes into offsite.env in 2.5
```

`anki-vm` can create and read objects, but **cannot delete or overwrite** them.

### 1.7 Tailscale preparation (Tailscale admin console)

1. https://login.tailscale.com/admin/dns: **MagicDNS** and **HTTPS Certificates** are on. They already are,
   because the laptop uses `tailscale serve`.
2. https://login.tailscale.com/admin/acls: the policy must have an `ssh` rule that lets you reach your own devices
   as root. The default policy has one:
   ```json
   "ssh": [
     {"action": "check", "src": ["autogroup:member"], "dst": ["autogroup:self"], "users": ["autogroup:nonroot", "root"]}
   ]
   ```
   If it's missing, add it. "check" makes you re-confirm in the browser now and then. That's expected.
3. https://login.tailscale.com/admin/machines: make sure no machine is already called `anki`. If one is, the new
   VM would become `anki-1`.
4. https://login.tailscale.com/admin/settings/keys → **Generate auth key…**:
   - Description `anki bootstrap`.
   - **Reusable off**, Expiration **1 day**, **Ephemeral off**, no tags.
   - **Pre-approved on**, if that option is shown (only when device approval is on).
   - **Generate**, then copy the `tskey-auth-…` value. It's shown once.

### 1.8 Create the VM with the bootstrap script (Cloud Shell)

```bash
source ~/anki-vars
cat > bootstrap.sh <<'EOF'
#!/bin/bash
# One-time bootstrap (docs/runbooks/vm.md 1.8): install Tailscale and join the tailnet, so the VM
# never needs an inbound firewall rule. Removed from the VM's metadata in step 1.9.
set -euo pipefail
if tailscale status >/dev/null 2>&1; then echo "bootstrap: tailscale already up"; exit 0; fi
if ! command -v tailscale >/dev/null; then
  mkdir -p --mode=0755 /usr/share/keyrings
  curl -fsSL https://pkgs.tailscale.com/stable/debian/bookworm.noarmor.gpg -o /usr/share/keyrings/tailscale-archive-keyring.gpg
  curl -fsSL https://pkgs.tailscale.com/stable/debian/bookworm.tailscale-keyring.list -o /etc/apt/sources.list.d/tailscale.list
  for try in 1 2 3 4 5 6; do   # first boot: apt may be busy with its own daily jobs
    apt-get -o DPkg::Lock::Timeout=300 update && apt-get -o DPkg::Lock::Timeout=300 install -y tailscale && break
    echo "bootstrap: apt attempt $try failed, retrying"; sleep 20
  done
  command -v tailscale >/dev/null
fi
key="$(curl -fsS -H 'Metadata-Flavor: Google' http://metadata.google.internal/computeMetadata/v1/instance/attributes/ts-authkey)"
tailscale up --auth-key="$key" --ssh --hostname anki
echo "bootstrap: tailscale is up"
EOF

read -rsp 'Tailscale auth key: ' TS_AUTHKEY; echo      # paste the key; it isn't echoed or saved in bash history

gcloud compute instances create anki \
  --zone=us-east1-b \
  --machine-type=e2-micro \
  --image-family=debian-12 --image-project=debian-cloud \
  --boot-disk-size=30GB --boot-disk-type=pd-standard \
  --shielded-secure-boot --shielded-vtpm --shielded-integrity-monitoring \
  --service-account="$SA" --scopes=cloud-platform \
  --metadata-from-file=startup-script=bootstrap.sh \
  --metadata=ts-authkey="$TS_AUTHKEY"
unset TS_AUTHKEY
```

What the flags give you:

- `e2-micro` in `us-east1-b`, Debian 12, a 30 GB `pd-standard` disk.
- Shielded VM on: Secure Boot, vTPM, integrity monitoring.
- The `anki-vm` service account with the `cloud-platform` scope, so IAM alone decides access. **Not** the default
  Compute service account.
- The default network with an ephemeral external IPv4, used for outbound traffic only.

Expect this warning: `You have selected a disk size of under [200GB]. This may result in poor I/O performance.`
It's fine for this app. You may also see a note that the disk is larger than the image; Debian grows the file
system on first boot.

The key can still turn up in two places: gcloud's own logs under `~/.config/gcloud/logs` (they record command
arguments), and the VM's metadata until step 1.9. It's single-use, and step 1.9 revokes it once it has been used,
so neither copy is any good after that.

### 1.9 Check the bootstrap, then remove it (Cloud Shell and laptop)

Wait about two minutes, then:

```bash
# Cloud Shell: the startup script's output is in the serial log
gcloud compute instances get-serial-port-output anki --zone=us-east1-b | grep startup-script | tail -20
# expect a line ending in: bootstrap: tailscale is up
```

```bash
# Laptop
tailscale status | grep anki      # the VM is listed
ssh root@anki hostname            # prints: anki
```

On the first connection, `ssh` asks whether to trust the host key. Answer `yes`. Tailscale SSH in "check" mode may
also print a URL. Open it in the browser and approve, and the command carries on.

If `anki` doesn't resolve, use the full name, `anki.<tailnet>.ts.net`. Then remove the bootstrap from metadata,
so it never runs again and the key isn't kept:

```bash
# Cloud Shell
gcloud compute instances remove-metadata anki --zone=us-east1-b --keys=startup-script,ts-authkey
gcloud compute instances describe anki --zone=us-east1-b --format="value(metadata.items)"   # neither key listed
```

In the Tailscale admin console, open **Settings → Keys**. If the `anki bootstrap` key is still listed, **revoke**
it.

**If the bootstrap failed** (no "tailscale is up" line): read the serial log for the error. The usual causes are an
expired or already-used key, or a key that needed **Pre-approved**. To retry:

1. In **Settings → Keys**, revoke the previous key. Then generate a new one (1.7 step 4).
2. In Cloud Shell, set the new key without it reaching history, then restart the VM. The startup script runs on
   every boot until it's removed.
   ```bash
   read -rsp 'Tailscale auth key: ' TS_AUTHKEY; echo
   gcloud compute instances add-metadata anki --zone=us-east1-b --metadata=ts-authkey="$TS_AUTHKEY"
   unset TS_AUTHKEY
   gcloud compute instances stop anki --zone=us-east1-b && gcloud compute instances start anki --zone=us-east1-b
   ```
3. Repeat the checks above, remove the metadata, and revoke the key.

## 2. Host setup (§7.2)

### 2.1 Copy the scripts and run `setup.sh`

```bash
# Laptop, in the repo, on the branch that has deploy/vm/ (feat/vm, later main)
scp -r deploy/vm root@anki:/tmp/
ssh root@anki
```

The host-key question and "check" URL from 1.9 can come up again here. Answer `yes` and approve in the browser.

```bash
# VM
cd /tmp/vm && bash setup.sh
```

`setup.sh` is idempotent: running it again changes nothing. It:

- installs Docker Engine (from Docker's apt repository), Tailscale (skipped, it's already there), `jq`,
  `unattended-upgrades` and, if missing, the Google Cloud CLI;
- sets the time zone to Europe/London, with security updates and reboots allowed at 04:30;
- adds a 1 GB swap file and caps journald at 500 MB;
- creates the §7.3 folders;
- installs `lib.sh`, `anki-update`, `anki-restore` and `anki-offsite` into `/opt/anki-app/bin/`, and the units
  from `deploy/vm/systemd/` into `/etc/systemd/system/`;
- enables and starts `anki-update.timer` and `anki-offsite.timer`. It does **not** enable `anki-app.service`; the
  first deploy does that.

At the end it prints a checklist of the manual steps that remain (the rest of section 2), with `[x]` on the ones
already done, and a reminder to pause before deploying a specific build (section 6).

Check it:

```bash
# VM
timedatectl | grep 'Time zone'                       # Europe/London
systemctl list-timers 'anki-*'                       # anki-update.timer and anki-offsite.timer
ls -l /opt/anki-app/bin/                             # lib.sh anki-update anki-restore anki-offsite
ls -ld /etc/anki-app /var/lib/anki-app/data /var/lib/anki-app/deploy   # data: 65532 65532, drwx------
gcloud --version | head -1                           # shows whether gcloud was preinstalled or installed by setup.sh
free -h | grep Swap                                  # about 1.0Gi
```

Until step 2.6 the update timer runs every minute and fails to pull, because it isn't logged in to GHCR yet. That's
harmless: nothing is running, and a failed pull changes nothing.

### 2.2 Tailscale on the VM

The bootstrap already ran `tailscale up --ssh --hostname anki`. If you ever rebuild the VM without the bootstrap,
run that command on the VM yourself.

- [ ] **Turn off key expiry**: https://login.tailscale.com/admin/machines → `anki` → **⋯** → **Disable key
  expiry**. Otherwise the VM drops off the tailnet after 180 days.

### 2.3 `tailscale serve`

```bash
# VM
tailscale serve --bg --https=8443 http://127.0.0.1:8080
tailscale serve status        # https://anki.<tailnet>.ts.net:8443 (tailnet only) proxies to http://127.0.0.1:8080
```

Port 8443, not 443: 443 is the only port Claude's servers connect to, so it's kept for the connector's Funnel
(§9), and everything mapped on a Funnel port is public.

This setting survives reboots. Until the first deploy, the URL returns a 502 error. That's expected.

### 2.4 `/etc/anki-app/anki-app.env` (root, 0600)

Run this **once**. It refuses to overwrite an existing file, so the passphrase can't be changed by accident.

```bash
# VM
f=/etc/anki-app/anki-app.env
if [ -e "$f" ]; then echo "$f already exists; not touching it"; else
  TSNAME="$(tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//')"    # anki.<tailnet>.ts.net
  PASS="$(openssl rand -base64 24)"                                               # 32 characters
  ( umask 077
    printf 'PASSPHRASE=%s\nALLOWED_HOSTS=%s,127.0.0.1,localhost\nTZ=Europe/London\n' "$PASS" "$TSNAME" > "$f" )
  chmod 0600 "$f"
  echo "Passphrase (save it in your password manager now): $PASS"
  unset PASS
fi
ls -l "$f"                      # -rw------- root root
grep -v '^PASSPHRASE=' "$f"     # ALLOWED_HOSTS=anki.<tailnet>.ts.net,127.0.0.1,localhost and TZ=Europe/London
```

Run `clear` once the passphrase is saved.

### 2.5 `/etc/anki-app/offsite.env` (root, 0644)

```bash
# VM: use the bucket name from 1.6
printf 'BUCKET=%s\n' '<BUCKET>' > /etc/anki-app/offsite.env
chmod 0644 /etc/anki-app/offsite.env
cat /etc/anki-app/offsite.env
gcloud storage ls "gs://<BUCKET>"       # works as anki-vm (empty output is fine; an error isn't)
```

The bucket name isn't a secret.

### 2.6 GHCR read-only token and `docker login` (do this last)

Logging in "arms" the updater: within a minute of this step, the timer pulls `:main` and deploys it. That's why the
env files come first.

1. GitHub → **Settings** → **Developer settings** → **Personal access tokens** → **Tokens (classic)** →
   **Generate new token (classic)**.
2. Note `anki VM ghcr read`. Expiration **Custom**: one year from today. Scope: tick **only `read:packages`**.
   **Generate token**, then copy it.
3. Add a calendar reminder for **two weeks before** the expiry date to renew it (section 7).

```bash
# VM, as root (the updater runs as root and uses /root/.docker/config.json)
read -rsp 'GHCR token: ' T; echo           # paste the token; it isn't echoed or saved in history
printf %s "$T" | docker login ghcr.io -u marccowan --password-stdin; unset T
# Expect "Login Succeeded". The warning about unencrypted storage in /root/.docker/config.json is expected;
# that file is root-only.
```

## 3. First deploy

The timer will usually have started it already. To watch it, or run it by hand:

```bash
# VM
journalctl -u anki-update -f             # watch the timer: "deploying <sha>" … "deployed <sha>"; Ctrl-C to stop
systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-update; echo "exit=$?"
# A manual run says nothing and exits 0 if the timer is running at that moment (lock held)
# or the image is already deployed. Check the journal either way.
```

Run manual deploys under `systemd-run` (as above) rather than directly: an SSH drop or Ctrl-C can't kill
it mid-deploy, and its log lands in the journal (`--wait` waits for it, `--pipe` shows its output, `--collect`
cleans up the transient unit; all three exist in systemd 252 on Debian 12). The same goes for every manual
`anki-update <ref>` and `anki-restore` below.

Checks before moving on:

```bash
# VM
journalctl -u anki-app -n 20             # should show the app's "listening" line
journalctl CONTAINER_NAME=anki-app -n 20 # use this instead if the line above is empty
```

**If the first deploy fails.** A failed first deploy leaves the app stopped and puts the `:main` digest into
`/var/lib/anki-app/deploy/bad-digests`, so the timer will **not** retry it, even after you fix the cause.
The cause is often a host problem: a typo or missing value in `/etc/anki-app/anki-app.env`, a passphrase
under 16 characters, or a slow start.
1. Read the logs: `journalctl -u anki-update -n 50` and `journalctl -u anki-app -n 50`.
2. Fix the cause (usually `/etc/anki-app/anki-app.env`, section 2.4).
3. Retry with an explicit ref, which ignores `bad-digests`:
   ```bash
   # VM
   systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-update ghcr.io/marccowan/anki-app:main; echo "exit=$?"
   ```

Then do §11.3 step 3 below.

## 4. Acceptance (§11.3 steps 3–8)

This is the §9 rehearsal, on an empty database. Steps 5 and 6 need some data to compare, so first create a test
deck with two or three cards in the UI.

- [ ] **3. First deploy.** On the VM:
  ```bash
  curl -s -H 'Host: 127.0.0.1' http://127.0.0.1:8080/api/health   # {"version":"<12 chars>","lastBackupAt":<ms>}
  systemctl is-enabled anki-app                                     # enabled
  cat /var/lib/anki-app/deploy/image.env                            # IMAGE=ghcr.io/marccowan/anki-app@sha256:…
  ls /var/lib/anki-app/data/backups/daily/                          # one anki-app-<UTC>.db from just now
  ```
  The scheduler checks as soon as the app starts. After 03:00 London it takes the day's backup within seconds, so
  `lastBackupAt` is a time from just now. It's `null` only if you deployed between 00:00 and 03:00 London.

  On the laptop, `git fetch && git rev-parse origin/main | cut -c1-12` gives the same version. On the phone, open
  `https://anki.<tailnet>.ts.net:8443` in Safari, enter the passphrase, and check the line at the bottom of Settings.
  It should show `Version <that sha> · Last backup <today, a few minutes ago>`, or `Last backup: none yet` in
  the 00:00–03:00 case.
- [ ] **4. Normal deploy.** Merge a trivial change to `main` and push it, noting the time. Within about 10 minutes:
  ```bash
  journalctl -u anki-update --since "-15 min" | grep -E 'deploying|deployed'
  ls -lt /var/lib/anki-app/data/backups/pre-deploy/ | head -3      # a new anki-app-<UTC>.db
  curl -s -H 'Host: 127.0.0.1' http://127.0.0.1:8080/api/health   # the new version
  ```
- [ ] **5. Failed deploy.** Note the deck counts in the UI first. Then deploy an image with no app in it:
  ```bash
  docker pull gcr.io/distroless/static-debian12
  REF="$(docker inspect --format '{{index .RepoDigests 0}}' gcr.io/distroless/static-debian12)"
  echo "$REF"                                      # gcr.io/distroless/static-debian12@sha256:…
  systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-update "$REF"; echo "exit=$?"
  ```
  Expected, after about 60 seconds of failed health checks:
  - `exit=1`, and the log shows the rollback;
  - `cat /var/lib/anki-app/deploy/image.env` shows `IMAGE` back on the previous digest;
  - `cat /var/lib/anki-app/deploy/bad-digests` lists the distroless digest;
  - `/api/health` shows the previous version;
  - the deck counts in the UI are unchanged, because the database was restored from the pre-deploy backup.

  `exit=2` with `MANUAL ATTENTION NEEDED` is a failure of this test: see "Something's wrong" in section 6.
- [ ] **6. Restore.** Use a daily backup. Normally there's already one from the first deploy (step 3). If you
  deployed between 00:00 and 03:00, wait until after 03:00.
  1. Run `ls /var/lib/anki-app/data/backups/daily/` and pick a file.
  2. In the UI, add a card called `restore test`. It postdates the backup.
  3. Run `systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-restore --yes /var/lib/anki-app/data/backups/daily/anki-app-<UTC>.db`.
     `--yes` is needed because `--pipe` gives the confirmation prompt no terminal to answer on.

  Expected: exit 0, the `restore test` card is gone, and the data matches the backup. A new file in
  `backups/pre-deploy/` holds the state from before the restore, so the restore can be undone. The phone is
  logged out (sessions are in the database); log in again.
- [ ] **7. Reboot.** On the VM run `reboot`. The SSH session drops. After a minute or two, from the laptop:
  ```bash
  curl -s https://anki.<tailnet>.ts.net:8443/api/health      # answers with the current version
  ssh root@anki 'systemctl is-active anki-app; systemctl list-timers "anki-*" --no-pager; tailscale serve status'
  ```
- [ ] **8. Off-site copy.** After 04:00 London, or right away with `systemctl start anki-offsite.service`. Run
  this **on the VM**, so gcloud acts as `anki-vm`. In Cloud Shell you are the project Owner, and the delete would
  succeed.
  ```bash
  # VM
  journalctl -u anki-offsite -n 20 --no-pager
  gcloud storage ls -r "gs://<BUCKET>/backups/"                          # today's daily backup is listed
  gcloud storage rm "gs://<BUCKET>/backups/daily/anki-app-<UTC>.db"      # MUST fail: 403, no storage.objects.delete
  ```

## 5. Cutover from the laptop (§9, one time)

Do this only once section 4 has passed.

1. **Rehearse.** That's section 4.
2. **Freeze the laptop.** Stop the laptop app. The cutover on 2026-11-07 used `bash scripts/kill.sh`, which
   `feat/cutover` then deleted; it is in git history before that merge. Check with
   `curl -s http://127.0.0.1:8080/api/auth`, which must fail to connect. From here the laptop database is
   read-only. To leave it untouched as the fallback, copy `anki-app.db` with its `-wal` and `-shm` to a fresh
   folder and point `DB_PATH` in step 3 at the copy.
3. **Back up the laptop database.** On the laptop, in the repo on a branch that has the `backup` subcommand:
   ```bash
   bash scripts/build.sh
   OUT="$HOME/anki-cutover-$(date -u +%Y%m%dT%H%M%SZ)"; mkdir "$OUT"
   DB_PATH="$(cygpath -w "$LOCALAPPDATA/anki-app/anki-app.db")" BACKUP_DIR="$(cygpath -w "$OUT")" server/anki-app backup
   F="$(ls "$OUT"/daily/anki-app-*.db)"; echo "$F"
   ```
   The result is one consistent file, with the WAL folded in. Note the deck counts the laptop showed.
4. **Copy it to the VM and swap it in:**
   ```bash
   # Laptop
   scp "$F" root@anki:/tmp/anki-app.db
   ```
   Then on the VM, swap it in with `anki-restore`. It does the §9 swap (stop, move into place, `65532:65532`,
   remove `-wal`/`-shm` by name, start). It also backs up the rehearsal database first and health-checks
   afterwards. Pause automatic deploys around the swap, so a deploy can't start in the middle:
   ```bash
   # VM
   touch /etc/anki-app/paused
   systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-restore --yes /tmp/anki-app.db; echo "exit=$?"     # expect exit=0
   rm /etc/anki-app/paused
   rm -f /tmp/anki-app.db                  # anki-restore copies it, so the original is still here
   ```
   If `anki-restore` isn't usable, the manual equivalent (still paused) is:
   ```bash
   systemctl stop anki-app
   mv /tmp/anki-app.db /var/lib/anki-app/data/anki-app.db
   chown 65532:65532 /var/lib/anki-app/data/anki-app.db
   chmod 0600 /var/lib/anki-app/data/anki-app.db
   rm -f /var/lib/anki-app/data/anki-app.db-wal /var/lib/anki-app/data/anki-app.db-shm
   systemctl start anki-app
   ```
5. **Check.** The deck list counts match what the laptop showed, and one study review works.
6. **Phone.** Delete the old home-screen app (the laptop's hostname). Open `https://anki.<tailnet>.ts.net:8443` in
   Safari, log in, then Share → **Add to Home Screen**.
7. **Laptop:**
   - Run `tailscale serve reset`.
   - Keep `%LOCALAPPDATA%\anki-app` for a month as a fallback, then delete it by hand.
   - Delete the cutover copy by name: `rm "$F"`, then `rmdir "$OUT/daily" "$OUT/pre-deploy" "$OUT"`. Drop any
     folder that doesn't exist from the `rmdir`.

## 6. Operations (§7.6)

All of these run on the VM as root.

| Task | How |
|---|---|
| Status at a glance | `systemctl status anki-app`, `cat /var/lib/anki-app/deploy/image.env`, `systemctl list-timers 'anki-*'` |
| Stop the app (kill switch) | `systemctl stop anki-app`. It stays stopped until it's started again, but a **new image** starts it, and so does a reboot. To keep it off, also pause deploys, and run `systemctl disable anki-app`. |
| Start it again | `systemctl start anki-app` (plus `systemctl enable anki-app` if you disabled it) |
| Pause automatic deploys | `touch /etc/anki-app/paused`. Resume with `rm /etc/anki-app/paused`. |
| Deploy a specific build | `touch /etc/anki-app/paused` first, then `systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-update ghcr.io/marccowan/anki-app:sha-<12>`. An explicit image works while paused. Without the pause, the timer deploys `:main` again within a minute. `rm /etc/anki-app/paused` to go back to automatic deploys. |
| Retry a digest that failed once | Delete its line from `/var/lib/anki-app/deploy/bad-digests` (the timer then tries it again), or deploy it explicitly (row above) |
| Restore a backup | `systemd-run --wait --pipe --collect /opt/anki-app/bin/anki-restore --yes <file>` (`--yes` is required under `--pipe`; run it directly without `--yes` to get the prompt). Accepts a file from `daily/`, from `pre-deploy/`, or downloaded from the bucket. Sessions live in the database, so the phone has to log in again afterwards. |
| Take an off-site copy now | `systemctl start anki-offsite.service` |
| Logs | `journalctl -u anki-app`, `journalctl -u anki-update`, `journalctl -u anki-offsite` (add `-f` to follow, `--since today`) |
| Re-run host setup after a `deploy/vm/` change | Laptop: `scp -r deploy/vm root@anki:/tmp/`. VM: `cd /tmp/vm && bash setup.sh`. Image pulls never update these scripts. |
| Stop the whole VM (from anywhere, even the phone) | Cloud Shell or the Cloud Console app: `gcloud compute instances stop anki --zone=us-east1-b` |

**Undoing a bad version that passed its health check:** revert the commit on `main` and push. CI deploys the
revert. Migrations only go forward. Never restore an old image on its own; the migration guard refuses it anyway.

**Never "Re-run" an older GitHub Actions run** once newer ones have succeeded. It re-pushes `:main` with the old
code, and the VM deploys it within a minute. To go back, revert on `main` (above) or pin a build (pause first).

**If `anki-update` timed out** (`start operation timed out` or `Failed with result 'timeout'` in
`journalctl -u anki-update`): systemd kills it after 15 minutes (`TimeoutStartSec`). If that happened in the
middle of a deploy or rollback, the app can be left stopped. Check `systemctl status anki-app` and
`cat /var/lib/anki-app/deploy/image.env`, then follow "Something's wrong" below.

**Something's wrong** (for example `MANUAL ATTENTION NEEDED` in `journalctl -u anki-update`):

1. `touch /etc/anki-app/paused`.
2. Read `journalctl -u anki-app -n 100` and `journalctl -u anki-update -n 100`.
3. Deploy a known-good `sha-<12>` explicitly, or `anki-restore` the newest `pre-deploy/` backup.

**Emergency access when Tailscale SSH is down:**

- **Read-only boot log:** `gcloud compute instances get-serial-port-output anki --zone=us-east1-b`, from Cloud
  Shell.
- **Re-join the tailnet:** run the bootstrap again.
  1. Revoke any old bootstrap key, then generate a new one (1.7 step 4).
  2. In Cloud Shell, with `bootstrap.sh` from 1.8 (it's in your Cloud Shell home folder; recreate it if not):
     ```bash
     read -rsp 'Tailscale auth key: ' TS_AUTHKEY; echo
     gcloud compute instances add-metadata anki --zone=us-east1-b \
       --metadata-from-file=startup-script=bootstrap.sh --metadata=ts-authkey="$TS_AUTHKEY"
     unset TS_AUTHKEY
     gcloud compute instances stop anki --zone=us-east1-b && gcloud compute instances start anki --zone=us-east1-b
     ```
  3. Afterwards, remove the metadata and revoke the key, as in 1.9.
- **Interactive serial console** (§8): `gcloud compute instances add-metadata anki --zone=us-east1-b
  --metadata=serial-port-enable=TRUE`, then `gcloud compute connect-to-serial-port anki --zone=us-east1-b`.
  - The image has no password logins, so you can only log in if a user with a password exists.
  - Set one temporarily through a startup script, and remove it when you're done.
  - Afterwards, turn the console off again with `--metadata=serial-port-enable=FALSE`.

## 7. Renewing the GHCR token (yearly)

GitHub emails you before a token expires. Once it has expired, pulls fail with `denied` or `unauthorized` in
`journalctl -u anki-update`. The app **keeps running** the current version, but no new deploys arrive.

1. GitHub → Settings → Developer settings → Tokens (classic) → `anki VM ghcr read` → **Regenerate token**. It keeps
   `read:packages`. Set the expiration a year out, then copy the token.
2. On the VM, as root, the same way as in 2.6:
   ```bash
   read -rsp 'GHCR token: ' T; echo           # paste the token; it isn't echoed or saved in history
   printf %s "$T" | docker login ghcr.io -u marccowan --password-stdin; unset T
   ```
   Expect `Login Succeeded`.
3. Check with `docker pull ghcr.io/marccowan/anki-app:main`, then
   `journalctl -u anki-update --since "-5 min"`, which should have no pull errors.
4. Set the next reminder.

## 8. Backup restore drill (laptop; quarterly is a good rhythm)

This proves the off-site copies open and contain your data. It doesn't touch the VM.

1. Cloud Console → **Cloud Storage** → **Buckets** → `<BUCKET>` → `backups/daily/`. Click the newest
   `anki-app-<UTC>.db` and **Download** it.
2. On the laptop, in the repo on `main`:
   ```bash
   bash scripts/build.sh
   DRILL="$HOME/anki-drill-$(date -u +%Y%m%dT%H%M%SZ)"; mkdir "$DRILL"
   mv "$HOME/Downloads/anki-app-<UTC>.db" "$DRILL/anki-app.db"
   DB_PATH="$(cygpath -w "$DRILL/anki-app.db")" ADDR=127.0.0.1:18080 server/anki-app serve
   ```
3. Open http://127.0.0.1:18080. No passphrase is set, so it opens directly. The decks and counts should match the
   VM as of that backup. Stop the server with Ctrl-C.
4. Clean up by exact names:
   1. Delete the drill database and any `-wal`/`-shm` next to it:
      `rm -f "$DRILL/anki-app.db" "$DRILL/anki-app.db-wal" "$DRILL/anki-app.db-shm"`.
   2. If `serve` made a backup, delete it: `ls "$DRILL/backups/daily"`, then `rm` that one file by name.
   3. Remove the folders that exist: `rmdir "$DRILL/backups/daily" "$DRILL/backups/pre-deploy" "$DRILL/backups" "$DRILL"`.

## 9. Claude connector (MCP)

Design: [docs/specs/2026-11-08-mcp-connector-design.md](../specs/2026-11-08-mcp-connector-design.md). It lets
Claude (claude.ai) list, read and search your cards and add new ones. It can't edit or delete anything. Only
the connector's paths are put on the internet, through Tailscale Funnel on port **443**. The web UI is on
**8443** and stays private to the tailnet (§2.3).

**Why 443.** Claude's servers only connect to MCP servers on port 443. On any other port, claude.ai says
"Couldn't reach" and no request ever arrives (anthropics/claude-ai-mcp issue #125; found the hard way on
2026-11-14 with the connector on 8443). Funnel is switched on per port, so every path mapped on 443 is public:
**never map `/` or anything else from the web UI on 443.** The web UI lives on 8443 for that reason.

### 9.1 Turn it on (once)

1. **Tailscale admin console** → https://login.tailscale.com/admin/acls → the JSON editor (the visual editor has no
   node attributes). Add at the top level, then Save:
   ```json
   "hosts": { "anki": "100.x.y.z" },
   "nodeAttrs": [ { "target": ["anki"], "attr": ["funnel"] } ],
   ```
   `100.x.y.z` is the VM's Tailscale IP (`tailscale ip -4` on the VM). Check on the VM that it arrived:
   `tailscale status --json | grep -c funnel` should print a non-zero count.
2. **VM:** add one line to `/etc/anki-app/anki-app.env` (it stays root, 0600), then restart:
   ```bash
   echo 'MCP_PUBLIC_URL=https://anki.<tailnet>.ts.net' >> /etc/anki-app/anki-app.env
   systemctl restart anki-app
   journalctl -u anki-app -n 20 | grep listening_public   # expect addr=127.0.0.1:8081
   ```
   The app refuses to start if `MCP_PUBLIC_URL` is set without `PASSPHRASE`.
3. **VM:** check the web UI is on 8443, not 443, then publish exactly these paths on 443, all to the public
   listener on 127.0.0.1:8081:
   ```bash
   tailscale serve status          # must show / only under :8443. If / is under the bare name (443), stop: see §2.3
   for p in /mcp /.well-known/oauth-protected-resource/mcp /.well-known/oauth-authorization-server \
            /oauth/register /oauth/authorize /oauth/authorize.css /oauth/token /oauth/revoke; do
     tailscale funnel --bg --yes --https=443 --set-path="$p" "http://127.0.0.1:8081$p"
   done
   tailscale funnel status
   ```
   `tailscale funnel status` should list those 8 paths under `https://anki.<tailnet>.ts.net (Funnel on)` and,
   separately, `/` under `:8443 (tailnet only)`. A new Funnel can take a few minutes before it answers publicly.

### 9.2 Check what is public (from a machine **off** the tailnet, e.g. Cloud Shell or a phone on mobile data)

```bash
U=https://anki.<tailnet>.ts.net
curl -s -o /dev/null -w '%{http_code}\n' "$U/.well-known/oauth-authorization-server"         # 200
curl -s -o /dev/null -w '%{http_code}\n' "$U/.well-known/oauth-protected-resource/mcp"       # 200
curl -s -D - -o /dev/null -X POST -H 'Content-Type: application/json' -d '{}' "$U/mcp" | grep -i -E '^HTTP|www-authenticate'
                                                                  # 401 and WWW-Authenticate: Bearer resource_metadata=...
curl -s -o /dev/null -w '%{http_code}\n' "$U/"                   # 404 (not mapped)
curl -s -o /dev/null -w '%{http_code}\n' "$U/api/health"         # 404 (not mapped)
curl -s -o /dev/null -w '%{http_code}\n' --max-time 10 "$U:8443/api/health"   # 000: 8443 isn't public
```

If any of the 404s is a 200, or 8443 answers, **stop** (`tailscale funnel --https=443 off`) and look at
`tailscale funnel status`.

A laptop on the tailnet resolves the name to the VM's tailnet address, so its results say nothing about the
public path. To test the public path from it anyway, pin a public address:
`curl --resolve anki.<tailnet>.ts.net:443:<ip from 'nslookup anki.<tailnet>.ts.net 1.1.1.1'> ...`.

### 9.3 Connect Claude

1. claude.ai → **Settings** → **Connectors** → **Add custom connector**. Name: `anki-app`. URL:
   `https://anki.<tailnet>.ts.net/mcp` (also shown, with a Copy button, in the app under Settings → Claude
   connector). If its pre-check says "Couldn't reach", that check is unreliable: choose **Continue anyway**.
2. Settings: Authentication **Sign in now**; OAuth client **Register automatically** (the app doesn't support
   "Claude's published identity", CIMD); no request headers; Advanced → Transport **Streamable HTTP**.
3. **Connect** opens the app's consent page. Check that it says **Claude (claude.ai)**, enter the passphrase, and
   choose **Allow**. Approve only when you just clicked Connect yourself.
4. In a chat: "List my flashcard decks." Then check that Settings → Claude connector shows **Connected**.

If connecting fails:
- `journalctl -u anki-app | grep 'rejected redirect_uri'` shows whether Claude used a callback URL the app doesn't
  allow yet. The allowed list is `oauth.AllowedRedirectURIs` in `server/internal/oauth/oauth.go`.
- The app doesn't log successful public requests. To see whether Claude's servers reach the VM at all, watch the
  public listener's request lines for a few minutes while you retry (headers only; bodies aren't printed):
  `timeout 300 tcpdump -i lo -l -A 'tcp dst port 8081' | grep -aoE '(GET|POST) /[^ ]* HTTP|^User-Agent:.*'`.
  Claude's servers show up as `python-httpx` (sign-in) and `Claude-User` (`/mcp`). Nothing at all means the
  problem is before the VM: port, DNS (Claude connects over IPv4 only, so the name needs a public A record), or
  Funnel.

### 9.4 Day to day

- **A bad batch:** Settings → Claude connector → Recent batches → **Delete batch**. That deletes that call's cards,
  including any you've edited since.
- **Limits:** 50 cards per call and 200 cards per rolling 24 hours (all connections together). Claude gets a
  message saying how many are left and when more free up.

### 9.5 Kill switches

| Action | Effect |
|---|---|
| Settings → **Disconnect Claude** | Revokes every connection and token at once. Claude has to sign in again. |
| `tailscale funnel --https=443 off` (VM) | Nothing is public any more. The web UI on 8443 is untouched. Turn it back on with 9.1 step 3. |
| Remove `MCP_PUBLIC_URL` from `/etc/anki-app/anki-app.env`, then `systemctl restart anki-app` | The public listener doesn't start at all. |
