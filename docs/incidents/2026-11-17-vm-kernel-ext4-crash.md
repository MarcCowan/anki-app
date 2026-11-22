# 2026-11-17: VM down for 3.5 days after an automatic kernel update

| | |
|---|---|
| **Status** | Resolved (follow-ups open) |
| **Severity** | SEV-2: full outage, no data loss |
| **Duration** | 2026-11-17 ~07:51 UTC → 2026-11-20 22:23 UTC (about 3 days 14.5 hours) |
| **Detected by** | Marc, by hand, when the web UI wouldn't load (no alert fired) |
| **Affected** | Web UI (tailnet :8443), Claude connector (Funnel :443 `/mcp`), deploys, backups. Until 11-21: off-site copies, deploys and security updates (VM DNS broken, see [Follow-up](#follow-up-vm-dns-broken-until-2026-11-21)) |
| **Data loss** | None. Last DB write 2026-11-17 04:30, before the fault; `PRAGMA integrity_check` = `ok` |

## Summary

On 2026-11-16 unattended-upgrades installed Debian kernel `6.1.0-55-cloud-amd64` (6.1.192-1). The scheduled
04:30 reboot on 2026-11-17 booted into it. About 3h20m later, the kernel hit a NULL pointer dereference in the
ext4 block allocator (`ext4_mb_load_buddy_gfp`). PID 1 was killed and the kernel panicked. From then on, it hit the
same fault 35–45 s into every boot, as soon as Docker started writing to disk, and the guest hung. GCE still
reported the instance as `RUNNING`, Tailscale still answered pings from memory, and nothing alerted.

We repaired it by attaching the boot disk to a temporary rescue VM, running `e2fsck`, and removing the new kernel
so the VM boots the previous one (`6.1.0-54`).

## Timeline (UTC)

| Time | Event |
|---|---|
| 11-16 06:49 | unattended-upgrades installs `linux-image-6.1.0-55-cloud-amd64` and upgrades the `linux-image-cloud-amd64` metapackage. |
| 11-17 04:30 | App writes the DB for the last time (clean shutdown, empty WAL). Scheduled reboot into kernel 55. |
| 11-17 04:30:08 | During the shutdown, tailscaled fails to restart systemd-resolved (`exit status 4`) and falls back to "direct" DNS mode with no upstream resolver. Not noticed until 11-21 (see [Follow-up](#follow-up-vm-dns-broken-until-2026-11-21)). |
| 11-17 ~07:51 | **Outage starts.** Oops in `ext4_mb_load_buddy_gfp` at uptime 12011 s → `Kernel panic - Attempted to kill init!` → reboot. |
| 11-17 07:51:56 | The same oops 35 s into the new boot (`dockerd`). The guest hangs: journald and timesyncd can't be killed. |
| 11-17 11:31 | Tailscale control plane last sees `anki`. |
| 11-20 evening | Marc can't load the web UI. Diagnosis: `tailscale status` shows `anki` offline since 11-17; ping and SSH time out. |
| 11-20 | `gcloud ... describe` gives a 404: Cloud Shell was on the default project, not the app's. Switched with `source ~/anki-vars`. Instance is `RUNNING`. |
| 11-20 | Serial log shows the oops twice, identical, in the ext4 allocator. |
| 11-20 21:46 | `gcloud compute instances reset`. The VM rejoins the tailnet, then oopses again at 43 s (21:47:04). Points to a deterministic fault, not a one-off. |
| 11-20 ~22:00 | Stop, snapshot `anki-pre-fsck-20261120`, detach the boot disk. |
| 11-20 22:06 | Rescue VM (Debian 13, no external IP, no service account) runs `e2fsck` via a startup script. Read-only pass (journal not replayed) shows damage in `/var/lib/docker`. Repair pass replays the journal; only the free block/inode counts are wrong, and they're fixed. Verify pass is clean. |
| 11-20 ~22:12 | Read-only inspection of the disk: kernel 55 arrived 11-16, the day before the first crash. Kernel 54 had run since at least 11-07. |
| 11-20 ~22:18 | In a chroot on the rescue VM: `apt-get remove linux-image-6.1.0-55-cloud-amd64 linux-image-cloud-amd64`. GRUB regenerated with only kernel 54. |
| 11-20 22:23 | Disk reattached to `anki` and booted on `6.1.0-54`. App listening at 22:23:38; startup backup written. |
| 11-20 22:26 | **Resolved.** No oops after 3+ min, services active, `integrity_check` ok (105 cards, 2 decks), web UI 200. |
| 11-21 04:01 | Nightly `anki-offsite` fails: `Failed to resolve 'storage.googleapis.com'`. No alert. |
| 11-21 20:11 | Found while preparing other VM work: off-site copy fails again; `getent hosts storage.googleapis.com` fails on the VM. |
| 11-21 20:12 | **DNS fixed.** `/etc/resolv.conf` pointed back at the systemd-resolved stub and tailscaled restarted (now in `systemd-resolved` mode). Off-site copy succeeds; the updater deploys `6621adb`, the first deploy since 11-17. |

## Root cause

**Most likely a regression in Debian kernel 6.1.192-1 (`6.1.0-55-cloud-amd64`)** in the ext4 multi-block
allocator, hit by Docker's overlay writes. The evidence:

- All three oopses have the same `RIP: ext4_mb_load_buddy_gfp+0xbc/0x380`, NULL dereference.
- The first oops was ~3h20m after the first boot into kernel 55. The previous kernel ran for 10+ days with the same
  workload.
- After journal replay the filesystem was essentially clean (only free-count mismatches, normal after a crash).
  So the on-disk damage was a result of the crashes, not their cause.

Confirmed on 2026-11-21: Debian bug #1154101 reports the same oops on 6.1.192-1, fixed in 6.1.192-2 (`6.1.0-56`). The
VM has run 56 cleanly since 2026-11-22 (action item 2).

### Contributing factors

- **Automatic kernel updates with an automatic reboot, and no fallback.** A bad kernel is installed and booted
  with no one watching, and nothing reverts it if it fails.
- **No outage alerting.** The app was down for 3.5 days before anyone noticed.
- **GCE `RUNNING` doesn't mean healthy.** The hypervisor view looked fine, and Tailscale still answered pings
  from memory.
- **Wrong default gcloud project in Cloud Shell** gave a misleading 404 at first ("instance not found").

## What went well

- The runbook's emergency-access section (serial log, project vars in `~/anki-vars`) gave a quick way in.
- The rescue VM ran its jobs via startup scripts and was read through the serial log, so no firewall rules, SSH keys,
  or external IP were needed.
- We took a snapshot before touching the disk. The DB's WAL design meant there was nothing to lose.
- The first `e2fsck` pass was read-only, and a separate read-only inspection followed. We confirmed the cause before
  changing anything.

## What went badly

- Detection took 3.5 days and relied on Marc trying to use the app.
- The first reset just walked into the same crash. In hindsight the repeatable oops already pointed at the kernel.

## Action items

| # | Action | Type | Status |
|---|---|---|---|
| 1 | **Outage alerting.** The tailnet-only UI can't be checked from outside. Options: a GCP uptime check on the public Funnel `/mcp` (expect 401/405, alert on 5xx or timeout), or a heartbeat from the VM to a dead-man's-switch service. | Detect | Open |
| 2 | **Re-enable kernel updates.** Installed `linux-image-6.1.0-56-cloud-amd64` (6.1.192-2, which carries the ext4 fix for Debian bug #1154101) and the `linux-image-cloud-amd64` metapackage, booted it once with 54 as the GRUB fallback, and soaked it for 19 hours with no BUG/Oops. Then GRUB went back to "newest kernel", so unattended upgrades install and boot security kernels at 04:30 again. Plan: `docs/plans/2026-11-21-vm-kernel-update.md`. | Mitigate | Done 11-22 |
| 3 | **Safer kernel reboots.** Boot a new kernel once (`grub-reboot`) and only make it the default once it has stayed up, so a bad kernel falls back by itself. Higher priority now that reboots are automatic again (item 2). | Prevent | Open |
| 4 | **Runbook:** add a "VM hung, `RUNNING` but off the tailnet" section that covers the serial-log oops check and the rescue-VM procedure used here. | Process | Open |
| 5 | Delete snapshot `anki-pre-fsck-20261120` after a week of stable running (from 2026-11-27): `gcloud compute snapshots delete anki-pre-fsck-20261120`. | Cleanup | Open |
| 6 | Turn the Compute API back off in the default Cloud Shell project, where it was enabled by accident while diagnosing. | Cleanup | Open |
| 7 | **Fix VM DNS.** Point `/etc/resolv.conf` back at `/run/systemd/resolve/stub-resolv.conf` and restart tailscaled. | Mitigate | Done 11-21 |
| 8 | **Alert on failed units.** `anki-offsite` and `anki-update` failed for days without anyone knowing. Fold into item 1: report `systemctl --failed` (or those two units) through the same alert. | Detect | Open |
| 9 | **`setup.sh` checks DNS mode.** Make `deploy/vm/setup.sh` check that `/etc/resolv.conf` is a symlink to the systemd-resolved stub and that `getent hosts storage.googleapis.com` works, and add both to the runbook's post-reboot checks. | Prevent | Open |
| 10 | Delete snapshot `anki-pre-k56-20261122` (taken before the kernel 56 boot) after a week of stable running (from 2026-11-29): `gcloud compute snapshots delete anki-pre-k56-20261122`. | Cleanup | Open |

## Follow-up: VM DNS broken until 2026-11-21

**What happened.** During the 04:30 shutdown on 11-17, tailscaled was stopped and tried to restart
systemd-resolved, which failed (`exit status 4`). It then found `/etc/resolv.conf` pointing at the upstream resolver
rather than the systemd-resolved stub, logged `dns: using "direct" mode`, and wrote `/etc/resolv.conf` itself:
`nameserver 100.100.100.100` (MagicDNS) only. In direct mode it also couldn't read the system's DNS settings
(`tailscale dns status`: `failed to read system DNS configuration`), so MagicDNS had no upstream for names outside
`ts.net` and answered them with SERVFAIL. Tailnet names, and the metadata server via `/etc/hosts`, still worked.
That's why SSH and the web UI looked healthy after the kernel fix.

**Effect, 11-17 → 11-21 (4.5 days):**

- Off-site copies to GCS failed: the 11-21 04:00 run with a DNS error. (The 11-20 21:46 catch-up run was cut off
  by the 21:47 oops, before DNS mattered.) Local backups kept working.
- The updater couldn't reach GHCR, so `main` (#17, #19) didn't deploy.
- `apt` couldn't refresh its package lists. unattended-upgrades logged `No packages found that can be upgraded`
  every day, which looks like "up to date" but was really "can't see the archive". No security updates installed.

**Why it wasn't caught.** The rescue on 11-20 checked the app, the web UI and the DB, all of which only need the
tailnet. Nothing checked outbound DNS, and failed units don't alert (action item 8).

**Fix (11-21).** `ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf`, then restart tailscaled. It now
logs `dns: using "systemd-resolved" mode` and sets MagicDNS only on the `tailscale0` link, so every other name
goes to the GCE resolver. The direct-mode file is kept at `/root/resolv.conf.direct-mode-2026-11-17`.

**Not established:** why `/etc/resolv.conf` pointed at `/run/systemd/resolve/resolv.conf` instead of the stub
before 11-17. The answer decides whether a fresh VM from the runbook starts in the same fragile state (action
item 9 covers it either way).

## Rescue procedure used (for reuse)

Everything ran from Cloud Shell after `source ~/anki-vars && gcloud config set project "$PROJECT_ID"`.

1. `gcloud compute instances stop anki`, then snapshot the boot disk, then `detach-disk anki --disk=anki`.
2. Create a rescue VM on a **different image family** (`debian-13`). Disks from the same image share a root
   filesystem UUID, so the rescue VM could otherwise boot from the patient disk. Attach the patient disk with
   `--disk=name=anki,device-name=sick,boot=no,auto-delete=no`, plus `--no-address --no-service-account --no-scopes`.
3. Put each job in `startup-script` metadata and `reset` the rescue VM to run it. Read the output with
   `get-serial-port-output | grep startup-script`. The patient root is `/dev/disk/by-id/google-sick-part1`
   and the EFI partition is `-part15`.
4. For package changes: mount root and EFI, bind-mount `dev dev/pts proc sys run`, and `chroot` in.
5. Detach the disk from the rescue VM, delete the rescue VM, `attach-disk anki --disk=anki --boot`, then start.
