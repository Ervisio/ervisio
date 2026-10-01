#!/bin/bash
# Installs LinuxAdmin from a release folder into the versioned layout used by
# self-update (docs/RELEASING.md):
#
#   /usr/lib/linuxadmin/versions/<version>/{bin,web,plugins,packaging,VERSION}
#   /usr/lib/linuxadmin/current  -> versions/<version>
#   /usr/lib/linuxadmin/previous -> versions/<version before>   (rollback)
#   /usr/bin/linuxadmind         -> /usr/lib/linuxadmin/current/bin/linuxadmind
#
# From an extracted release archive:   sudo ./packaging/install.sh
# From any release-shaped folder:      sudo ./packaging/install.sh /path/to/folder
# (packaging/install-dev.sh builds such a folder from a local build.)
#
# Re-running it installs or replaces that version and makes it current; the
# version that was current before is kept as `previous`. An old flat install
# (/usr/bin/linuxadmind as a file, /usr/share/linuxadmin/web) is replaced.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "Run with sudo."; exit 1; }

SRC="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
SRC="$(cd "$SRC" && pwd)"
LIB=/usr/lib/linuxadmin
PKG="$SRC/packaging"
[ -d "$PKG" ] || PKG="$(cd "$(dirname "$0")" && pwd)"

for f in bin/linuxadmind bin/linuxadmin-bridge web/index.html VERSION; do
  [ -f "$SRC/$f" ] || { echo "Missing $SRC/$f: not a LinuxAdmin release folder."; exit 1; }
done
if [ -n "$(find "$SRC/bin" "$SRC/web" "$SRC/plugins" -type l 2>/dev/null | head -n1)" ]; then
  echo "The release folder contains symlinks: refused."; exit 1
fi
VER="$(tr -d '[:space:]' < "$SRC/VERSION")"
if ! [[ "$VER" =~ ^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$ ]] || [[ "$VER" == *..* ]]; then
  echo "Invalid version '$VER' in $SRC/VERSION"; exit 1
fi
GOT="$("$SRC/bin/linuxadmind" --version 2>/dev/null || true)"
[ "$GOT" = "$VER" ] || [ "${GOT#v}" = "$VER" ] || { echo "bin/linuxadmind reports version '$GOT', VERSION says '$VER'."; exit 1; }

# 1. Copy the version folder (next to its destination, then rename).
install -d -m755 "$LIB/versions"
TMP="$(mktemp -d "$LIB/versions/.$VER.partial-XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
cp -r "$SRC/bin" "$SRC/web" "$TMP/"
[ -d "$SRC/plugins" ] && cp -r "$SRC/plugins" "$TMP/"
[ -d "$PKG" ] && { install -d "$TMP/packaging"; cp -r "$PKG/." "$TMP/packaging/"; }
printf '%s\n' "$VER" > "$TMP/VERSION"
chown -R root:root "$TMP"
chmod -R u+rwX,go+rX,go-w "$TMP"
chmod 755 "$TMP" "$TMP/bin/linuxadmind" "$TMP/bin/linuxadmin-bridge"

OLDDIR=""
if [ -e "$LIB/versions/$VER" ]; then
  OLDDIR="$LIB/versions/.$VER.old-$$"
  mv -T "$LIB/versions/$VER" "$OLDDIR"
fi
mv -T "$TMP" "$LIB/versions/$VER"
trap - EXIT

# 2. Switch `current` atomically, keeping the version before as `previous`.
setlink() { ln -sfn "$2" "$1.tmp-$$" && mv -T "$1.tmp-$$" "$1"; }
CUR="$(readlink "$LIB/current" 2>/dev/null || true)"
CUR="${CUR#versions/}"
if [ -n "$CUR" ] && [ "$CUR" != "$VER" ] && [ -d "$LIB/versions/$CUR" ]; then
  setlink "$LIB/previous" "versions/$CUR"
fi
setlink "$LIB/current" "versions/$VER"
# Entry points follow `current`. The bridge path is kept for unit files
# written by older versions (ExecStart=... --bridge /usr/lib/linuxadmin/linuxadmin-bridge).
setlink /usr/bin/linuxadmind "$LIB/current/bin/linuxadmind"
setlink "$LIB/linuxadmin-bridge" "current/bin/linuxadmin-bridge"

# 3. Leftovers of the flat layout (the web app and plugins now live in the version folder).
rm -rf /usr/share/linuxadmin/web /usr/share/linuxadmin/plugins
rmdir /usr/share/linuxadmin 2>/dev/null || true

# 4. Keep current + previous only.
PREV="$(readlink "$LIB/previous" 2>/dev/null || true)"
PREV="${PREV#versions/}"
for d in "$LIB"/versions/* "$LIB"/versions/.*; do
  n="$(basename "$d")"
  case "$n" in .|..|"$VER"|"$PREV") continue ;; esac
  [ -d "$d" ] && rm -rf -- "$d"
done
[ -n "$OLDDIR" ] && rm -rf -- "$OLDDIR"

# 5. State folders, PAM service, systemd unit.
install -d -m755 /var/lib/linuxadmin /var/lib/linuxadmin/updates
install -d -m700 /var/lib/linuxadmin/updates/staging
install -d -m755 /etc/linuxadmin
install -Dm644 "$PKG/pam.d/linuxadmin" /etc/pam.d/linuxadmin
install -Dm644 "$PKG/linuxadmin.service" /etc/systemd/system/linuxadmin.service

systemctl daemon-reload
systemctl enable linuxadmin.service >/dev/null 2>&1
systemctl restart linuxadmin.service
sleep 1
systemctl --no-pager --lines=5 status linuxadmin.service || true
echo
echo "LinuxAdmin $VER installed${PREV:+ (previous version kept for rollback: $PREV)}."
echo "Open https://localhost:9090 (self-signed certificate: accept the browser warning)."
