#!/bin/bash
# Builds the .deb and .rpm packages from a release folder:
#
#   packaging/build-packages.sh RELEASE_DIR ARCH OUTDIR
#   -> OUTDIR/linuxadmin_VERSION_ARCH.deb
#      OUTDIR/linuxadmin-VERSION-1.RPMARCH.rpm      (RPMARCH: x86_64 / aarch64)
#
# RELEASE_DIR is an extracted release archive (bin/, web/, plugins/, VERSION;
# see build-release.sh). The packaging files (unit, PAM files, scripts) come
# from this checkout. Needs nfpm (https://nfpm.goreleaser.com) in PATH or in
# $NFPM; the release workflow downloads a pinned version. The file names
# avoid "~" so that SHA256SUMS stays parseable by every console.
set -euo pipefail
cd "$(dirname "$0")/.."

[ $# -eq 3 ] || { echo "usage: $0 RELEASE_DIR ARCH OUTDIR"; exit 2; }
SRC="$(cd "$1" && pwd)" ARCH="$2" OUT="$3"
NFPM="${NFPM:-nfpm}"
case "$ARCH" in
  amd64) RPMARCH=x86_64 ;;
  arm64) RPMARCH=aarch64 ;;
  *) echo "ARCH must be amd64 or arm64"; exit 2 ;;
esac
for f in bin/linuxadmind bin/linuxadmin-bridge web/index.html VERSION; do
  [ -f "$SRC/$f" ] || { echo "missing $SRC/$f"; exit 1; }
done
VERSION="$(tr -d '[:space:]' < "$SRC/VERSION")"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "VERSION '$VERSION' is not x.y.z or x.y.z-pre"; exit 2; }
command -v "$NFPM" >/dev/null || { echo "nfpm not found (set NFPM=/path/to/nfpm)"; exit 1; }

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -r "$SRC/bin" "$SRC/web" "$STAGE/"
if [ -d "$SRC/plugins" ]; then cp -r "$SRC/plugins" "$STAGE/"; else mkdir "$STAGE/plugins"; fi
cp -r packaging/pam.d packaging/pkg/scripts "$STAGE/"
cp packaging/linuxadmin.service packaging/package-service.sh packaging/pkg/nfpm.yaml "$STAGE/"
if [ -f "$SRC/LICENSE" ]; then cp "$SRC/LICENSE" "$STAGE/"; else cp LICENSE "$STAGE/"; fi
printf 'apt\n' > "$STAGE/managed.apt"
chmod -R u+rwX,go+rX,go-w "$STAGE"

mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
DEB="$OUT/linuxadmin_${VERSION}_${ARCH}.deb"
RPM="$OUT/linuxadmin-${VERSION}-1.${RPMARCH}.rpm"
(
  cd "$STAGE"
  export VERSION ARCH
  # Reproducible timestamps (nfpm reads SOURCE_DATE_EPOCH).
  export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$OLDPWD" log -1 --format=%ct 2>/dev/null || date +%s)}"
  "$NFPM" package --config nfpm.yaml --packager deb --target "$DEB" >/dev/null
  "$NFPM" package --config nfpm.yaml --packager rpm --target "$RPM" >/dev/null
)
echo "$DEB"
echo "$RPM"
