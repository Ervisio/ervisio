#!/bin/sh
# LinuxAdmin installer.
#
#   curl -fsSL https://raw.githubusercontent.com/Fonlogen/LinuxAdmin/main/install.sh | sudo sh
#   curl -fsSL https://raw.githubusercontent.com/Fonlogen/LinuxAdmin/main/install.sh | sudo sh -s -- --version 0.1.0
#
# or download it, read it, then run it:
#
#   curl -fsSLO https://raw.githubusercontent.com/Fonlogen/LinuxAdmin/main/install.sh
#   less install.sh
#   sudo sh install.sh [options]
#
# It downloads a release from GitHub, checks its ed25519 signature (with
# openssl, against the release key below) and its sha256, installs it into
# the versioned layout that self-update uses (docs/RELEASING.md):
#
#   /usr/lib/linuxadmin/versions/<version>/{bin,web,plugins,packaging,VERSION}
#   /usr/lib/linuxadmin/current  -> versions/<version>
#   /usr/lib/linuxadmin/previous -> versions/<version before>   (rollback)
#   /usr/bin/linuxadmind         -> /usr/lib/linuxadmin/current/bin/linuxadmind
#
# writes the PAM service for this distribution and the systemd unit, makes
# sure sudo is installed, and starts linuxadmin.service. Running it again
# upgrades or repairs the installation. LinuxAdmin installed this way
# updates itself from Settings > About. Installs made by a distribution
# package (.deb, .rpm, AUR) are left alone: update those with the package
# manager.
#
# Options:
#   --version X.Y.Z            install this release instead of the latest stable one
#   --prerelease               install the newest release, pre-releases included
#   --from DIR                 install a release folder already on disk (an extracted
#                              archive, or the folder packaging/install-dev.sh builds);
#                              nothing is downloaded or signature-checked
#   --uninstall                remove LinuxAdmin (keeps /etc/linuxadmin and installed plugins)
#   --purge                    with --uninstall: also remove /etc/linuxadmin and /var/lib/linuxadmin
#   --open-firewall            open the port in ufw or firewalld when one of them is active
#   --dry-run                  show what would be done, change nothing (can run without root)
#   -y, --yes                  do not ask questions (the firewall is only opened with --open-firewall)
#   --insecure-skip-signature  install even when this openssl cannot verify ed25519 signatures
#                              (the sha256 is still checked, but against an unverified list)
#   -h, --help                 show this help
#
# The script never runs downloaded code other than the verified release
# binaries (`linuxadmind --version`), and never turns off TLS verification.

REPO="Fonlogen/LinuxAdmin"
LIB=/usr/lib/linuxadmin
BIN_LINK=/usr/bin/linuxadmind
UNIT=linuxadmin.service
UNIT_FILE=/etc/systemd/system/linuxadmin.service
PAM_FILE=/etc/pam.d/linuxadmin
CONF_DIR=/etc/linuxadmin
CONF_FILE=/etc/linuxadmin/linuxadmin.conf
STATE_DIR=/var/lib/linuxadmin
FIREWALL_RECORD=/var/lib/linuxadmin/firewall
CERT=/etc/linuxadmin/tls/self-signed.crt
MANAGED_MARKER=/usr/lib/linuxadmin/managed
# First line of the unit file written by this script: distribution packages
# remove a unit file that starts with it (they ship their own).
UNIT_HEADER='# Installed by the LinuxAdmin installer (install.sh); removed by install.sh --uninstall.'

# The LinuxAdmin release key (ReleasePublicKey in server/internal/update/sign.go,
# as an SPKI PEM). SHA256SUMS.sig is an ed25519 signature over
# "linuxadmin-release-v1\n" followed by SHA256SUMS. A Go test checks that this
# copy matches the key the consoles trust.
RELEASE_KEY_PEM='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAeYuaHtzKiE4ofn2sX/p7tSNX/p5+sGV9EJT0aKSmNU8=
-----END PUBLIC KEY-----'

# RFC 8032 ed25519 test 2 (message "r"): proves this openssl can verify
# ed25519 before the real signature is checked.
SELFTEST_KEY_PEM='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAPUAXw+hDiVqStwqnTRt+vJyYLM8uxJaMwM1V8Sr0Zgw=
-----END PUBLIC KEY-----'
SELFTEST_SIG='kqAJqfDUyrhyDoILX2QlQKKye1QWUD+Ps3YiI+vbadoIWsHkPhWZbkWPNhPQ8R2MOHsurrQwKu6wDSkWErsMAA=='

# ---------------------------------------------------------------- output

say() { printf '%s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }
warn() { printf 'Warning: %s\n' "$*" >&2; }
die() {
	printf 'Error: %s\n' "$*" >&2
	exit 1
}
have() { command -v "$1" >/dev/null 2>&1; }

# run CMD...: runs a command, or prints it with --dry-run.
run() {
	if [ "$DRY" = 1 ]; then
		say "  [dry-run] $*"
	else
		"$@"
	fi
}

# ask QUESTION DEFAULT(y|n): yes/no question. Without a terminal on stdin,
# or with --yes, the default answer is taken without asking.
ask() {
	if [ "$YES" = 1 ] || [ ! -t 0 ]; then
		[ "$2" = y ]
		return
	fi
	printf '%s ' "$1"
	ask_answer=
	read -r ask_answer || ask_answer=
	case $ask_answer in
	[Yy]*) return 0 ;;
	[Nn]*) return 1 ;;
	'') [ "$2" = y ] ;;
	*) return 1 ;;
	esac
}

