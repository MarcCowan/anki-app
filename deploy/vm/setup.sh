#!/usr/bin/env bash
# setup.sh: host setup for the anki-app VM (spec §7.2; docs/runbooks/vm.md section 2.1). Run as root
# on Debian 12 from the copied folder:
#   laptop: scp -r deploy/vm root@anki:/tmp/      VM: cd /tmp/vm && bash setup.sh
# Idempotent: every step checks first and acts only when something differs, so a re-run changes
# nothing. Re-run it after any change under deploy/vm/; image pulls never update these files.
# It doesn't enable anki-app.service: the first successful deploy does (spec §7.5).
set -euo pipefail

src="$(cd "$(dirname "$0")" && pwd)"
export DEBIAN_FRONTEND=noninteractive

say() { printf 'setup: %s\n' "$*"; }
die() {
  printf 'setup: %s\n' "$*" >&2
  exit 1
}

[ "$(id -u)" = 0 ] || die "run this as root"
# shellcheck source=/dev/null
. /etc/os-release
if [ "${ID:-}" != debian ] || [ "${VERSION_ID:-}" != 12 ]; then
  die "this is for Debian 12, not ${PRETTY_NAME:-an unknown system}"
fi
codename=$VERSION_CODENAME

scripts=(lib.sh anki-update anki-restore anki-offsite)
units=(anki-app.service anki-update.service anki-update.timer anki-offsite.service anki-offsite.timer)
for f in "${scripts[@]}"; do [ -f "$src/$f" ] || die "missing $src/$f"; done
for f in "${units[@]}"; do [ -f "$src/systemd/$f" ] || die "missing $src/systemd/$f"; done

# ---- helpers -------------------------------------------------------------------------------------

APT=(apt-get -o DPkg::Lock::Timeout=300)
apt_fresh=0 # 1 once `apt-get update` has run since the last change to the sources

installed() { dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q 'ok installed'; }

