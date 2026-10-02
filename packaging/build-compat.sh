#!/bin/sh
# Builds server/bin/* against glibc 2.17 (manylinux2014, CentOS 7 userland), so the release
# binaries run on old distributions too: Amazon Linux 2, RHEL/Rocky 8, Debian 10/11, Ubuntu 20.04.
# The daemon links libpam and libc through cgo; built on a new distribution it would need that
# distribution's glibc.
#
#   packaging/build-compat.sh VERSION ARCH        (ARCH: amd64 | arm64; needs docker)
#
# GO_VERSION (default: the go line of server/go.mod) picks the Go toolchain.
set -eu

VERSION="$1"
ARCH="$2"
cd "$(dirname "$0")/.."

case "$ARCH" in
amd64) img=quay.io/pypa/manylinux2014_x86_64 goarch=amd64 ;;
arm64) img=quay.io/pypa/manylinux2014_aarch64 goarch=arm64 ;;
*) echo "unknown arch $ARCH" >&2; exit 2 ;;
esac
GO_VERSION="${GO_VERSION:-$(sed -n 's/^go \([0-9.]*\)$/\1/p' server/go.mod)}"

docker run --rm --platform "linux/$goarch" \
	-v "$PWD:/src" -w /src \
	-e VERSION="$VERSION" -e GO_VERSION="$GO_VERSION" -e GOARCH_DL="$goarch" \
	-e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
	"$img" sh -euc '
		yum install -y -q pam-devel >/dev/null
		curl -fsSL --proto "=https" "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH_DL}.tar.gz" | tar -xz -C /usr/local
		export PATH="/usr/local/go/bin:$PATH" GOFLAGS=-buildvcs=false GOTOOLCHAIN=local CGO_ENABLED=1
		make build-server VERSION="$VERSION"
		chown -R "$HOST_UID:$HOST_GID" server/bin
	'

# Refuse binaries that need a newer glibc than 2.17.
for f in server/bin/ervisiod server/bin/ervisio-bridge; do
	need="$(objdump -T "$f" | grep -o 'GLIBC_[0-9.]*' | sort -uV | tail -n 1)"
	echo "$f: needs $need"
	case "$need" in
	GLIBC_2.1[0-7] | GLIBC_2.[0-9] | GLIBC_2.[0-9].* | GLIBC_2.1[0-7].* | "") ;;
	*) echo "$f needs $need, newer than glibc 2.17" >&2; exit 1 ;;
	esac
done