usage() {
	cat <<'USAGE'
LinuxAdmin installer

  curl -fsSL https://raw.githubusercontent.com/Fonlogen/LinuxAdmin/main/install.sh | sudo sh -s -- [options]
  sudo sh install.sh [options]

Options:
  --version X.Y.Z            install this release instead of the latest stable one
  --prerelease               install the newest release, pre-releases included
  --from DIR                 install a release folder already on disk (no download, no signature check)
  --uninstall                remove LinuxAdmin (keeps /etc/linuxadmin and installed plugins)
  --purge                    with --uninstall: also remove /etc/linuxadmin and /var/lib/linuxadmin
  --open-firewall            open the port in ufw or firewalld when one of them is active
  --dry-run                  show what would be done, change nothing (can run without root)
  -y, --yes                  do not ask questions (the firewall is only opened with --open-firewall)
  --insecure-skip-signature  install even when this openssl cannot verify ed25519 signatures
  -h, --help                 show this help
USAGE
}

# ---------------------------------------------------------------- checks

valid_version() {
	printf '%s\n' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
}

# The name of a version folder (what `linuxadmind --version` prints, without "v").
valid_dirname() {
	case $1 in *..*) return 1 ;; esac
	printf '%s\n' "$1" | grep -Eq '^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$'
}

# ver_cmp A B: prints -1, 0 or 1 (semantic versions; pre-releases sort
# before their release; anything that is not x.y.z sorts before every release).
ver_cmp() {
	awk -v a="$1" -v b="$2" '
	function isnum(s) { return s ~ /^[0-9]+$/ }
	function valid(v) { return v ~ /^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$/ }
	function cmp(x, y,   xc, yc, xp, yp, xa, ya, n, m, i) {
		if (!valid(x) && !valid(y)) return 0
		if (!valid(x)) return -1
		if (!valid(y)) return 1
		xp = ""; yp = ""
		if (index(x, "-")) { xp = substr(x, index(x, "-") + 1); x = substr(x, 1, index(x, "-") - 1) }
		if (index(y, "-")) { yp = substr(y, index(y, "-") + 1); y = substr(y, 1, index(y, "-") - 1) }
		split(x, xc, "."); split(y, yc, ".")
		for (i = 1; i <= 3; i++) if (xc[i] + 0 != yc[i] + 0) return (xc[i] + 0 < yc[i] + 0) ? -1 : 1
		if (xp == yp) return 0
		if (xp == "") return 1
		if (yp == "") return -1
		n = split(xp, xa, "."); m = split(yp, ya, ".")
		for (i = 1; i <= n && i <= m; i++) {
			if (xa[i] == ya[i]) continue
			if (isnum(xa[i]) && isnum(ya[i])) return (xa[i] + 0 < ya[i] + 0) ? -1 : 1
			if (isnum(xa[i])) return -1
			if (isnum(ya[i])) return 1
			return (xa[i] < ya[i]) ? -1 : 1
		}
		return (n < m) ? -1 : (n > m) ? 1 : 0
	}
	BEGIN { print cmp(a, b) }'
}

os_release_value() {
	[ -r /etc/os-release ] || return 0
	sed -n "s/^$1=//p" /etc/os-release | head -n 1 | tr -d "\"'"
}

# Sets FAMILY (arch, debian, fedora, suse or unknown), DISTRO_NAME and PM.
detect_family() {
	FAMILY=
	DISTRO_NAME="$(os_release_value PRETTY_NAME)"
	[ -n "$DISTRO_NAME" ] || DISTRO_NAME="unknown Linux"
	for df_id in $(os_release_value ID) $(os_release_value ID_LIKE); do
		case $df_id in
		arch | archarm | manjaro* | endeavouros | garuda | cachyos | artix) FAMILY=arch ;;
		debian | ubuntu | linuxmint | pop | raspbian | kali | elementary | zorin | neon | devuan) FAMILY=debian ;;
		fedora | rhel | centos | rocky | almalinux | ol | amzn | nobara | ultramarine) FAMILY=fedora ;;
		opensuse* | suse | sles | sled | sle-micro | sle_hpc) FAMILY=suse ;;
		esac
		[ -n "$FAMILY" ] && break
	done
	# Not listed: guess from the PAM stacks the system has.
	if [ -z "$FAMILY" ]; then
		if [ -e /etc/pam.d/system-login ]; then
			FAMILY=arch
		elif [ -e /etc/pam.d/password-auth ]; then
			FAMILY=fedora
		elif [ -e /etc/debian_version ] && [ -e /etc/pam.d/common-auth ]; then
			FAMILY=debian
		elif [ -e /etc/pam.d/common-auth ] || [ -e /usr/lib/pam.d/common-auth ]; then
			FAMILY=suse
		else
			FAMILY=unknown
		fi
	fi
	case $FAMILY in
	arch) PM=pacman ;;
	debian) PM=apt-get ;;
	fedora) if have dnf; then PM=dnf; elif have yum; then PM=yum; else PM=microdnf; fi ;;
	suse) PM=zypper ;;
	*) PM= ;;
	esac
	have "${PM:-/nonexistent}" || PM=
}

detect_arch() {
	case $(uname -m) in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) die "LinuxAdmin is built for x86-64 and ARM64; this machine is $(uname -m)." ;;
	esac
}

# pkg_install PACKAGE...: installs packages with the system package manager.
pkg_install() {
	[ -n "$PM" ] || return 1
	case $PM in
	pacman) run pacman -S --needed --noconfirm "$@" ;;
	apt-get)
		if [ "$DRY" = 1 ]; then
			say "  [dry-run] apt-get update && apt-get install -y $*"
		else
			DEBIAN_FRONTEND=noninteractive apt-get update -q >/dev/null &&
				DEBIAN_FRONTEND=noninteractive apt-get install -y -q "$@"
		fi
		;;
	dnf | yum | microdnf) run "$PM" install -y "$@" ;;
	zypper) run zypper --non-interactive install "$@" ;;
	*) return 1 ;;
	esac
}