# apt_install <pkg>...: installs the packages that aren't installed yet.
apt_install() {
  local p missing=()
  for p in "$@"; do
    installed "$p" || missing+=("$p")
  done
  [ ${#missing[@]} -gt 0 ] || return 0
  if [ "$apt_fresh" = 0 ]; then
    "${APT[@]}" update -q
    apt_fresh=1
  fi
  say "installing ${missing[*]}"
  "${APT[@]}" install -y -q "${missing[@]}"
}

# repo_configured <text>: an apt source (one-line or deb822) already mentions <text>. Adding a second
# entry for the same repository with a different Signed-By breaks `apt-get update`. Dotfiles are
# skipped: apt ignores them too, and a leftover temp file from tmp_next_to must not count.
repo_configured() {
  grep -rqsF --exclude='.*' -- "$1" /etc/apt/sources.list /etc/apt/sources.list.d/
}

# tmp_next_to <dest>: creates a temp file in <dest>'s folder (so the final mv is a rename on the same
# file system) and prints its path. The name is .<base>.tmp.XXXXXX: apt skips dotfiles in
# apt.conf.d/ and sources.list.d/, so a file left behind by an interrupted run is never read.
tmp_next_to() {
  mktemp "$(dirname "$1")/.$(basename "$1").tmp.XXXXXX"
}

# fetch <url> <dest>: downloads to a temp file next to <dest>, then moves it into place (mode 0644).
fetch() {
  local tmp
  tmp=$(tmp_next_to "$2")
  if ! curl -fsSL "$1" -o "$tmp"; then
    rm -f "$tmp"
    die "download failed: $1"
  fi
  chmod 0644 "$tmp"
  mv -f "$tmp" "$2"
}

# put <dest> <mode>: writes stdin to <dest>, owned by root, unless it already has that content, owner
# and mode. Sets changed=1 if it wrote. The temp file + mv means a running script keeps its old copy.
changed=0
put() {
  local dest=$1 mode=$2 tmp
  tmp=$(tmp_next_to "$dest")
  cat >"$tmp"
  if [ -f "$dest" ] && cmp -s "$tmp" "$dest" &&
    [ "$(stat -c '%u:%g %a' "$dest")" = "0:0 ${mode#0}" ]; then
    rm -f "$tmp"
    return 0
  fi
  chown 0:0 "$tmp"
  chmod "$mode" "$tmp"
  mv -f "$tmp" "$dest"
  say "wrote $dest"
  changed=1
}

# ensure_dir <path> <uid:gid> <mode>: creates the folder if needed and fixes its owner and mode. Never
# recursive: only the folder itself.
ensure_dir() {
  local path=$1 owner=$2 mode=$3
  if [ ! -d "$path" ]; then
    mkdir -p "$path"
    say "created $path"
  fi
  if [ "$(stat -c '%u:%g' "$path")" != "$owner" ]; then
    chown "$owner" "$path"
    say "chown $owner $path"
  fi
  if [ "$(stat -c '%a' "$path")" != "${mode#0}" ]; then
    chmod "$mode" "$path"
    say "chmod $mode $path"
  fi
}

# ensure_unit_on <unit>: enables and starts it, if it isn't already.
ensure_unit_on() {
  if ! systemctl is-enabled --quiet "$1"; then
    systemctl enable "$1"
    say "enabled $1"
  fi
  if ! systemctl is-active --quiet "$1"; then
    systemctl start "$1"
    say "started $1"
  fi
}

# ---- packages ------------------------------------------------------------------------------------

apt_install ca-certificates curl jq unattended-upgrades

# Docker Engine, from Docker's apt repository.
if ! repo_configured download.docker.com; then
  install -d -m 0755 /etc/apt/keyrings
  fetch https://download.docker.com/linux/debian/gpg /etc/apt/keyrings/docker.asc
  put /etc/apt/sources.list.d/docker.list 0644 <<EOF
deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $codename stable
EOF
  apt_fresh=0
fi
apt_install docker-ce docker-ce-cli containerd.io
ensure_unit_on docker.service

# Tailscale: normally already installed by the VM's startup-script bootstrap (runbook 1.8), with the
# same keyring and list file names as here.
if ! command -v tailscale >/dev/null 2>&1; then
  if ! repo_configured pkgs.tailscale.com; then
    install -d -m 0755 /usr/share/keyrings
    fetch "https://pkgs.tailscale.com/stable/debian/$codename.noarmor.gpg" \
      /usr/share/keyrings/tailscale-archive-keyring.gpg
    fetch "https://pkgs.tailscale.com/stable/debian/$codename.tailscale-keyring.list" \
      /etc/apt/sources.list.d/tailscale.list
    apt_fresh=0
  fi
  apt_install tailscale
fi

# Google Cloud CLI: GCE's Debian images ship it. Install it only if it's missing.
if ! command -v gcloud >/dev/null 2>&1; then
  if ! repo_configured packages.cloud.google.com/apt; then
    key_tmp=$(mktemp)
    if ! curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg -o "$key_tmp"; then
      rm -f "$key_tmp"
      die "download failed: the Google Cloud apt key"
    fi
    # apt reads an armored key only from a .asc file and a binary one only from a .gpg file.
    key=/usr/share/keyrings/cloud.google.gpg
    if [ "$(head -c 5 "$key_tmp")" = "-----" ]; then key=/usr/share/keyrings/cloud.google.asc; fi
    install -m 0644 "$key_tmp" "$key"
    rm -f "$key_tmp"
    put /etc/apt/sources.list.d/google-cloud-sdk.list 0644 <<EOF
deb [signed-by=$key] https://packages.cloud.google.com/apt cloud-sdk main
EOF
    apt_fresh=0
  fi
  apt_install google-cloud-cli
fi

# ---- time zone and automatic security updates ----------------------------------------------------

# London time, so the 04:30 reboot and the 04:00 off-site copy are London times.
if [ "$(timedatectl show -p Timezone --value)" != Europe/London ]; then
  timedatectl set-timezone Europe/London
  say "time zone set to Europe/London"
fi

put /etc/apt/apt.conf.d/20auto-upgrades 0644 <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF
put /etc/apt/apt.conf.d/52anki-app-unattended-upgrades 0644 <<'EOF'
// anki-app (deploy/vm/setup.sh): reboot after updates that need it, at 04:30 London time.
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "04:30";
EOF

# ---- tuning --------------------------------------------------------------------------------------

# A 1 GB swap file: 1 GB of RAM is tight while pulling images. If it isn't active, it's (re)made
# before swapon: a run interrupted after fallocate leaves a file without a swap signature, and one
# cut short in fallocate leaves the wrong size. Neither is in use, so remaking it loses nothing.
swap_bytes=1073741824
if ! swapon --show=NAME --noheadings | grep -qx /swapfile; then
  if [ -f /swapfile ] && [ "$(stat -c %s /swapfile)" != "$swap_bytes" ]; then
    rm -f /swapfile
    say "removed /swapfile: wrong size"
  fi
  if [ ! -f /swapfile ]; then
    fallocate -l "$swap_bytes" /swapfile
    say "created /swapfile"
  fi
  chmod 0600 /swapfile
  mkswap /swapfile >/dev/null
  swapon /swapfile
  say "swap on: /swapfile"
fi
if ! grep -qE '^/swapfile[[:space:]]' /etc/fstab; then
  printf '/swapfile none swap sw 0 0\n' >>/etc/fstab
  say "added /swapfile to /etc/fstab"
fi

install -d -m 0755 /etc/systemd/journald.conf.d
changed=0
put /etc/systemd/journald.conf.d/50-anki-app.conf 0644 <<'EOF'
[Journal]
SystemMaxUse=500M
EOF
if [ "$changed" = 1 ]; then
  systemctl restart systemd-journald
  say "restarted systemd-journald"
fi

# ---- folders, scripts and units (spec §7.3) ------------------------------------------------------

ensure_dir /etc/anki-app 0:0 0755
ensure_dir /opt/anki-app 0:0 0755
ensure_dir /opt/anki-app/bin 0:0 0755
ensure_dir /var/lib/anki-app 0:0 0755
ensure_dir /var/lib/anki-app/data 65532:65532 0700
ensure_dir /var/lib/anki-app/deploy 0:0 0755

for f in "${scripts[@]}"; do
  mode=0755
  if [ "$f" = lib.sh ]; then mode=0644; fi # sourced, never run
  put "/opt/anki-app/bin/$f" "$mode" <"$src/$f"
done

units_changed=()
for u in "${units[@]}"; do
  changed=0
  put "/etc/systemd/system/$u" 0644 <"$src/systemd/$u"
  if [ "$changed" = 1 ]; then units_changed+=("$u"); fi
done
if [ ${#units_changed[@]} -gt 0 ]; then
  systemctl daemon-reload
  say "systemctl daemon-reload"
fi

for u in "${units_changed[@]}"; do
  case $u in
    *.timer)
      # A timer that was already running picks up its new schedule on restart.
      if systemctl is-active --quiet "$u"; then
        systemctl restart "$u"
        say "restarted $u"
      fi
      ;;
    anki-app.service)
      if systemctl is-active --quiet anki-app; then
        say "anki-app.service changed: run 'systemctl restart anki-app' to apply it"
      fi
      ;;
  esac
done
for t in anki-update.timer anki-offsite.timer; do
  ensure_unit_on "$t"
done

# ---- what's left ---------------------------------------------------------------------------------

mark() { if "$@" >/dev/null 2>&1; then printf '[x]'; else printf '[ ]'; fi; }
serving() { tailscale serve status 2>/dev/null | grep -qF 127.0.0.1:8080; }
logged_in() { grep -qsF '"ghcr.io"' /root/.docker/config.json; }

cat <<EOF

setup: done. The manual steps that remain (docs/runbooks/vm.md section 2, in this order):
  $(mark tailscale status) 2.2 tailscale up --ssh --hostname anki (the bootstrap did this), then turn
      off key expiry for 'anki' at https://login.tailscale.com/admin/machines
  $(mark serving) 2.3 tailscale serve --bg --https=8443 http://127.0.0.1:8080 (443 is kept for the Claude connector's Funnel)
  $(mark test -f /etc/anki-app/anki-app.env) 2.4 write /etc/anki-app/anki-app.env (root, 0600): PASSPHRASE, ALLOWED_HOSTS, TZ
  $(mark test -f /etc/anki-app/offsite.env) 2.5 write /etc/anki-app/offsite.env (root, 0644): BUCKET=<name>
  $(mark logged_in) 2.6 docker login ghcr.io (read:packages token) - do this last: it arms the updater

Then section 3: watch the first deploy with 'journalctl -u anki-update -f'.
Deploy a specific build: touch /etc/anki-app/paused first, then
  /opt/anki-app/bin/anki-update ghcr.io/marccowan/anki-app:sha-<12>
(without the pause, the timer deploys :main again within a minute; rm the file to resume).
EOF
