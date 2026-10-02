#!/bin/bash
# Installs a locally built Ervisio for testing on this machine, with the
# same installer and layout as a release (install.sh, docs/RELEASING.md).
# Build first as your user:  make build            (VERSION=1.2.3 to stamp a version)
# Then:                      sudo ./packaging/install-dev.sh          (install or update)
#                            sudo ./packaging/install-dev.sh --remove (uninstall, keeps /etc/ervisio)
# Other options are passed to install.sh (--dry-run, --yes, --open-firewall).
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${1:-}" = "--remove" ]; then
  shift
  exec sh ./install.sh --uninstall --yes "$@"
fi

for f in server/bin/ervisiod server/bin/ervisio-bridge web/dist/index.html; do
  [ -e "$f" ] || { echo "Missing $f: run 'make build' as your user first."; exit 1; }
done

# The version folder is named after what the binary reports: git describe
# for local builds (e.g. v0.1.0-3-gab12cd3-dirty), VERSION=x.y.z if given.
RAW="$(server/bin/ervisiod --version)"
VER="${RAW#v}"
if ! [[ "$VER" =~ ^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$ ]] || [[ "$VER" == *..* ]]; then
  echo "Version '$RAW' cannot name a folder; rebuild with 'make build VERSION=x.y.z'."; exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/bin" "$STAGE/plugins" "$STAGE/packaging"
install -m755 server/bin/ervisiod server/bin/ervisio-bridge "$STAGE/bin/"
cp -r web/dist "$STAGE/web"
cp -r packaging/ervisio.service packaging/pam.d "$STAGE/packaging/"
printf '%s\n' "$VER" > "$STAGE/VERSION"

# install.sh picks packaging/pam.d/ervisio.<family> for this distribution.
sh ./install.sh --from "$STAGE" "$@"