# Downloading: curl or wget, HTTPS only, certificates verified.
fetch() {
	if have curl; then
		curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 20 -o "$2" "$1"
	else
		wget -q --https-only --tries=3 --timeout=20 -O "$2" "$1"
	fi
}

fetch_api() {
	if have curl; then
		curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 20 \
			-H 'Accept: application/vnd.github+json' "$1"
	else
		wget -q --https-only --tries=3 --timeout=20 --header='Accept: application/vnd.github+json' -O - "$1"
	fi
}

# Prints the tag names found in a GitHub releases JSON document, in order.
tag_names() {
	tr ',{' '\n' | sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p'
}

# Sets VERSION to the release to install (--version, newest pre-release, or latest stable).
resolve_version() {
	if [ -n "$WANT_VERSION" ]; then
		VERSION="${WANT_VERSION#v}"
		valid_version "$VERSION" || die "--version must look like 1.2.3 or 1.2.3-rc.1 (got '$WANT_VERSION')."
		return
	fi
	if [ "$PRERELEASE" = 1 ]; then
		rv_tag="$(fetch_api "https://api.github.com/repos/$REPO/releases?per_page=20" 2>/dev/null | tag_names | head -n 1)" || rv_tag=
	else
		rv_tag="$(fetch_api "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | tag_names | head -n 1)" || rv_tag=
		# API unreachable or rate limited: follow the release page redirect.
		if [ -z "$rv_tag" ] && have curl; then
			rv_url="$(curl -fsSL --proto '=https' --tlsv1.2 -o /dev/null -w '%{url_effective}' \
				"https://github.com/$REPO/releases/latest" 2>/dev/null)" || rv_url=
			case $rv_url in */releases/tag/*) rv_tag="${rv_url##*/}" ;; esac
		fi
	fi
	[ -n "$rv_tag" ] || die "Could not find the latest release on GitHub (no network, or the API rate limit). Try again later or pass --version X.Y.Z."
	VERSION="${rv_tag#v}"
	valid_version "$VERSION" || die "The latest release has an unexpected tag '$rv_tag'."
}

sha256_of() {
	if have sha256sum; then
		sha256sum "$1" | cut -d' ' -f1
	else
		openssl dgst -sha256 -r "$1" | cut -d' ' -f1
	fi
}

# ed25519_verify PEM_FILE MESSAGE_FILE BASE64_SIGNATURE_FILE
ed25519_verify() {
	openssl base64 -d -A -in "$3" -out "$3.bin" 2>/dev/null || return 1
	openssl pkeyutl -verify -pubin -inkey "$1" -rawin -in "$2" -sigfile "$3.bin" >/dev/null 2>&1
}

# Sets CAN_VERIFY=1 when this openssl verifies ed25519 (OpenSSL 3 or newer).
openssl_selftest() {
	CAN_VERIFY=0
	have openssl || return 0
	printf '%s\n' "$SELFTEST_KEY_PEM" >"$TMPD/selftest.pem"
	printf 'r' >"$TMPD/selftest.msg"
	printf '%s\n' "$SELFTEST_SIG" >"$TMPD/selftest.sig"
	if ed25519_verify "$TMPD/selftest.pem" "$TMPD/selftest.msg" "$TMPD/selftest.sig"; then
		CAN_VERIFY=1
	fi
}

# ---------------------------------------------------------------- state of this machine

# Sets INSTALLED to the running version folder ("" when not installed).
installed_version() {
	INSTALLED=
	if iv_t="$(readlink "$LIB/current" 2>/dev/null)"; then
		INSTALLED="${iv_t#versions/}"
	elif [ -f "$BIN_LINK" ] && [ ! -L "$BIN_LINK" ]; then
		# An old flat install (before the versioned layout).
		INSTALLED="$("$BIN_LINK" --version 2>/dev/null || true)"
		INSTALLED="${INSTALLED#v}"
		[ -n "$INSTALLED" ] || INSTALLED=flat
	fi
}

refuse_managed() {
	if [ -e "$MANAGED_MARKER" ]; then
		rm_by="$(head -c 64 "$MANAGED_MARKER" 2>/dev/null | tr -cd 'a-z0-9._+-')"
		[ -n "$rm_by" ] || rm_by="a package manager"
		die "LinuxAdmin on this machine was installed by $rm_by (the linuxadmin package). Update or remove it with $rm_by; this script only manages installs it made."
	fi
}

# Reads the listen address from the configuration (default 0.0.0.0:9090).
read_listen() {
	LISTEN=
	if [ -r "$CONF_FILE" ]; then
		LISTEN="$(awk '/^[[:space:]]*\[/ { exit } /^[[:space:]]*listen[[:space:]]*=/ { sub(/^[^=]*=[[:space:]]*/, ""); gsub(/["'\'']/, ""); sub(/[[:space:]]*(#.*)?$/, ""); print; exit }' "$CONF_FILE")"
	fi
	[ -n "$LISTEN" ] || LISTEN="0.0.0.0:9090"
	PORT="${LISTEN##*:}"
	LISTEN_HOST="${LISTEN%:*}"
	case $PORT in '' | *[!0-9]*) PORT=9090 ;; esac
}

