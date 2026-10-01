#!/bin/bash
# Packs an already built tree into a release archive:
#
#   packaging/build-release.sh VERSION ARCH OUTDIR
#   -> OUTDIR/linuxadmin-VERSION-linux-ARCH.tar.gz
#
# Expects server/bin/{linuxadmind,linuxadmin-bridge} built for ARCH with
# brand.Version = VERSION (make build-server VERSION=...) and web/dist built.
# The archive holds one folder, linuxadmin-VERSION-linux-ARCH/, with
# bin/, web/, plugins/, packaging/, install.sh and VERSION; only regular files and
# folders (the updater refuses anything else). Used by
# .github/workflows/release.yml; see docs/RELEASING.md.
set -euo pipefail
cd "$(dirname "$0")/.."

[ $# -eq 3 ] || { echo "usage: $0 VERSION ARCH OUTDIR"; exit 2; }
VERSION="$1" ARCH="$2" OUT="$3"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "VERSION must be x.y.z or x.y.z-pre (no leading v)"; exit 2; }
[[ "$ARCH" =~ ^(amd64|arm64)$ ]] || { echo "ARCH must be amd64 or arm64"; exit 2; }

for f in server/bin/linuxadmind server/bin/linuxadmin-bridge web/dist/index.html plugins/docker/manifest.sig; do
  [ -f "$f" ] || { echo "missing $f"; exit 1; }
done
# Check the binaries carry VERSION (they can only run on a host of the same arch).
case "$(uname -m)" in x86_64) HOST=amd64 ;; aarch64|arm64) HOST=arm64 ;; *) HOST=other ;; esac
if [ "$HOST" = "$ARCH" ]; then
  for b in linuxadmind linuxadmin-bridge; do
    got="$(server/bin/$b --version)"
    [ "$got" = "$VERSION" ] || { echo "server/bin/$b reports '$got', expected '$VERSION'"; exit 1; }
  done
fi

NAME="linuxadmin-$VERSION-linux-$ARCH"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
D="$STAGE/$NAME"
mkdir -p "$D/bin" "$D/plugins" "$D/packaging"
install -m755 server/bin/linuxadmind server/bin/linuxadmin-bridge "$D/bin/"
cp -r web/dist "$D/web"
cp -r plugins/docker "$D/plugins/docker"
cp -r packaging/linuxadmin.service packaging/pam.d packaging/install.sh packaging/README.md "$D/packaging/"
# The installer; packaging/install.sh runs it with --from this folder.
install -m755 install.sh "$D/install.sh"
install -m644 LICENSE "$D/LICENSE"
printf '%s\n' "$VERSION" > "$D/VERSION"
chmod -R u+rwX,go+rX,go-w "$D"

if [ -n "$(find "$D" ! -type f ! -type d | head -n1)" ]; then
  echo "release tree contains symlinks or special files:"; find "$D" ! -type f ! -type d; exit 1
fi

mkdir -p "$OUT"
MTIME="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || date +%s)}"
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$MTIME" --format=gnu \
  -C "$STAGE" -cf - "$NAME" | gzip -n -9 > "$OUT/$NAME.tar.gz"
echo "$OUT/$NAME.tar.gz"
