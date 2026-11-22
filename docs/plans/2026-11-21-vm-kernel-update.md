# VM kernel update (6.1.0-56) Implementation Plan

Status: done 2026-11-22. Reviewed with Marc before running. Steps 1-6 as written. The soak ran 19 hours, not 24, with
no BUG/Oops. Step 7 changed: instead of pinning 56 with `grub-set-default`, `GRUB_DEFAULT` went back to `0` (newest
kernel), so unattended upgrades boot new security kernels at 04:30 again. The trial-boot follow-up below is still open
(incident action item 3). Snapshot: `anki-pre-k56-20261122`.

**Goal:** Get Debian kernel security updates flowing again on the VM, after the 2026-11-17 ext4 crash
(`docs/incidents/2026-11-17-vm-kernel-ext4-crash.md`, action items 2 and 3), without risking another blind outage.

**Context:** The VM runs `6.1.0-54-cloud-amd64` (6.1.190-1). Kernel 55 and the `linux-image-cloud-amd64` metapackage
are removed, so no kernel updates arrive. Runbook: `docs/runbooks/vm.md`. All commands run on the VM as root
(`ssh root@anki`) unless noted.

## The fix (checked 2026-11-21)

- Verified: Debian source `linux 6.1.192-2` in **bookworm-security** (urgency=high, dated 2026-11-16) has the single
  entry "ext4: fix NULL folio dereference in mballoc (Closes: #1154101)". Binary: `linux-image-6.1.0-56-cloud-amd64`.
  Sources: `https://salsa.debian.org/kernel-team/linux/-/raw/debian/6.1.192-2/debian/changelog`,
  `https://bugs.debian.org/1154101`, `https://tracker.debian.org/pkg/linux`.
- Verified: bug #1154101 reports `6.1.0-55` (6.1.192-1) oopsing with a NULL dereference in `ext4_mb_load_buddy_gfp`,
  in the block-free path on file close, "fixed in linux/6.1.192-2".
- Verified on the VM after `apt-get update`: `linux-image-cloud-amd64` candidate is `6.1.192-2`.
- Not verified: the upstream commit hash (the bug page links a linux-ext4 "ext4-ntfs3-null-folio" series and Debian
  MR 2168). Nobody has run 6.1.0-56 on this VM, so treat the first boot as a test. The fix is not in the plain
  `bookworm` suite yet (still 6.1.176-1).

## Steps

1. **Snapshot** (Cloud Shell, `source ~/anki-vars`):
   `gcloud compute disks snapshot anki --snapshot-names=anki-pre-k56-20261121`.
2. **Pause deploys:** `touch /etc/anki-app/paused`. Do this in daytime, with time to watch the box.
3. **Install without rebooting:**
   `apt-get update && apt-get install linux-image-6.1.0-56-cloud-amd64 linux-image-cloud-amd64`
   (this also restores the metapackage). Check `dpkg -l 'linux-image*'` and that `/boot` has
   `vmlinuz-6.1.0-56-cloud-amd64`. If unattended-upgrades reboots at 04:30 before step 4 is done, it boots 56 with
   no fallback, so finish steps 3-5 the same day.
4. **Pin the known-good kernel as default.** `GRUB_DEFAULT=0` means "newest", so after step 3 any reset
   (including a reset of a hung guest) boots 56. Fix that first:
   ```
   sed -i 's/^GRUB_DEFAULT=.*/GRUB_DEFAULT=saved/' /etc/default/grub
   update-grub
   U=$(grep -oP 'gnulinux-advanced-\K[0-9a-f-]+' /boot/grub/grub.cfg | head -1)
   grep -n "gnulinux-6.1.0-5[46]-cloud-amd64-advanced-$U" /boot/grub/grub.cfg   # both entries must exist
   grub-set-default "gnulinux-advanced-$U>gnulinux-6.1.0-54-cloud-amd64-advanced-$U"
   grub-reboot      "gnulinux-advanced-$U>gnulinux-6.1.0-56-cloud-amd64-advanced-$U"
   grub-editenv list   # saved_entry is 54, next_entry is 56
   ```
5. **One-shot boot:** `systemctl stop anki-app` (clean WAL), then `reboot`.
6. **Checks after boot** (SSH back in within about 2 minutes):
   ```
   uname -r                          # 6.1.0-56-cloud-amd64
   dmesg | grep -E "BUG:|Oops"       # no output
   systemctl is-active docker anki-app
   curl -s -H 'Host: localhost' http://127.0.0.1:8080/api/health
   ```
   The old crash came 35-45 s after boot (dockerd writes), and once after 3h20m. Soak: force Docker writes
   (`docker pull hello-world; docker system df`), run `systemctl start anki-offsite.service`, and re-check dmesg
   after 10 minutes, 1 hour and 24 hours. If the guest hangs, reset it from GCE: it falls back to 54 because step 4
   pinned it.
7. **Make it the default once stable** (24 h, no BUG/Oops):
   `grub-set-default "gnulinux-advanced-$U>gnulinux-6.1.0-56-cloud-amd64-advanced-$U"` (recompute `U` as in step 4),
   then `rm /etc/anki-app/paused`. Delete the snapshot after a week.

## Rollback

- Booted but misbehaving: `grub-reboot` is already spent, so a plain `reboot` returns to 54 (the saved default).
- Hung or panicking: `gcloud compute instances reset anki`. It boots 54. Read
  `gcloud compute instances get-serial-port-output anki` for the oops.
- 54 also fails to boot: use the rescue-VM procedure in the incident report (snapshot, detach boot disk, chroot,
  remove 56).
- Once off 56 for good: `apt-get remove linux-image-6.1.0-56-cloud-amd64 linux-image-cloud-amd64` and
  `apt-mark hold linux-image-cloud-amd64` so the metapackage doesn't pull it back.

## Other pending updates (not part of this change)

`apt list --upgradable` shows 12 packages (checked 2026-11-21). The only Debian-security item is the kernel above.
The rest are docker-ce and its plugins, containerd.io, tailscale, and five Google guest/cloud-sdk packages. Do those
separately, not in the same reboot window.

## Follow-up: safe reboots for every future kernel

Unattended-upgrades (Debian security origin, `Automatic-Reboot "true"` at 04:30) will install the next kernel and
boot it unattended. Replace that with a pre-reboot hook that does `grub-reboot` into the newest kernel only (default
stays on the last known-good one), a post-boot check that runs the step 6 checks, and `grub-set-default` only on
success. Pair it with outage alerting (incident action item 1). Not done here.