tls_mode() {
	tm=
	if [ -r "$CONF_FILE" ]; then
		tm="$(awk '/^[[:space:]]*\[tls\]/ { s = 1; next } /^[[:space:]]*\[/ { s = 0 } s && /^[[:space:]]*mode[[:space:]]*=/ { sub(/^[^=]*=[[:space:]]*/, ""); gsub(/["'\'']/, ""); sub(/[[:space:]]*(#.*)?$/, ""); print; exit }' "$CONF_FILE")"
	fi
	printf '%s\n' "${tm:-self-signed}"
}

# ---------------------------------------------------------------- PAM

# Embedded copies of packaging/pam.d/linuxadmin.<family>, used when the
# release folder does not ship them (0.1.0). A Go test keeps them in sync.
embedded_pam() {
	case $1 in
arch)
		cat <<'PAM'
#%PAM-1.0
# PAM service for LinuxAdmin sign-in (/etc/pam.d/linuxadmin): Arch Linux and
# derivatives. The session stack runs around every user bridge (pam_limits,
# pam_loginuid, pam_systemd...), opened by linuxadmind's root session helper.
auth      include   system-login
account   include   system-login
session   include   system-login
PAM
		;;
debian)
		cat <<'PAM'
#%PAM-1.0
# PAM service for LinuxAdmin sign-in (/etc/pam.d/linuxadmin): Debian, Ubuntu
# and derivatives. The session stack runs around every user bridge, opened by
# linuxadmind's root session helper.
auth      requisite pam_nologin.so
@include common-auth
@include common-account
session   optional  pam_loginuid.so
session   optional  pam_keyinit.so force revoke
session   required  pam_limits.so
@include common-session
PAM
		;;
fedora)
		cat <<'PAM'
#%PAM-1.0
# PAM service for LinuxAdmin sign-in (/etc/pam.d/linuxadmin): Fedora, RHEL,
# CentOS Stream, Rocky, AlmaLinux. The session stack runs around every user
# bridge, opened by linuxadmind's root session helper.
auth      substack  password-auth
auth      include   postlogin
account   required  pam_nologin.so
account   include   password-auth
session   optional  pam_loginuid.so
session   optional  pam_keyinit.so force revoke
session   include   password-auth
session   include   postlogin
PAM
		;;
suse)
		cat <<'PAM'
#%PAM-1.0
# PAM service for LinuxAdmin sign-in (/etc/pam.d/linuxadmin): openSUSE and
# SLES. The session stack runs around every user bridge, opened by
# linuxadmind's root session helper.
auth      requisite pam_nologin.so
auth      include   common-auth
account   include   common-account
session   optional  pam_loginuid.so
session   optional  pam_keyinit.so force revoke
session   include   common-session
PAM
		;;
	*) return 1 ;;
	esac
}

# install_pam SRC_DIR: writes /etc/pam.d/linuxadmin for this distribution
# family. A file written by LinuxAdmin (it says "PAM service for LinuxAdmin")
# is replaced; one written by the administrator is kept.
install_pam() {
	if [ "$FAMILY" = unknown ]; then
		warn "Unknown distribution: no $PAM_FILE written. LinuxAdmin then uses the 'login' PAM service; see packaging/README.md to write one."
		return 0
	fi
	if [ -f "$1/packaging/pam.d/linuxadmin.$FAMILY" ]; then
		cp "$1/packaging/pam.d/linuxadmin.$FAMILY" "$TMPD/pam"
	else
		embedded_pam "$FAMILY" >"$TMPD/pam"
	fi
	if [ -f "$PAM_FILE" ]; then
		if cmp -s "$TMPD/pam" "$PAM_FILE"; then
			say "PAM service $PAM_FILE is up to date ($FAMILY)."
			return 0
		fi
		if ! grep -q 'PAM service for LinuxAdmin' "$PAM_FILE"; then
			say "Keeping your own $PAM_FILE (not written by LinuxAdmin)."
			return 0
		fi
		run cp -p "$PAM_FILE" "$CONF_DIR/pam.d-linuxadmin.bak"
	fi
	say "Writing $PAM_FILE ($FAMILY)."
	run install -D -m 644 "$TMPD/pam" "$PAM_FILE"
}

# ---------------------------------------------------------------- sudo

ensure_sudo() {
	if ! have sudo; then
		say "sudo is not installed; LinuxAdmin uses it for administrator rights. Installing it."
		pkg_install sudo || warn "Could not install sudo. Install it yourself: administrator rights in LinuxAdmin need it."
	fi
}

