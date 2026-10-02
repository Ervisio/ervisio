#!/bin/bash
# Packs an already built tree into the release archives:
#
#   packaging/build-release.sh VERSION ARCH OUTDIR
#   -> OUTDIR/ervisio-VERSION-linux-ARCH.tar.gz
#      OUTDIR/linuxadmin-VERSION-linux-ARCH.tar.gz   (compatibility archive)
#
# Expects server/bin/{ervisiod,ervisio-bridge} built for ARCH with
# brand.Version = VERSION (make build-server VERSION=...) and web/dist built.
# The archive holds one folder, ervisio-VERSION-linux-ARCH/, with
# bin/, web/, plugins/, packaging/, install.sh and VERSION; only regular files and
# folders (the updater refuses anything else). Used by
# .github/workflows/release.yml; see docs/RELEASING.md.
#
# The compatibility archive is what consoles still running LinuxAdmin (the
# product's name up to 0.2.0) download when they update themselves: their
# updater asks for linuxadmin-VERSION-linux-ARCH.tar.gz with
# bin/linuxadmind and bin/linuxadmin-bridge in a linuxadmin-VERSION-linux-ARCH/
# folder. It holds the same files, with the binaries under those names, plus
# packaging/linuxadmin.service and pam.d/linuxadmin.* for LinuxAdmin's
# install.sh. The new binary, started from there, moves the installation to
# Ervisio (docs/RELEASING.md, "Rename transition").
set -euo pipefail
cd "$(dirname "$0")/.."

[ $# -eq 3 ] || { echo "usage: $0 VERSION ARCH OUTDIR"; exit 2; }
VERSION="$1" ARCH="$2" OUT="$3"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "VERSION must be x.y.z or x.y.z-pre (no leading v)"; exit 2; }
[[ "$ARCH" =~ ^(amd64|arm64)$ ]] || { echo "ARCH must be amd64 or arm64"; exit 2; }

for f in server/bin/ervisiod server/bin/ervisio-bridge web/dist/index.html; do
  [ -f "$f" ] || { echo "missing $f"; exit 1; }
done
# Check the binaries carry VERSION (they can only run on a host of the same arch).
case "$(uname -m)" in x86_64) HOST=amd64 ;; aarch64|arm64) HOST=arm64 ;; *) HOST=other ;; esac
if [ "$HOST" = "$ARCH" ]; then
  for b in ervisiod ervisio-bridge; do
    got="$(server/bin/$b --version)"
    [ "$got" = "$VERSION" ] || { echo "server/bin/$b reports '$got', expected '$VERSION'"; exit 1; }
  done
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
MTIME="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || date +%s)}"
mkdir -p "$OUT"

# pack NAME: checks $STAGE/NAME and writes OUT/NAME.tar.gz
pack() {
  local name="$1" d="$STAGE/$1"
  chmod -R u+rwX,go+rX,go-w "$d"
  if [ -n "$(find "$d" ! -type f ! -type d | head -n1)" ]; then
    echo "release tree contains symlinks or special files:"; find "$d" ! -type f ! -type d; exit 1
  fi
  tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$MTIME" --format=gnu \
    -C "$STAGE" -cf - "$name" | gzip -n -9 > "$OUT/$name.tar.gz"
  echo "$OUT/$name.tar.gz"
}

NAME="ervisio-$VERSION-linux-$ARCH"
D="$STAGE/$NAME"
mkdir -p "$D/bin" "$D/plugins" "$D/packaging"
install -m755 server/bin/ervisiod server/bin/ervisio-bridge "$D/bin/"
cp -r web/dist "$D/web"
# plugins/ stays empty: plugins (Docker included, up to 0.3.0 shipped here)
# come from the signed marketplace catalog (docs/api/plugins.md).
cp -r packaging/ervisio.service packaging/pam.d packaging/install.sh packaging/README.md "$D/packaging/"
# The installer; packaging/install.sh runs it with --from this folder.
install -m755 install.sh "$D/install.sh"
install -m644 LICENSE "$D/LICENSE"
printf '%s\n' "$VERSION" > "$D/VERSION"
pack "$NAME"

# The compatibility archive for LinuxAdmin consoles.
LNAME="linuxadmin-$VERSION-linux-$ARCH"
L="$STAGE/$LNAME"
cp -r "$D" "$L"
mv "$L/bin/ervisiod" "$L/bin/linuxadmind"
mv "$L/bin/ervisio-bridge" "$L/bin/linuxadmin-bridge"
# For LinuxAdmin's install.sh (--version VERSION): the unit it writes runs
# /usr/bin/linuxadmind, which then moves itself to ervisio.service.
sed -e 's/^Description=Ervisio web console/Description=LinuxAdmin web console (moving to Ervisio)/' \
  -e 's#^ExecStart=/usr/bin/ervisiod#ExecStart=/usr/bin/linuxadmind#' \
  -e '/^Conflicts=/d' -e '/^# Ervisio was called LinuxAdmin/,/^# going back to it/d' \
  packaging/ervisio.service > "$L/packaging/linuxadmin.service"
for f in packaging/pam.d/ervisio.*; do
  sed -e 's#PAM service for Ervisio sign-in (/etc/pam.d/ervisio)#PAM service for LinuxAdmin sign-in (/etc/pam.d/linuxadmin)#' \
    -e "s/ervisiod's root session helper/linuxadmind's root session helper/" "$f" > "$L/packaging/pam.d/linuxadmin.${f##*.}"
done
pack "$LNAME"
