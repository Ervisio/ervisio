#!/bin/bash
# Points both AUR packages at a release:
#
#   packaging/arch/update-pkgbuild.sh 1.2.0
#
# - downloads SHA256SUMS and SHA256SUMS.sig of v1.2.0 and verifies the
#   signature against the release key compiled into the code (needs go);
#   the checksums of the two release archives are taken from that signed file
# - downloads the source archive of the tag and hashes it
# - refreshes the local copies of packaging/pam.d/linuxadmin.arch and
#   packaging/package-service.sh next to each PKGBUILD
# - sets pkgver, resets pkgrel to 1, writes the sha256sums arrays and
#   regenerates .SRCINFO (makepkg --printsrcinfo; run as a normal user)
#
# Then review the diff, commit, and publish as described in docs/PACKAGING.md.
# Pre-releases (x.y.z-rc.1) are not published to the AUR.
set -euo pipefail
cd "$(dirname "$0")"
ARCHDIR="$(pwd)"
REPO_ROOT="$(cd ../.. && pwd)"
REPO=Fonlogen/LinuxAdmin

[ $# -eq 1 ] || { echo "usage: $0 VERSION   (x.y.z)"; exit 2; }
V="${1#v}"
[[ "$V" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "VERSION must be x.y.z (pre-releases are not published to the AUR)"; exit 2; }
command -v go >/dev/null || { echo "go is needed to verify the release signature"; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
dl() { curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$2" "$1"; }

BASE="https://github.com/$REPO/releases/download/v$V"
dl "$BASE/SHA256SUMS" "$TMP/SHA256SUMS"
dl "$BASE/SHA256SUMS.sig" "$TMP/SHA256SUMS.sig"
(cd "$REPO_ROOT/server" && go run ./tools/release-sign -verify "$TMP/SHA256SUMS")
sum_of() {
  local s
  s="$(awk -v n="$1" '$2 == n || $2 == "*" n { print $1; exit }' "$TMP/SHA256SUMS")"
  [[ "$s" =~ ^[0-9a-f]{64}$ ]] || { echo "$1 is not in SHA256SUMS of v$V" >&2; exit 1; }
  printf '%s' "$s"
}
AMD64="$(sum_of "linuxadmin-$V-linux-amd64.tar.gz")"
ARM64="$(sum_of "linuxadmin-$V-linux-arm64.tar.gz")"
dl "https://github.com/$REPO/archive/refs/tags/v$V.tar.gz" "$TMP/src.tar.gz"
SRC="$(sha256sum "$TMP/src.tar.gz" | cut -d' ' -f1)"

for d in linuxadmin-bin linuxadmin; do
  cp "$REPO_ROOT/packaging/pam.d/linuxadmin.arch" "$d/linuxadmin.pam"
  cp "$REPO_ROOT/packaging/package-service.sh" "$d/package-service.sh"
  cmp -s linuxadmin-bin/linuxadmin.install "$d/linuxadmin.install" || cp linuxadmin-bin/linuxadmin.install "$d/linuxadmin.install"
done
PAM="$(sha256sum linuxadmin-bin/linuxadmin.pam | cut -d' ' -f1)"
HELPER="$(sha256sum linuxadmin-bin/package-service.sh | cut -d' ' -f1)"

sed -i -E \
  -e "s/^pkgver=.*/pkgver=$V/" -e "s/^pkgrel=.*/pkgrel=1/" \
  -e "s/^sha256sums=.*/sha256sums=('$PAM' '$HELPER')/" \
  -e "s/^sha256sums_x86_64=.*/sha256sums_x86_64=('$AMD64')/" \
  -e "s/^sha256sums_aarch64=.*/sha256sums_aarch64=('$ARM64')/" \
  linuxadmin-bin/PKGBUILD
sed -i -E \
  -e "s/^pkgver=.*/pkgver=$V/" -e "s/^pkgrel=.*/pkgrel=1/" \
  -e "s/^sha256sums=.*/sha256sums=('$SRC' '$PAM' '$HELPER')/" \
  linuxadmin/PKGBUILD

if command -v makepkg >/dev/null && [ "$(id -u)" -ne 0 ]; then
  for d in linuxadmin-bin linuxadmin; do
    (cd "$d" && makepkg --printsrcinfo > .SRCINFO)
  done
else
  echo "makepkg is not available (or running as root): regenerate .SRCINFO on Arch with 'makepkg --printsrcinfo > .SRCINFO'."
fi
echo "Updated $ARCHDIR/{linuxadmin-bin,linuxadmin} to $V. Review: git diff -- packaging/arch"