report_admin_group() {
	case $FAMILY in
	debian) ADMIN_GROUP=sudo ;;
	*) ADMIN_GROUP=wheel ;;
	esac
	rg_user="${SUDO_USER:-}"
	[ "$rg_user" = root ] && rg_user=
	rg_rule=0
	rg_new=''
	getent group "$ADMIN_GROUP" >/dev/null 2>&1 || rg_new="groupadd $ADMIN_GROUP && "
	# openSUSE keeps the default policy in /usr/etc.
	for rg_f in /etc/sudoers /etc/sudoers.d/* /usr/etc/sudoers /usr/etc/sudoers.d/*; do
		[ -f "$rg_f" ] && grep -Eq "^[[:space:]]*%${ADMIN_GROUP}[[:space:]]" "$rg_f" 2>/dev/null && rg_rule=1
	done
	step "Administrator rights"
	say "LinuxAdmin unlocks administrator rights with sudo, using your own password."
	if [ -n "$rg_user" ]; then
		if id -nG "$rg_user" 2>/dev/null | tr ' ' '\n' | grep -qx "$ADMIN_GROUP"; then
			say "Your account '$rg_user' is in the '$ADMIN_GROUP' group."
		else
			say "Your account '$rg_user' is NOT in the '$ADMIN_GROUP' group. To manage the system from LinuxAdmin:"
			say "  ${rg_new}usermod -aG $ADMIN_GROUP $rg_user      (then sign out and in again)"
		fi
	else
		say "Accounts in the '$ADMIN_GROUP' group get administrator rights. Add one with: ${rg_new}usermod -aG $ADMIN_GROUP <user>"
	fi
	if [ "$rg_rule" = 0 ] && { [ -r /etc/sudoers ] || [ -r /usr/etc/sudoers ]; }; then
		say "No sudoers rule for '%$ADMIN_GROUP' was found. Enable it with 'visudo' (uncomment the line"
		say "  %$ADMIN_GROUP ALL=(ALL:ALL) ALL"
		say "or put it in /etc/sudoers.d/$ADMIN_GROUP)."
	fi
	if grep -Eqs '^[[:space:]]*Defaults[[:space:]]+targetpw' /etc/sudoers /usr/etc/sudoers &&
		! cat /etc/sudoers.d/* /usr/etc/sudoers.d/* 2>/dev/null | grep -Eq "^[[:space:]]*Defaults:%${ADMIN_GROUP}[[:space:]]+!targetpw"; then
		say "sudo asks for the root password here (Defaults targetpw). LinuxAdmin sends your own password,"
		say "so administrator rights will not unlock until that changes. On openSUSE:"
		say "  zypper install sudo-policy-wheel-auth-self      (wheel members use their own password)"
	fi
}

# ---------------------------------------------------------------- firewall

firewall() {
	read_listen
	case $LISTEN_HOST in 127.* | localhost | '[::1]') return 0 ;; esac
	fw=
	if have ufw && ufw status 2>/dev/null | grep -q '^Status: active'; then
		fw=ufw
		ufw status 2>/dev/null | grep -Eq "^$PORT(/tcp)?[[:space:]].*ALLOW" && return 0
	elif have firewall-cmd && [ "$(firewall-cmd --state 2>/dev/null)" = running ]; then
		fw=firewalld
		firewall-cmd --query-port="$PORT/tcp" >/dev/null 2>&1 && return 0
		firewall-cmd --query-service=cockpit >/dev/null 2>&1 && [ "$PORT" = 9090 ] && return 0
	fi
	[ -n "$fw" ] || return 0
	step "Firewall"
	say "$fw is active and port $PORT/tcp is closed: other machines cannot reach LinuxAdmin yet."
	if [ "$OPEN_FW" = 1 ] || ask "Open port $PORT/tcp in $fw? [y/N]" n; then
		if [ "$fw" = ufw ]; then
			run ufw allow "$PORT/tcp" comment LinuxAdmin
		else
			run firewall-cmd --quiet --permanent --add-port="$PORT/tcp"
			run firewall-cmd --quiet --add-port="$PORT/tcp"
		fi
		if [ "$DRY" = 0 ]; then
			printf '%s %s/tcp\n' "$fw" "$PORT" >"$FIREWALL_RECORD"
		fi
		say "Opened $PORT/tcp in $fw."
	else
		if [ "$fw" = ufw ]; then
			say "To open it later: ufw allow $PORT/tcp"
		else
			say "To open it later: firewall-cmd --permanent --add-port=$PORT/tcp && firewall-cmd --add-port=$PORT/tcp"
		fi
	fi
}

close_firewall() {
	[ -f "$FIREWALL_RECORD" ] || return 0
	read -r cf_fw cf_port <"$FIREWALL_RECORD" || return 0
	case $cf_port in [0-9]*/tcp) ;; *) return 0 ;; esac
	case $cf_fw in
	ufw) have ufw && run ufw delete allow "$cf_port" && say "Closed $cf_port in ufw." ;;
	firewalld)
		if have firewall-cmd; then
			run firewall-cmd --quiet --permanent --remove-port="$cf_port" || true
			run firewall-cmd --quiet --remove-port="$cf_port" || true
			say "Closed $cf_port in firewalld."
		fi
		;;
	esac
	return 0
}

# ---------------------------------------------------------------- install

# Downloads and verifies the release; sets SRC to the extracted folder.
download_release() {
	ASSET="linuxadmin-$VERSION-linux-$ARCH.tar.gz"
	BASE="https://github.com/$REPO/releases/download/v$VERSION"
	step "Downloading LinuxAdmin $VERSION ($ARCH)"
	for dl_f in SHA256SUMS SHA256SUMS.sig "$ASSET"; do
		say "  $BASE/$dl_f"
		fetch "$BASE/$dl_f" "$TMPD/$dl_f" || die "Download of $dl_f failed. Does release v$VERSION exist, with a build for $ARCH?"
	done

	step "Verifying"
	if [ "$CAN_VERIFY" = 1 ]; then
		printf '%s\n' "$RELEASE_KEY_PEM" >"$TMPD/release.pem"
		printf 'linuxadmin-release-v1\n' >"$TMPD/signed.msg"
		cat "$TMPD/SHA256SUMS" >>"$TMPD/signed.msg"
		if ! ed25519_verify "$TMPD/release.pem" "$TMPD/signed.msg" "$TMPD/SHA256SUMS.sig"; then
			die "The signature of SHA256SUMS does not match the LinuxAdmin release key. The download was corrupted or tampered with; nothing was installed."
		fi
		say "Signature of SHA256SUMS: good (LinuxAdmin release key)."
	else
		warn "This openssl cannot verify ed25519 signatures (OpenSSL 3 or newer is needed): SHA256SUMS is NOT verified. Continuing because of --insecure-skip-signature."
	fi
	dl_want="$(awk -v n="$ASSET" '$2 == n || $2 == "*" n { print $1; exit }' "$TMPD/SHA256SUMS")"
	printf '%s\n' "$dl_want" | grep -Eq '^[0-9a-f]{64}$' || die "$ASSET is not listed in SHA256SUMS."
	dl_got="$(sha256_of "$TMPD/$ASSET")"
	[ "$dl_got" = "$dl_want" ] || die "$ASSET does not match its sha256 in SHA256SUMS (download corrupted or tampered with)."
	say "sha256 of $ASSET: good."

	PREFIX="linuxadmin-$VERSION-linux-$ARCH"
	tar -tzf "$TMPD/$ASSET" >"$TMPD/list" || die "$ASSET is not a readable archive."
	if grep -Ev "^$PREFIX(/|\$)" "$TMPD/list" | grep -q . || grep -Eq '(^|/)\.\.(/|$)' "$TMPD/list"; then
		die "$ASSET contains files outside $PREFIX/: refused."
	fi
	mkdir "$TMPD/x"
	tar -xzf "$TMPD/$ASSET" -C "$TMPD/x" --no-same-owner || die "Could not extract $ASSET."
	SRC="$TMPD/x/$PREFIX"
}

