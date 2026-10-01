#!/bin/sh
# Installs the release folder this script belongs to, for an archive
# downloaded by hand from the GitHub release page:
#
#   tar xzf linuxadmin-1.2.0-linux-amd64.tar.gz
#   sudo ./linuxadmin-1.2.0-linux-amd64/packaging/install.sh [--dry-run] [--yes] [--open-firewall]
#
# It runs the installer shipped at the top of the folder (the same script as
# install.sh at the root of the repository) with --from <release folder>, so
# nothing is downloaded. A folder given as first argument is installed
# instead. From a source checkout use packaging/install-dev.sh.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
src="$(dirname "$here")"
if [ $# -gt 0 ] && [ -d "$1" ]; then
	src="$(cd "$1" && pwd)"
	shift
fi
if [ ! -f "$src/VERSION" ]; then
	echo "$src is not an extracted release archive. From a source checkout run packaging/install-dev.sh;" >&2
	echo "to download and install a release run install.sh at the root of the repository." >&2
	exit 1
fi
installer="$src/install.sh"
[ -f "$installer" ] || installer="$(dirname "$src")/install.sh"
[ -f "$installer" ] || {
	echo "No install.sh next to $src." >&2
	exit 1
}
exec sh "$installer" --from "$src" "$@"