# check_release_folder DIR: sets VERSION from DIR/VERSION and checks the folder.
check_release_folder() {
	for cr_f in bin/linuxadmind bin/linuxadmin-bridge web/index.html VERSION; do
		[ -f "$1/$cr_f" ] || die "$1 is not a LinuxAdmin release folder (no $cr_f)."
	done
	if [ -n "$(find "$1" ! -type f ! -type d | head -n 1)" ]; then
		die "$1 contains symlinks or special files: refused."
	fi
	cr_v="$(tr -d '[:space:]' <"$1/VERSION")"
	valid_dirname "$cr_v" || die "Invalid version '$cr_v' in $1/VERSION."
	if [ -n "${VERSION:-}" ] && [ "$cr_v" != "$VERSION" ]; then
		die "$1 is version $cr_v, expected $VERSION."
	fi
	VERSION="$cr_v"
}

# binary_runs DIR: DIR/bin/linuxadmind runs here and prints VERSION. It is
# run from its install location (/tmp may be mounted noexec).
binary_runs() {
	br_got="$("$1/bin/linuxadmind" --version 2>/dev/null || true)"
	[ "$br_got" = "$VERSION" ] || [ "${br_got#v}" = "$VERSION" ]
}

setlink() {
	ln -sfn "$2" "$1.tmp-$$" && mv -T "$1.tmp-$$" "$1"
}

# install_layout SRC: copies SRC to versions/<VERSION> and switches `current`.
install_layout() {
	step "Installing into $LIB/versions/$VERSION"
	if [ "$DRY" = 1 ]; then
		binary_runs "$1" || warn "Could not run bin/linuxadmind --version from $1 (a real run checks it in $LIB)."
		say "  [dry-run] copy bin/ web/ plugins/ packaging/ VERSION to $LIB/versions/$VERSION"
		say "  [dry-run] $LIB/current -> versions/$VERSION"
		say "  [dry-run] $BIN_LINK -> $LIB/current/bin/linuxadmind"
		say "  [dry-run] keep only the current and previous versions"
		say "  [dry-run] create $STATE_DIR/updates, $CONF_DIR"
		return 0
	fi
	install -d -m 755 "$LIB/versions"
	il_tmp="$(mktemp -d "$LIB/versions/.$VERSION.partial-XXXXXX")" || die "Cannot write to $LIB/versions."
	PARTIAL="$il_tmp"
	cp -r "$1/bin" "$1/web" "$il_tmp/"
	[ -d "$1/plugins" ] && cp -r "$1/plugins" "$il_tmp/"
	[ -d "$1/packaging" ] && cp -r "$1/packaging" "$il_tmp/"
	printf '%s\n' "$VERSION" >"$il_tmp/VERSION"
	chown -R root:root "$il_tmp"
	chmod -R u+rwX,go+rX,go-w "$il_tmp"
	chmod 755 "$il_tmp" "$il_tmp/bin/linuxadmind" "$il_tmp/bin/linuxadmin-bridge"
	binary_runs "$il_tmp" || die "bin/linuxadmind of $VERSION does not run on this machine (or reports another version); nothing was changed."

	il_old=
	if [ -e "$LIB/versions/$VERSION" ]; then
		il_old="$LIB/versions/.$VERSION.old-$$"
		mv -T "$LIB/versions/$VERSION" "$il_old"
	fi
	mv -T "$il_tmp" "$LIB/versions/$VERSION"
	PARTIAL=

	il_cur="$(readlink "$LIB/current" 2>/dev/null || true)"
	il_cur="${il_cur#versions/}"
	if [ -n "$il_cur" ] && [ "$il_cur" != "$VERSION" ] && [ -d "$LIB/versions/$il_cur" ]; then
		setlink "$LIB/previous" "versions/$il_cur"
	fi
	setlink "$LIB/current" "versions/$VERSION"
	setlink "$BIN_LINK" "$LIB/current/bin/linuxadmind"
	# Unit files written by older versions start $LIB/linuxadmin-bridge.
	setlink "$LIB/linuxadmin-bridge" "current/bin/linuxadmin-bridge"

	# Leftovers of the flat layout.
	rm -rf /usr/share/linuxadmin/web /usr/share/linuxadmin/plugins
	rmdir /usr/share/linuxadmin 2>/dev/null || true

	# Keep current + previous only.
	il_prev="$(readlink "$LIB/previous" 2>/dev/null || true)"
	il_prev="${il_prev#versions/}"
	for il_d in "$LIB"/versions/* "$LIB"/versions/.*; do
		il_n="$(basename "$il_d")"
		case $il_n in . | .. | "$VERSION" | "$il_prev") continue ;; esac
		[ -d "$il_d" ] && rm -rf -- "$il_d"
	done
	[ -n "$il_old" ] && rm -rf -- "$il_old"
	PREVIOUS="$il_prev"

	install -d -m 755 "$STATE_DIR" "$STATE_DIR/updates"
	install -d -m 700 "$STATE_DIR/updates/staging"
	install -d -m 755 "$CONF_DIR"
}

install_unit() {
	if [ -f "$1/packaging/linuxadmin.service" ]; then
		{
			printf '%s\n' "$UNIT_HEADER"
			cat "$1/packaging/linuxadmin.service"
		} >"$TMPD/unit"
	else
		die "The release folder has no packaging/linuxadmin.service."
	fi
	if [ -f "$UNIT_FILE" ] && cmp -s "$TMPD/unit" "$UNIT_FILE"; then
		say "systemd unit $UNIT_FILE is up to date."
	else
		say "Writing $UNIT_FILE."
		run install -D -m 644 "$TMPD/unit" "$UNIT_FILE"
	fi
}

start_service() {
	step "Starting $UNIT"
	run systemctl daemon-reload
	run systemctl enable --quiet "$UNIT"
	run systemctl restart "$UNIT"
	[ "$DRY" = 1 ] && return 0
	ss_i=0
	while [ $ss_i -lt 15 ]; do
		systemctl is-active --quiet "$UNIT" && break
		sleep 1
		ss_i=$((ss_i + 1))
	done
	if ! systemctl is-active --quiet "$UNIT"; then
		systemctl --no-pager --lines=20 status "$UNIT" >&2 || true
		die "$UNIT did not start. See: journalctl -u linuxadmin"
	fi
	say "$UNIT is running."
}

print_access() {
	read_listen
	step "Open LinuxAdmin"
	case $LISTEN_HOST in
	'' | 0.0.0.0 | '[::]' | '::')
		say "  https://$(uname -n):$PORT"
		if have ip; then
			ip -o addr show scope global 2>/dev/null | awk '
				$2 ~ /^(docker|br-|veth|virbr|cni|flannel|cali|podman|lxc|tun|wg)/ { next }
				{ split($4, a, "/"); if ($3 == "inet") print a[1]; else if ($3 == "inet6") print "[" a[1] "]" }' |
				while read -r pa_ip; do say "  https://$pa_ip:$PORT"; done
		fi
		;;
	*) say "  https://$LISTEN_HOST:$PORT" ;;
	esac
	say "Sign in with a Linux account."
	[ "$DRY" = 1 ] && return 0
	if [ "$(tls_mode)" != self-signed ]; then
		say "TLS uses the certificate configured in $CONF_FILE."
		return 0
	fi
	pa_i=0
	while [ ! -s "$CERT" ] && [ $pa_i -lt 20 ]; do
		sleep 1
		pa_i=$((pa_i + 1))
	done
	if [ -s "$CERT" ] && have openssl; then
		pa_fp="$(openssl x509 -in "$CERT" -noout -fingerprint -sha256 2>/dev/null | sed 's/^[^=]*=//')"
		say ""
		say "The certificate is self-signed, so the browser warns once. Before you accept it, check that"
		say "the SHA-256 fingerprint the browser shows (certificate details) is:"
		say "  $pa_fp"
	fi
}

do_install() {
	refuse_managed
	installed_version
	if [ -n "$FROM" ]; then
		SRC="$(cd "$FROM" 2>/dev/null && pwd)" || die "No such folder: $FROM"
		check_release_folder "$SRC"
	else
		resolve_version
		if [ -n "$INSTALLED" ] && [ -z "$WANT_VERSION" ] && [ "$(ver_cmp "$INSTALLED" "$VERSION")" = 1 ]; then
			die "LinuxAdmin $INSTALLED is installed, newer than $VERSION. To install $VERSION anyway: --version $VERSION"
		fi
	fi

	say "LinuxAdmin installer"
	say "  System:   $DISTRO_NAME ($FAMILY, $ARCH)"
	if [ -n "$INSTALLED" ] && [ "$INSTALLED" = "$VERSION" ]; then
		say "  Version:  $VERSION (installed: repairing)"
	elif [ -n "$INSTALLED" ]; then
		say "  Version:  $VERSION (installed: $INSTALLED)"
	else
		say "  Version:  $VERSION"
	fi
	[ -n "$FROM" ] && say "  From:     $SRC"
	[ "$DRY" = 1 ] && say "  Dry run:  nothing will be changed"
	if ! ask "Continue? [Y/n]" y; then
		say "Nothing changed."
		exit 0
	fi

	if [ -z "$FROM" ]; then
		if ! have openssl; then
			say "openssl is needed to verify the release signature. Installing it."
			[ "$DRY" = 1 ] || pkg_install openssl || true
		fi
		openssl_selftest
		if [ "$CAN_VERIFY" = 0 ] && [ "$SKIP_SIG" = 0 ]; then
			if [ "$DRY" = 1 ] && ! have openssl; then
				warn "openssl is missing; a real run installs it first."
			else
				die "This system's openssl cannot verify ed25519 signatures (OpenSSL 3 or newer is needed), so the download cannot be checked. Update openssl, or pass --insecure-skip-signature to install without the signature check."
			fi
		fi
		download_release
		check_release_folder "$SRC"
	fi

	step "Prerequisites"
	ensure_sudo
	have systemd-run || warn "systemd-run is missing: self-update will not work."

	install_layout "$SRC"
	step "System files"
	install_pam "$SRC"
	install_unit "$SRC"
	start_service
	report_admin_group
	firewall
	print_access
	say ""
	if [ "$DRY" = 1 ]; then
		say "Dry run finished: nothing was changed."
	else
		say "LinuxAdmin $VERSION is installed${PREVIOUS:+ (version $PREVIOUS is kept for rollback)}."
		say "It updates itself from Settings > About. Uninstall: sh install.sh --uninstall"
	fi
}

do_uninstall() {
	refuse_managed
	installed_version
	if [ -z "$INSTALLED" ] && [ ! -e "$UNIT_FILE" ] && [ ! -e "$LIB" ]; then
		say "LinuxAdmin is not installed."
		[ "$PURGE" = 1 ] || exit 0
	fi
	say "Removing LinuxAdmin${INSTALLED:+ $INSTALLED}."
	if [ "$PURGE" = 1 ]; then
		say "--purge: the configuration ($CONF_DIR, including the TLS certificate) and $STATE_DIR (installed plugins) are removed too."
	fi
	[ "$DRY" = 1 ] && say "Dry run: nothing will be changed."
	ask "Continue? [Y/n]" y || {
		say "Nothing changed."
		exit 0
	}
	if [ -d /run/systemd/system ]; then
		run systemctl disable --quiet --now "$UNIT" || true
	fi
	close_firewall
	run rm -f "$UNIT_FILE"
	if [ -L "$BIN_LINK" ] || [ -f "$BIN_LINK" ]; then
		run rm -f "$BIN_LINK"
	fi
	if [ -f "$PAM_FILE" ] && grep -q 'PAM service for LinuxAdmin' "$PAM_FILE"; then
		run rm -f "$PAM_FILE"
	fi
	run rm -rf "$LIB" /usr/share/linuxadmin "$STATE_DIR/updates" "$FIREWALL_RECORD"
	if [ "$PURGE" = 1 ]; then
		run rm -rf "$CONF_DIR" "$STATE_DIR"
	fi
	if [ -d /run/systemd/system ]; then
		run systemctl daemon-reload
	fi
	if [ "$DRY" = 1 ]; then
		say "Dry run finished: nothing was changed."
	elif [ "$PURGE" = 1 ]; then
		say "LinuxAdmin was removed. Per-user preferences stay in each user's ~/.config/linuxadmin."
	else
		say "LinuxAdmin was removed. Kept: $CONF_DIR (configuration, TLS certificate) and $STATE_DIR (installed plugins); --purge removes them."
	fi
}

cleanup() {
	[ -n "${PARTIAL:-}" ] && rm -rf -- "$PARTIAL"
	[ -n "${TMPD:-}" ] && rm -rf -- "$TMPD"
}

main() {
	set -u
	WANT_VERSION='' PRERELEASE=0 FROM='' UNINSTALL=0 PURGE=0 OPEN_FW=0 DRY=0 YES=0 SKIP_SIG=0
	PARTIAL='' TMPD='' PREVIOUS='' VERSION=''
	while [ $# -gt 0 ]; do
		case $1 in
		--version)
			[ $# -ge 2 ] || die "--version needs a value (X.Y.Z)."
			WANT_VERSION="$2"
			shift
			;;
		--version=*) WANT_VERSION="${1#*=}" ;;
		--prerelease) PRERELEASE=1 ;;
		--from)
			[ $# -ge 2 ] || die "--from needs a folder."
			FROM="$2"
			shift
			;;
		--from=*) FROM="${1#*=}" ;;
		--uninstall) UNINSTALL=1 ;;
		--purge) PURGE=1 ;;
		--open-firewall) OPEN_FW=1 ;;
		--dry-run) DRY=1 ;;
		-y | --yes) YES=1 ;;
		--insecure-skip-signature) SKIP_SIG=1 ;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "Unknown option '$1' (see --help)." ;;
		esac
		shift
	done
	[ "$PURGE" = 1 ] && [ "$UNINSTALL" = 0 ] && die "--purge only goes with --uninstall."
	[ -n "$FROM" ] && { [ -n "$WANT_VERSION" ] || [ "$PRERELEASE" = 1 ]; } && die "--from cannot be combined with --version or --prerelease."

	if [ "$(id -u)" != 0 ]; then
		[ "$DRY" = 1 ] || die "Run it as root: curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sudo sh"
		warn "Not running as root: dry run only."
	fi
	[ "$(uname -s)" = Linux ] || die "LinuxAdmin runs on Linux only."
	if [ ! -d /run/systemd/system ]; then
		[ "$DRY" = 1 ] || die "systemd is not running on this machine; LinuxAdmin needs it."
		warn "systemd is not running here (dry run continues)."
	fi
	detect_arch
	detect_family
	m_missing=''
	for m_tool in tar gzip awk; do
		have "$m_tool" || m_missing="$m_missing $m_tool"
	done
	if [ -n "$m_missing" ] && [ "$DRY" = 0 ] && [ "$UNINSTALL" = 0 ]; then
		say "Installing missing tools:$m_missing"
		# shellcheck disable=SC2046,SC2086
		pkg_install $(printf '%s\n' $m_missing | sed 's/^awk$/gawk/') || true
	fi
	for m_tool in tar gzip awk sed mktemp; do
		have "$m_tool" || die "'$m_tool' is missing."
	done
	if [ "$UNINSTALL" = 0 ] && [ -z "$FROM" ] && ! have curl && ! have wget; then
		die "Neither curl nor wget is installed."
	fi
	umask 022
	TMPD="$(mktemp -d)" || die "mktemp failed."
	trap cleanup EXIT
	trap 'exit 130' INT TERM

	if [ "$UNINSTALL" = 1 ]; then
		do_uninstall
	else
		do_install
	fi
}

# Everything above only defines functions: a truncated download runs nothing.
main "$@"
