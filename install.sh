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
# openssl 3, or a built-in Python verifier on older systems such as Amazon
# Linux 2, against the release key below) and its sha256, installs it into
# the versioned layout that self-update uses (docs/RELEASING.md):
#
#   /usr/lib/linuxadmin/versions/<version>/{bin,web,plugins,packaging,VERSION}
#   /usr/lib/linuxadmin/current  -> versions/<version>
#   /usr/lib/linuxadmin/previous -> versions/<version before>   (rollback)
#   /usr/bin/linuxadmind         -> /usr/lib/linuxadmin/current/bin/linuxadmind
#
# writes the PAM service for this distribution and the systemd unit, makes
# sure sudo is installed, and starts linuxadmin.service. On a first install it
# asks a few questions (port, who can reach it, root sign-in, who may sign in,
# admin unlock time, TLS, Caddy) and writes /etc/linuxadmin/linuxadmin.conf; answers come from the
# terminal even when the script is piped, and --yes or no terminal means the
# defaults. Running it again upgrades or repairs the installation and keeps the
# configuration. LinuxAdmin installed this way
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
#   -y, --yes                  do not ask questions: every answer is the default (the firewall is
#                              only opened with --open-firewall, Caddy only changed with --caddy)
#   --insecure-skip-signature  install even when no tool here can verify ed25519 signatures
#                              (the sha256 is still checked, but against an unverified list)
#   --port N                   port to listen on (default 9090; asked when it is in use)
#   --listen all|local|IP      listen on all interfaces (default), on 127.0.0.1 only, or on one address
#   --allow-root               allow signing in as root (default: no); --no-allow-root
#   --allow-users a,b          only these users may sign in (auth.allow_users)
#   --allow-groups g1,g2       members of these groups may sign in (auth.allow_groups)
#   --admins-only              only administrators (sudo, wheel, admin) may sign in (auth.admins_only);
#                              the three options add up; none of them = every local account
#   --admin-unlock D           how long administrator rights stay unlocked: 5m (default), 15m, 1h,
#                              any 30s-24h, or signout (until sign-out)
#   --tls-cert F --tls-key F   use your own certificate instead of the self-signed one
#   --behind-proxy             a reverse proxy on this machine fronts LinuxAdmin (plain HTTP on 127.0.0.1)
#   --origin URL               browser origin to accept (web.allowed_origins), e.g. https://admin.example.org
#   --trusted-proxy ADDR       address or CIDR of a reverse proxy to trust (web.trusted_proxies)
#   --caddy | --no-caddy       set up (or never touch) a Caddy found on this machine or in Docker
#   --domain NAME              the (sub)domain Caddy serves LinuxAdmin on; implies --caddy
#   --reconfigure              ask the configuration questions again on an installed system
#   --no-enable, --no-start    do not enable at boot / do not start the service
#   -h, --help                 show this help
#
# The script never runs downloaded code other than the verified release
# binaries (`linuxadmind --version`), and never turns off TLS verification of
# its downloads.

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

# ---------------------------------------------------------------- questions
#
# The script is usually read from a pipe (curl | sh), so stdin is the script
# itself: answers are read from the terminal (/dev/tty), never from stdin.
# With --yes, or when there is no terminal, nothing is asked and every
# question takes its default; nothing here can hang.

tty_check() {
	TTY_OK=0
	[ "$YES" = 1 ] && return 0
	if ( : </dev/tty ) 2>/dev/null; then TTY_OK=1; fi
	return 0
}

# read_tty: reads one trimmed line from the terminal into REPLY. At end of
# input (Ctrl-D) it returns 1 and stops asking further questions.
read_tty() {
	REPLY=
	if [ "$TTY_OK" = 1 ] && IFS= read -r REPLY </dev/tty; then
		REPLY="$(printf '%s\n' "$REPLY" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
		return 0
	fi
	TTY_OK=0
	REPLY=
	return 1
}

# ask QUESTION DEFAULT(y|n): yes/no question.
ask() {
	if [ "$TTY_OK" = 0 ]; then
		[ "$2" = y ]
		return
	fi
	printf '%s ' "$1"
	if ! read_tty; then
		printf '\n'
		[ "$2" = y ]
		return
	fi
	case $REPLY in
	[Yy]*) return 0 ;;
	[Nn]*) return 1 ;;
	'') [ "$2" = y ] ;;
	*) return 1 ;;
	esac
}

# ask_value PROMPT DEFAULT: sets REPLY (the default for an empty answer).
ask_value() {
	if [ "$TTY_OK" = 0 ]; then
		REPLY=$2
		return 0
	fi
	printf '%s [%s]: ' "$1" "$2"
	if ! read_tty; then
		printf '\n'
		REPLY=$2
		return 0
	fi
	[ -n "$REPLY" ] || REPLY=$2
	return 0
}

# ask_choice QUESTION DEFAULT_NUMBER OPTION...: numbered menu, sets CHOICE.
ask_choice() {
	ac_q=$1
	ac_def=$2
	shift 2
	ac_n=$#
	while :; do
		say "$ac_q"
		ac_i=1
		for ac_o in "$@"; do
			if [ "$ac_i" = "$ac_def" ]; then say "  $ac_i) $ac_o (default)"; else say "  $ac_i) $ac_o"; fi
			ac_i=$((ac_i + 1))
		done
		ask_value "Choice" "$ac_def"
		case $REPLY in
		'' | *[!0-9]*) ;;
		*)
			if [ "$REPLY" -ge 1 ] && [ "$REPLY" -le "$ac_n" ]; then
				CHOICE=$REPLY
				return 0
			fi
			;;
		esac
		say "Type a number from 1 to $ac_n."
	done
}

# opt_set OPTION VALUE: checks a configuration option; a value given on the
# command line is an answer, so its question is not asked.
opt_set() {
	CFG_REQUESTED=1
	case $1 in
	--port)
		valid_port "$2" || die "--port must be a number from 1 to 65535."
		F_PORT=$2
		;;
	--listen)
		case $2 in
		all | local) F_LISTEN=$2 ;;
		*) printf '%s\n' "$2" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$|^[0-9A-Fa-f:]*:[0-9A-Fa-f:]+$' || die "--listen is all, local or an IP address."
			F_LISTEN=$2
			;;
		esac
		;;
	--admin-unlock)
		case $2 in
		signout | until-signout | off | 0) F_UNLOCK=0s ;;
		*) valid_unlock "$2" || die "--admin-unlock is 30s to 24h (5m, 15m, 1h...) or 'signout'."
			F_UNLOCK=$2
			;;
		esac
		;;
	--allow-users)
		parse_names "$2" || die "--allow-users: '$PN_BAD' is not a valid user name."
		# shellcheck disable=SC2086
		F_AUSERS="$(list_add_all "$F_AUSERS" $PN_OUT)"
		;;
	--allow-groups)
		parse_names "$2" || die "--allow-groups: '$PN_BAD' is not a valid group name."
		# shellcheck disable=SC2086
		F_AGROUPS="$(list_add_all "$F_AGROUPS" $PN_OUT)"
		;;
	--tls-cert) F_CERT=$2 ;;
	--tls-key) F_KEY=$2 ;;
	--origin)
		valid_origin "$2" || die "--origin must look like https://host or https://host:port."
		F_ORIGINS="$(list_add "$F_ORIGINS" "$2")"
		;;
	--trusted-proxy)
		valid_proxy_addr "$2" || die "--trusted-proxy is an IP address or a CIDR such as 172.17.0.0/16."
		F_TRUSTED="$(list_add "$F_TRUSTED" "$2")"
		;;
	--domain)
		valid_domain "$2" || die "--domain must be a domain name such as linuxadmin.example.org."
		F_DOMAIN=$2
		;;
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
  -y, --yes                  do not ask questions: every answer is the default
                             (the firewall is only opened with --open-firewall, Caddy only changed with --caddy)
  --insecure-skip-signature  install even when no tool here can verify ed25519 signatures
  -h, --help                 show this help

Configuration (first install; asked unless given here, --yes takes the defaults):
  --port N                   port to listen on (default 9090; asked again when it is in use)
  --listen all|local|IP      all interfaces (default), 127.0.0.1 only, or one address
  --allow-root               allow signing in as root (default: no); --no-allow-root
  --allow-users a,b          only these users may sign in
  --allow-groups g1,g2       members of these groups may sign in
  --admins-only              only administrators (sudo, wheel, admin) may sign in
                             (these three add up; none of them = every local account)
  --admin-unlock D           5m (default), 15m, 1h, any 30s-24h, or signout (until sign-out)
  --tls-cert F --tls-key F   use your own certificate instead of the self-signed one
  --behind-proxy             a reverse proxy on this machine fronts LinuxAdmin (plain HTTP on 127.0.0.1)
  --origin URL               browser origin to accept (web.allowed_origins)
  --trusted-proxy ADDR       address or CIDR of a reverse proxy to trust (web.trusted_proxies)
  --caddy | --no-caddy       set up (or never touch) a Caddy found on this machine or in Docker
  --domain NAME              the (sub)domain Caddy serves LinuxAdmin on (implies --caddy)
  --reconfigure              ask the configuration questions again on an installed system
  --no-enable, --no-start    do not enable at boot / do not start the service
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

# The signature check. openssl 3 verifies ed25519 itself (pkeyutl -rawin).
# OpenSSL 1.0.2 and 1.1.1 (Amazon Linux 2, CentOS 7) cannot, so a small Python
# verifier (Python 2.7 or 3, no modules to install) is the fallback.
write_pyverify() {
	cat >"$TMPD/ed25519.py" <<'PY'
# Ed25519 signature check after RFC 8032 section 6 (reference implementation),
# for Python 2.7 and 3. usage: ed25519.py PUBLIC_KEY_PEM MESSAGE_FILE BASE64_SIGNATURE_FILE
import base64, hashlib, sys
p = 2 ** 255 - 19
q = 2 ** 252 + 27742317777372353535851937790883648493
d = -121665 * pow(121666, p - 2, p) % p
sqrt_m1 = pow(2, (p - 1) // 4, p)
def le(b):
    return sum(int(x) << (8 * i) for i, x in enumerate(bytearray(b)))
def add(P, Q):
    A = (P[1] - P[0]) * (Q[1] - Q[0]) % p
    B = (P[1] + P[0]) * (Q[1] + Q[0]) % p
    C = 2 * P[3] * Q[3] * d % p
    D = 2 * P[2] * Q[2] % p
    E, F, G, H = B - A, D - C, D + C, B + A
    return (E * F % p, G * H % p, F * G % p, E * H % p)
def mul(s, P):
    Q = (0, 1, 1, 0)
    while s > 0:
        if s & 1:
            Q = add(Q, P)
        P = add(P, P)
        s >>= 1
    return Q
def equal(P, Q):
    return (P[0] * Q[2] - Q[0] * P[2]) % p == 0 and (P[1] * Q[2] - Q[1] * P[2]) % p == 0
def recover_x(y, sign):
    if y >= p:
        return None
    x2 = (y * y - 1) * pow(d * y * y + 1, p - 2, p) % p
    if x2 == 0:
        return None if sign else 0
    x = pow(x2, (p + 3) // 8, p)
    if (x * x - x2) % p != 0:
        x = x * sqrt_m1 % p
    if (x * x - x2) % p != 0:
        return None
    if (x & 1) != sign:
        x = p - x
    return x
def decompress(s):
    if len(s) != 32:
        return None
    y = le(s)
    sign = y >> 255
    y &= (1 << 255) - 1
    x = recover_x(y, sign)
    if x is None:
        return None
    return (x, y, 1, x * y % p)
gy = 4 * pow(5, p - 2, p) % p
gx = recover_x(gy, 0)
G = (gx, gy, 1, gx * gy % p)
def verify(public, msg, sig):
    if len(public) != 32 or len(sig) != 64:
        return False
    A = decompress(public)
    R = decompress(sig[:32])
    if A is None or R is None:
        return False
    s = le(sig[32:])
    if s >= q:
        return False
    h = le(hashlib.sha512(sig[:32] + public + msg).digest()) % q
    return equal(mul(s, G), add(R, mul(h, A)))
def main():
    pem = open(sys.argv[1], 'rb').read().decode('ascii').split('\n')
    der = base64.b64decode(''.join(l for l in pem if l and not l.startswith('-----')))
    msg = open(sys.argv[2], 'rb').read()
    sig = base64.b64decode(open(sys.argv[3], 'rb').read().strip())
    sys.exit(0 if verify(der[-32:], msg, sig) else 1)
try:
    main()
except SystemExit:
    raise
except Exception:
    sys.exit(2)
PY
}

# ed25519_verify PEM_FILE MESSAGE_FILE BASE64_SIGNATURE_FILE (uses VERIFIER, VBIN)
ed25519_verify() {
	case $VERIFIER in
	openssl)
		"$VBIN" base64 -d -A -in "$3" -out "$3.bin" 2>/dev/null || return 1
		"$VBIN" pkeyutl -verify -pubin -inkey "$1" -rawin -in "$2" -sigfile "$3.bin" >/dev/null 2>&1
		;;
	python) "$VBIN" "$TMPD/ed25519.py" "$1" "$2" "$3" >/dev/null 2>&1 ;;
	*) return 1 ;;
	esac
}

# Sets CAN_VERIFY=1, VERIFIER (openssl or python) and VBIN to the first tool
# that passes the RFC 8032 test vector (and refuses a wrong message):
# openssl (3 or newer), an openssl3 binary, python3, python, python2.
verifier_selftest() {
	CAN_VERIFY=0
	VERIFIER=
	VBIN=
	printf '%s\n' "$SELFTEST_KEY_PEM" >"$TMPD/selftest.pem"
	printf 'r' >"$TMPD/selftest.msg"
	printf 'x' >"$TMPD/selftest.bad"
	write_pyverify
	for vs_c in openssl openssl3 python3 python python2; do
		have "$vs_c" || continue
		case $vs_c in openssl*) VERIFIER=openssl ;; *) VERIFIER=python ;; esac
		VBIN=$vs_c
		printf '%s\n' "$SELFTEST_SIG" >"$TMPD/selftest.sig"
		ed25519_verify "$TMPD/selftest.pem" "$TMPD/selftest.msg" "$TMPD/selftest.sig" || continue
		if ed25519_verify "$TMPD/selftest.pem" "$TMPD/selftest.bad" "$TMPD/selftest.sig"; then
			continue
		fi
		CAN_VERIFY=1
		return 0
	done
	VERIFIER=
	VBIN=
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

# Sets LISTEN, LISTEN_HOST and PORT: what was decided in this run, else the
# configuration file, else the default (0.0.0.0:9090).
read_listen() {
	LISTEN="${C_LISTEN:-}"
	[ -n "$LISTEN" ] || LISTEN="$(conf_get '' listen | tr -d "\"'")"
	[ -n "$LISTEN" ] || LISTEN="0.0.0.0:9090"
	PORT="${LISTEN##*:}"
	LISTEN_HOST="${LISTEN%:*}"
	case $PORT in '' | *[!0-9]*) PORT=9090 ;; esac
}

tls_mode() {
	if [ -n "${C_TLS:-}" ]; then
		printf '%s\n' "$C_TLS"
		return 0
	fi
	tm="$(conf_get tls mode | tr -d "\"'")"
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
	if [ -n "$PROXY" ]; then
		say "LinuxAdmin sits behind a reverse proxy: the firewall is not changed for port $PORT."
		return 0
	fi
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

# ---------------------------------------------------------------- port

# port_in_use PORT: returns 0 when a process listens on the TCP port and sets
# PORT_OWNER to what it is ("" when it cannot be told). A running linuxadmind
# does not count (re-running the installer keeps the port).
port_in_use() {
	pi_p=$1
	PORT_OWNER=
	pi_line=
	pi_found=0
	if have ss && ss -ltn >/dev/null 2>&1; then
		pi_line="$(ss -ltnp 2>/dev/null | awk -v p=":$pi_p" '{ n = length($4); if (n >= length(p) && substr($4, n - length(p) + 1) == p) { print; exit } }')"
		[ -n "$pi_line" ] && pi_found=1 && PORT_OWNER="$(printf '%s\n' "$pi_line" | sed -n 's/.*users:(("\([^"]*\)".*/\1/p')"
	elif have netstat && netstat -ltn >/dev/null 2>&1; then
		pi_line="$(netstat -ltnp 2>/dev/null | awk -v p=":$pi_p" '{ n = length($4); if (n >= length(p) && substr($4, n - length(p) + 1) == p) { print; exit } }')"
		[ -n "$pi_line" ] && pi_found=1 && PORT_OWNER="$(printf '%s\n' "$pi_line" | awk '{ print $NF }' | sed -n 's|^[0-9][0-9]*/||p')"
	elif [ -r /proc/net/tcp ]; then
		pi_hex="$(printf '%04X' "$pi_p")"
		pi_inode="$(cat /proc/net/tcp /proc/net/tcp6 2>/dev/null | awk -v hp="$pi_hex" '$4 == "0A" { n = split($2, a, ":"); if (a[n] == hp) { print $10; exit } }')"
		if [ -n "$pi_inode" ]; then
			pi_found=1
			for pi_fd in /proc/[0-9]*/fd/*; do
				if [ "$(readlink "$pi_fd" 2>/dev/null)" = "socket:[$pi_inode]" ]; then
					pi_pid="${pi_fd#/proc/}"
					pi_pid="${pi_pid%%/*}"
					PORT_OWNER="$(cat "/proc/$pi_pid/comm" 2>/dev/null)"
					break
				fi
			done
		fi
	fi
	[ "$pi_found" = 1 ] || return 1
	case $PORT_OWNER in
	linuxadmind) return 1 ;;
	systemd | init)
		# socket activation: name the socket unit that owns the port
		pi_unit=
		have systemctl && pi_unit="$(systemctl list-sockets --no-legend --no-pager 2>/dev/null | awk -v p=":$pi_p" '{ n = length($1); if (substr($1, n - length(p) + 1) == p) { print $2; exit } }')"
		PORT_OWNER="systemd socket ${pi_unit:-unit}"
		;;
	docker-proxy) PORT_OWNER="Docker (a published container port)" ;;
	esac
	case $PORT_OWNER in
	*cockpit*) PORT_OWNER="Cockpit ($PORT_OWNER)" ;;
	esac
	if [ -z "$PORT_OWNER" ] && [ "$pi_p" = 9090 ] && have systemctl && systemctl is-active --quiet cockpit.socket 2>/dev/null; then
		PORT_OWNER="Cockpit (cockpit.socket)"
	fi
	return 0
}

# next_free_port START: prints the first port from START on that nothing listens on.
next_free_port() {
	nf_p=$1
	while [ "$nf_p" -lt 65535 ] && port_in_use "$nf_p"; do
		nf_p=$((nf_p + 1))
	done
	printf '%s\n' "$nf_p"
}

valid_port() {
	case $1 in '' | *[!0-9]*) return 1 ;; esac
	[ "${#1}" -le 5 ] && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]
}

# choose_port: sets C_PORT (asks; --port is the first answer).
choose_port() {
	cp_try=$C_PORT
	cp_ask=1
	[ -n "$F_PORT" ] && cp_try=$F_PORT && cp_ask=0
	while :; do
		if [ "$cp_ask" = 1 ]; then
			ask_value "Port" "$cp_try"
			cp_try=$REPLY
		fi
		cp_ask=1
		if ! valid_port "$cp_try"; then
			[ "$TTY_OK" = 1 ] || die "'$cp_try' is not a valid port."
			say "'$cp_try' is not a port number (1-65535)."
			cp_try=$C_PORT
			continue
		fi
		if port_in_use "$cp_try"; then
			cp_next="$(next_free_port $((cp_try + 1)))"
			if [ "$TTY_OK" = 1 ]; then
				say "Port $cp_try is in use${PORT_OWNER:+ by $PORT_OWNER}."
				if ask "Use it anyway (free it before the service starts, or LinuxAdmin will not start)? [y/N]" n; then
					break
				fi
				say "Pick another one; $cp_next is free."
				cp_try=$cp_next
				continue
			elif [ -n "$F_PORT" ]; then
				die "Port $cp_try is in use${PORT_OWNER:+ by $PORT_OWNER}. Pick another with --port."
			fi
			say "Port $cp_try is in use${PORT_OWNER:+ by $PORT_OWNER}; using $cp_next instead (change it with --port)."
			cp_try=$cp_next
		fi
		break
	done
	C_PORT=$cp_try
}

# ---------------------------------------------------------------- configuration file

TAB="$(printf '\t')"

# conf_get SECTION KEY: the raw value of a key in the configuration file.
conf_get() {
	[ -r "$CONF_FILE" ] || return 0
	awk -v sec="$1" -v key="$2" '
	BEGIN { insec = (sec == "") }
	/^[[:space:]]*\[/ { h = $0; sub(/^[[:space:]]*\[/, "", h); sub(/\].*$/, "", h); insec = (h == sec); next }
	insec && $0 ~ "^[[:space:]]*" key "[[:space:]]*=" { sub(/^[^=]*=[[:space:]]*/, ""); sub(/[[:space:]]+#.*$/, ""); sub(/[[:space:]]+$/, ""); print; exit }
	' "$CONF_FILE"
}

# conf_set SECTION KEY RAWVALUE: sets "KEY = RAWVALUE" in [SECTION] ("" is the
# top of the file) and leaves everything else, comments included, as it is.
conf_set() {
	CS_VAL="$3" awk -v sec="$1" -v key="$2" '
	BEGIN { insec = (sec == ""); seen = insec; done = 0; val = ENVIRON["CS_VAL"] }
	/^[[:space:]]*\[/ {
		if (insec && !done) { print key " = " val; done = 1 }
		h = $0; sub(/^[[:space:]]*\[/, "", h); sub(/\].*$/, "", h)
		insec = (h == sec); if (insec) seen = 1
		print; next
	}
	insec && !done && $0 ~ "^[[:space:]]*" key "[[:space:]]*=" { print key " = " val; done = 1; next }
	{ print }
	END { if (!done) { if (!seen) { print ""; print "[" sec "]" } print key " = " val } }
	' "$CONF_FILE" >"$CONF_FILE.new.$$" && mv -f "$CONF_FILE.new.$$" "$CONF_FILE"
}

# list_add "a b" c: prints the list with c appended unless it is there.
list_add() {
	case " $1 " in
	*" $2 "*) printf '%s\n' "$1" ;;
	*) printf '%s\n' "${1:+$1 }$2" ;;
	esac
}

# toml_list a b c: prints ["a", "b", "c"].
toml_list() {
	tl_out=
	for tl_i in "$@"; do tl_out="${tl_out:+$tl_out, }\"$tl_i\""; done
	printf '[%s]\n' "$tl_out"
}

# Defaults for the questions: the built-in ones, or what the existing file says.
load_current() {
	D_PORT=9090 D_HOST=0.0.0.0 D_ROOT=false D_UNLOCK=5m D_TLS=self-signed D_CERT='' D_KEY='' D_ORIGINS='' D_PROXIES='' D_AUSERS='' D_AGROUPS='' D_ADMINS=false
	CFG_EXISTS=0
	[ -f "$CONF_FILE" ] || return 0
	CFG_EXISTS=1
	lc_l="$(conf_get '' listen | tr -d "\"'")"
	if [ -n "$lc_l" ]; then
		D_PORT="${lc_l##*:}"
		D_HOST="${lc_l%:*}"
	fi
	case $D_PORT in '' | *[!0-9]*) D_PORT=9090 ;; esac
	[ -n "$D_HOST" ] || D_HOST=0.0.0.0
	[ "$(conf_get '' allow_root)" = true ] && D_ROOT=true
	lc_v="$(conf_get session admin_unlock | tr -d "\"'")"
	[ -n "$lc_v" ] && D_UNLOCK=$lc_v
	lc_v="$(conf_get tls mode | tr -d "\"'")"
	[ -n "$lc_v" ] && D_TLS=$lc_v
	D_CERT="$(conf_get tls cert | tr -d "\"'")"
	D_KEY="$(conf_get tls key | tr -d "\"'")"
	D_ORIGINS="$(conf_get web allowed_origins | tr -d '[]"' | tr ',' ' ' | tr -s ' ' | sed 's/^ //;s/ $//')"
	D_PROXIES="$(conf_get web trusted_proxies | tr -d '[]"' | tr ',' ' ' | tr -s ' ' | sed 's/^ //;s/ $//')"
	D_AUSERS="$(conf_get auth allow_users | tr -d '[]"' | tr ',' ' ' | tr -s ' ' | sed 's/^ //;s/ $//')"
	D_AGROUPS="$(conf_get auth allow_groups | tr -d '[]"' | tr ',' ' ' | tr -s ' ' | sed 's/^ //;s/ $//')"
	[ "$(conf_get auth admins_only)" = true ] && D_ADMINS=true
	return 0
}

# Seconds in "90s", "5m", "12h"; 0 for anything else.
duration_seconds() {
	case $1 in
	*[!0-9smh]* | '') echo 0 ;;
	*s) echo "${1%s}" ;;
	*m) echo $((${1%m} * 60)) ;;
	*h) echo $((${1%h} * 3600)) ;;
	*) echo 0 ;;
	esac
}

valid_unlock() {
	[ "$1" = 0s ] && return 0
	case $1 in [0-9]*[smh]) ;; *) return 1 ;; esac
	vu_n="$(duration_seconds "$1")"
	[ "$vu_n" -ge 30 ] && [ "$vu_n" -le 86400 ]
}

valid_path() {
	case $1 in /*) ;; *) return 1 ;; esac
	printf '%s\n' "$1" | grep -Eq '^/[A-Za-z0-9._/+@:-]*$'
}

valid_origin() {
	printf '%s\n' "$1" | grep -Eq '^https?://[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$'
}

valid_proxy_addr() {
	printf '%s\n' "$1" | grep -Eq '^[0-9A-Fa-f:.]+(/[0-9]{1,3})?$'
}

valid_domain() {
	printf '%s\n' "$1" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$'
}

# list_add_all "a b" c d: the list with each further name appended once.
list_add_all() {
	laa_l=$1
	shift
	for laa_n in "$@"; do laa_l="$(list_add "$laa_l" "$laa_n")"; done
	printf '%s\n' "$laa_l"
}

# valid_name NAME: a user or group name as the daemon accepts it (auth.allow_users).
valid_name() {
	printf '%s\n' "$1" | grep -Eq '^[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}\$?$'
}

# parse_names "a, b c": sets PN_OUT to the names separated by spaces, without
# duplicates. Returns 1 and sets PN_BAD when one is not a valid name.
parse_names() {
	PN_BAD=
	PN_OUT=
	for pn_n in $(printf '%s\n' "$1" | tr ',' ' '); do
		if ! valid_name "$pn_n"; then
			PN_BAD=$pn_n
			return 1
		fi
		PN_OUT="$(list_add "$PN_OUT" "$pn_n")"
	done
	return 0
}

# auth_text: the sign-in policy in words.
auth_text() {
	at_out=
	[ -n "$C_AUSERS" ] && at_out="users $C_AUSERS"
	[ -n "$C_AGROUPS" ] && at_out="${at_out:+$at_out; }groups $C_AGROUPS"
	[ "$C_ADMINS" = true ] && at_out="${at_out:+$at_out; }administrators (sudo, wheel, admin)"
	[ -n "$at_out" ] || at_out="every local account"
	printf '%s\n' "$at_out"
}

# auth_covers USER: success when the sign-in policy lets USER in.
auth_covers() {
	case " $C_AUSERS " in *" $1 "*) return 0 ;; esac
	ac_groups=" $(id -nG "$1" 2>/dev/null | tr '\n' ' ')"
	for ac_g in $C_AGROUPS; do
		case $ac_groups in *" $ac_g "*) return 0 ;; esac
	done
	if [ "$C_ADMINS" = true ]; then
		case $ac_groups in *" sudo "* | *" wheel "* | *" admin "*) return 0 ;; esac
	fi
	return 1
}

# auth_warnings: points out policies that would lock people out.
auth_warnings() {
	[ -n "$C_AUSERS$C_AGROUPS" ] || [ "$C_ADMINS" = true ] || return 0
	if have getent; then
		for aw_u in $C_AUSERS; do
			getent passwd "$aw_u" >/dev/null 2>&1 || say "  Note: there is no user '$aw_u' on this machine (yet)."
		done
		for aw_g in $C_AGROUPS; do
			getent group "$aw_g" >/dev/null 2>&1 || say "  Note: there is no group '$aw_g' on this machine (yet)."
		done
		if [ "$C_ADMINS" = true ] && [ "$C_ROOT" != true ] && ! getent group sudo wheel admin 2>/dev/null | awk -F: '$4 != "" { f = 1 } END { exit !f }'; then
			say "  Note: no account is in the sudo, wheel or admin group, so nobody could sign in with 'only administrators'."
		fi
	fi
	aw_me="${SUDO_USER:-}"
	if [ -n "$aw_me" ] && [ "$aw_me" != root ] && ! auth_covers "$aw_me"; then
		say "  Note: your own account '$aw_me' is not covered, so you could not sign in to LinuxAdmin yourself."
	fi
	return 0
}

# render_auth: the [auth] keys of a first-install configuration file.
render_auth() {
	cat <<'CONF'
# Sign in with an SSH key listed in the user's ~/.ssh/authorized_keys.
# ssh_keys = true
# Who may sign in. With none of the three below set, every local account may.
# Otherwise an account must be listed in allow_users, be in one of the
# allow_groups (primary or supplementary group), or, with admins_only, be an
# administrator (member of sudo, wheel or admin; root when allow_root is on).
CONF
	if [ -n "$C_AUSERS" ]; then
		# shellcheck disable=SC2086
		printf 'allow_users = %s\n' "$(toml_list $C_AUSERS)"
	else
		printf '# allow_users = ["alice", "bob"]\n'
	fi
	if [ -n "$C_AGROUPS" ]; then
		# shellcheck disable=SC2086
		printf 'allow_groups = %s\n' "$(toml_list $C_AGROUPS)"
	else
		printf '# allow_groups = ["wheel", "operators"]\n'
	fi
	if [ "$C_ADMINS" = true ]; then
		printf 'admins_only = true\n'
	else
		printf '# admins_only = false\n'
	fi
}

# Writes the commented configuration file (stdout) for a first install.
render_config() {
	cat <<CONF
# LinuxAdmin server configuration, written by the installer on $(date +%Y-%m-%d).
# Every key and its default is described in docs/api/config.md. Settings that
# say "restart" need: systemctl restart linuxadmin. Saving from Settings in the
# web interface rewrites this file without the comments (a backup stays next
# to it as linuxadmin.conf.bak).

# Address and port to listen on. 0.0.0.0 means every network interface,
# 127.0.0.1 only this machine (use that behind a reverse proxy). Restart.
listen = "$C_HOST:$C_PORT"

# Allow signing in as root. Default false: sign in as a normal user and unlock
# administrator rights with that user's password (sudo).
allow_root = $C_ROOT

[login]
# show_ip = true
# Failed sign-ins per client address in 15 minutes before it is blocked.
# max_failures = 5

[auth]
$(render_auth)

[session]
# Idle time before a session ends.
# timeout = "12h"
# How long administrator rights stay unlocked while in use: "30s" to "24h",
# or "0s" to keep them until sign-out.
admin_unlock = "$C_UNLOCK"

[tls]
# "self-signed": a certificate is created on first start (/etc/linuxadmin/tls).
# "custom": use cert and key below.
# "http": plain HTTP, only allowed while listen is 127.0.0.1 or [::1], for a
# reverse proxy on this machine that provides HTTPS (and sends
# X-Forwarded-Proto, so the session cookie stays Secure). Restart.
mode = "$C_TLS"
# Plain HTTP on the same port is redirected to HTTPS.
# redirect = true
CONF
	if [ "$C_TLS" = custom ]; then
		printf 'cert = "%s"\nkey = "%s"\n' "$C_CERT" "$C_KEY"
	else
		printf '# cert = "/etc/ssl/certs/linuxadmin.pem"\n# key = "/etc/ssl/private/linuxadmin.key"\n'
	fi
	cat <<CONF

[web]
# Browser origins accepted besides the address in the Host header, for
# reaching LinuxAdmin through a reverse proxy.
CONF
	if [ -n "$C_ORIGINS" ]; then
		# shellcheck disable=SC2086
		printf 'allowed_origins = %s\n' "$(toml_list $C_ORIGINS)"
	else
		printf '# allowed_origins = ["https://linuxadmin.example.org"]\n'
	fi
	printf '# Addresses whose X-Forwarded-* headers are believed (the reverse proxy).\n'
	if [ -n "$C_PROXIES" ]; then
		# shellcheck disable=SC2086
		printf 'trusted_proxies = %s\n' "$(toml_list $C_PROXIES)"
	else
		printf '# trusted_proxies = ["127.0.0.0/8", "::1/128"]\n'
	fi
	cat <<'CONF'

[plugins]
# Run plugins that are not signed by a trusted key.
# allow_unsigned = false

[updates]
# "stable" or "prerelease"
# channel = "stable"
# auto_check = true
# auto_install = false
# auto_install_at = "03:30"
CONF
}

# write_config: first install writes the commented file; with an existing file
# only the keys the questions cover are changed (the rest stays, comments too).
write_config() {
	[ "$CFG_MODE" = keep ] && return 0
	if [ "$CFG_MODE" = new ]; then
		step "Writing $CONF_FILE"
		render_config >"$TMPD/linuxadmin.conf"
		if [ "$DRY" = 1 ]; then
			say "  [dry-run] would write:"
			sed 's/^/    | /' "$TMPD/linuxadmin.conf"
		else
			install -D -m 644 "$TMPD/linuxadmin.conf" "$CONF_FILE"
		fi
		return 0
	fi
	step "Updating $CONF_FILE"
	if [ "$DRY" = 1 ]; then
		say "  [dry-run] listen = \"$C_HOST:$C_PORT\", allow_root = $C_ROOT, session.admin_unlock = \"$C_UNLOCK\", tls.mode = \"$C_TLS\""
		[ -n "$C_ORIGINS" ] && say "  [dry-run] web.allowed_origins = $C_ORIGINS"
		[ -n "$C_PROXIES" ] && say "  [dry-run] web.trusted_proxies = $C_PROXIES"
		say "  [dry-run] who may sign in: $(auth_text)"
		return 0
	fi
	wc_bak="$CONF_FILE.linuxadmin-backup-$(date +%Y%m%d-%H%M%S)"
	cp -p "$CONF_FILE" "$wc_bak"
	say "Backup: $wc_bak"
	WC_BAK=$wc_bak
	conf_set '' listen "\"$C_HOST:$C_PORT\""
	conf_set '' allow_root "$C_ROOT"
	conf_set session admin_unlock "\"$C_UNLOCK\""
	conf_set tls mode "\"$C_TLS\""
	if [ "$C_TLS" = custom ]; then
		conf_set tls cert "\"$C_CERT\""
		conf_set tls key "\"$C_KEY\""
	fi
	if [ -n "$C_AUSERS$C_AGROUPS" ] || [ "$C_ADMINS" = true ] || [ -n "$(conf_get auth allow_users)$(conf_get auth allow_groups)$(conf_get auth admins_only)" ]; then
		# shellcheck disable=SC2086
		conf_set auth allow_users "$(toml_list $C_AUSERS)"
		# shellcheck disable=SC2086
		conf_set auth allow_groups "$(toml_list $C_AGROUPS)"
		conf_set auth admins_only "$C_ADMINS"
	fi
	# shellcheck disable=SC2086
	[ -n "$C_ORIGINS" ] && conf_set web allowed_origins "$(toml_list $C_ORIGINS)"
	# shellcheck disable=SC2086
	[ -n "$C_PROXIES" ] && conf_set web trusted_proxies "$(toml_list $C_PROXIES)"
	return 0
}

# check_config: runs `linuxadmind --check-config` on the file before the
# service is (re)started. When the file the installer just wrote is not valid
# the previous one is put back (a first install removes it).
check_config() {
	[ "$DRY" = 1 ] && return 0
	[ -f "$CONF_FILE" ] || return 0
	if ! "$BIN_LINK" --help 2>&1 | grep -q -- '-check-config'; then
		say "This version cannot check the configuration beforehand (no --check-config); continuing."
		return 0
	fi
	step "Checking $CONF_FILE"
	if "$BIN_LINK" --check-config "$CONF_FILE" >"$TMPD/check.out" 2>&1; then
		say "$(head -n 1 "$TMPD/check.out")"
		return 0
	fi
	sed 's/^/  | /' "$TMPD/check.out" >&2
	case $CFG_MODE in
	change)
		if [ -n "$WC_BAK" ] && cp -p "$WC_BAK" "$CONF_FILE"; then
			die "The changed configuration is not valid (see above). Your previous configuration is back in place; the service was not restarted."
		fi
		die "The changed configuration is not valid (see above) and the backup $WC_BAK could not be restored. Fix $CONF_FILE, then: systemctl restart linuxadmin"
		;;
	new)
		rm -f "$CONF_FILE"
		die "The configuration the installer wrote is not valid (see above) and was removed; the service was not started. Run the installer again with other options."
		;;
	*) die "$CONF_FILE is not valid (see above). Fix it, then: systemctl restart linuxadmin (the service was not restarted)." ;;
	esac
}

# ---------------------------------------------------------------- Caddy
#
# Without a proxy LinuxAdmin serves HTTPS (self-signed by default). Behind a
# proxy on this machine it serves plain HTTP on 127.0.0.1 (tls.mode = "http"):
# the hop never leaves the machine, and the proxy provides the certificate the
# browser sees. A Caddy in a Docker container cannot reach the host's
# 127.0.0.1, so LinuxAdmin then listens on the Docker bridge or on all
# interfaces and keeps HTTPS: Caddy connects with TLS and does not check the
# certificate (tls_insecure_skip_verify). A container with host networking
# counts as this machine.

dk() { DOCKER_HOST='' DOCKER_CONTEXT='' docker -H unix:///var/run/docker.sock "$@"; }

# caddy_detect: sets CADDY_KIND (native, docker or empty) and, for it, CADDY_CF
# (the Caddyfile on this machine, empty when it is not reachable), CADDY_CTR
# and CADDY_CF_IN (container name and the Caddyfile path inside it).
caddy_detect() {
	CADDY_KIND='' CADDY_CF='' CADDY_CTR='' CADDY_CF_IN=''
	CD_HOSTNET=0 CD_GW='' CD_SUBNET='' CD_NOTE=''
	[ "$CADDY_MODE" = no ] && return 0
	cd_unit=
	for cd_u in /etc/systemd/system/caddy.service /usr/lib/systemd/system/caddy.service /lib/systemd/system/caddy.service; do
		[ -f "$cd_u" ] && cd_unit=$cd_u && break
	done
	cd_native=
	if [ -n "$cd_unit" ] || have caddy; then
		cd_cf=
		[ -n "$cd_unit" ] && cd_cf="$(sed -n 's/.*--config[= ]\([^ ]*\).*/\1/p' "$cd_unit" | head -n 1)"
		[ -n "$cd_cf" ] || cd_cf=/etc/caddy/Caddyfile
		[ -f "$cd_cf" ] && cd_native=$cd_cf
	fi
	cd_ctrs=
	if have docker && [ -S /var/run/docker.sock ]; then
		cd_ctrs="$(dk ps --format '{{.Names}} {{.Image}}' 2>/dev/null | awk 'tolower($2) ~ /caddy/ { print $1 }')"
	fi
	if [ -n "$cd_native" ] && [ -n "$cd_ctrs" ] && [ "$TTY_OK" = 1 ]; then
		cd_first="$(printf '%s\n' "$cd_ctrs" | head -n 1)"
		ask_choice "Caddy runs here and in Docker. Which one should serve LinuxAdmin?" 1 "Caddy on this machine ($cd_native)" "Caddy in the container $cd_first"
		[ "$CHOICE" = 2 ] && cd_native=
	fi
	if [ -n "$cd_native" ]; then
		CADDY_KIND=native
		CADDY_CF=$cd_native
		return 0
	fi
	[ -n "$cd_ctrs" ] || return 0
	cd_n="$(printf '%s\n' "$cd_ctrs" | wc -l)"
	CADDY_CTR="$(printf '%s\n' "$cd_ctrs" | head -n 1)"
	if [ "$cd_n" -gt 1 ] && [ "$TTY_OK" = 1 ]; then
		# shellcheck disable=SC2086
		set -- $cd_ctrs
		ask_choice "Several Caddy containers are running. Which one?" 1 "$@"
		CADDY_CTR="$(printf '%s\n' "$cd_ctrs" | sed -n "${CHOICE}p")"
	fi
	CADDY_KIND=docker
	caddy_docker_inspect
}

caddy_docker_inspect() {
	ci_cmd="$(dk inspect -f '{{range .Config.Entrypoint}}{{.}} {{end}}{{range .Config.Cmd}}{{.}} {{end}}' "$CADDY_CTR" 2>/dev/null)"
	CADDY_CF_IN="$(printf '%s\n' "$ci_cmd" | sed -n 's/.*--config[= ]\([^ ]*\).*/\1/p' | head -n 1)"
	[ -n "$CADDY_CF_IN" ] || CADDY_CF_IN=/etc/caddy/Caddyfile
	# The Caddyfile on this machine: the mount that holds that path.
	CADDY_CF="$(dk inspect -f '{{range .Mounts}}{{.Destination}}|{{.Source}}{{"\n"}}{{end}}' "$CADDY_CTR" 2>/dev/null |
		awk -F'|' -v f="$CADDY_CF_IN" '
			$1 == f { print $2; exit }
			index(f, $1 "/") == 1 && length($1) > best { best = length($1); src = $2 substr(f, length($1) + 1) }
			END { if (src != "") print src }')"
	[ -n "$CADDY_CF" ] && [ ! -f "$CADDY_CF" ] && CADDY_CF=''
	ci_mode="$(dk inspect -f '{{.HostConfig.NetworkMode}}' "$CADDY_CTR" 2>/dev/null)"
	if [ "$ci_mode" = host ]; then
		CD_HOSTNET=1
		return 0
	fi
	# shellcheck disable=SC2016
	ci_net="$(dk inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{$v.Gateway}}{{"\n"}}{{end}}' "$CADDY_CTR" 2>/dev/null | awk 'NF == 2 { print; exit }')"
	if [ -n "$ci_net" ]; then
		CD_GW="${ci_net#* }"
		ci_netname="${ci_net%% *}"
		CD_SUBNET="$(dk network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{"\n"}}{{end}}' "$ci_netname" 2>/dev/null | grep -v ':' | head -n 1)"
	else
		CD_NOTE="The container has no bridge network with a gateway address (network mode '$ci_mode')."
	fi
	return 0
}

# caddy_scan FILE: writes one "BLOCK<TAB>first<TAB>last<TAB>addresses" line per
# top-level block of a Caddyfile, or "UNSURE<TAB>reason" when the file uses
# something this parser does not follow (then nothing is edited).
caddy_scan() {
	awk '
	function unsure(why) { if (!bad) print "UNSURE\t" why; bad = 1 }
	function clean(s,   out, i, c, q, n) {
		out = ""; q = ""; n = length(s)
		for (i = 1; i <= n; i++) {
			c = substr(s, i, 1)
			if (q != "") { if (c == "\\" && q == "\"") i++; else if (c == q) q = ""; continue }
			if (c == "\"" || c == "`") { q = c; continue }
			if (c == "#" && (i == 1 || substr(s, i - 1, 1) ~ /[ \t]/)) break
			out = out c
		}
		if (q != "") unsure("a quoted string runs over several lines")
		return out
	}
	{
		line = clean($0)
		if (line ~ /<</) unsure("heredoc")
		gsub(/^[ \t]+|[ \t\r]+$/, "", line)
		if (line == "") next
		t = line
		gsub(/[{][A-Za-z$%][^{} \t]*[}]/, "", t)
		open = 0; close_ = 0
		if (t ~ /(^|[ \t])[{]$/) { open = 1; sub(/[ \t]*[{]$/, "", t) }
		else if (t == "}") close_ = 1
		if (!close_ && t ~ /[{}]/) { unsure("unusual braces on line " NR); next }
		if (close_) {
			depth--
			if (depth < 0) { unsure("unbalanced braces"); depth = 0; next }
			if (depth == 0 && start) { print "BLOCK\t" start "\t" NR "\t" addrs; start = 0 }
			next
		}
		if (depth == 0) {
			if (open) { start = NR; addrs = (t == "" ? "(global options)" : t); if (t ~ /^[(]/) addrs = "(snippet) " t; depth = 1 }
			else if (t ~ /^import[ \t]/) next
			else unsure("a directive outside any block (a Caddyfile with a single site and no braces)")
		} else if (open) depth++
	}
	END { if (depth != 0) unsure("unbalanced braces") }
	' "$1"
}

# caddy_find_proxy FILE FIRST LAST: prints "RP first last" for the one
# reverse_proxy directive directly inside the block, or NONE, MANY, MATCHER.
caddy_find_proxy() {
	awk -v s="$2" -v e="$3" '
	function clean(x,   out, i, c, q, n) {
		out = ""; q = ""; n = length(x)
		for (i = 1; i <= n; i++) {
			c = substr(x, i, 1)
			if (q != "") { if (c == "\\" && q == "\"") i++; else if (c == q) q = ""; continue }
			if (c == "\"" || c == "`") { q = c; continue }
			if (c == "#" && (i == 1 || substr(x, i - 1, 1) ~ /[ \t]/)) break
			out = out c
		}
		return out
	}
	NR <= s || NR >= e { next }
	{
		line = clean($0); gsub(/^[ \t]+|[ \t\r]+$/, "", line)
		if (line == "") next
		t = line; gsub(/[{][A-Za-z$%][^{} \t]*[}]/, "", t)
		open = 0; close_ = 0
		if (t ~ /(^|[ \t])[{]$/) { open = 1; sub(/[ \t]*[{]$/, "", t) } else if (t == "}") close_ = 1
		if (close_) { depth--; if (inrp && depth == 0) { rpend = NR; inrp = 0 } next }
		split(t, w, /[ \t]+/)
		if (w[1] == "reverse_proxy") {
			if (depth == 0) {
				n++; rpstart = NR; rpend = NR
				if (w[2] ~ /^[\/@*]/) matcher = 1
				if (open) inrp = 1
			} else many = 1
		}
		if (open) depth++
	}
	END {
		if (matcher) print "MATCHER"
		else if (many || n > 1) print "MANY"
		else if (n == 1) print "RP " rpstart " " rpend
		else print "NONE"
	}' "$1"
}

# caddy_directive: the reverse_proxy directive for LinuxAdmin.
caddy_directive() {
	if [ "$C_TLS" = http ]; then
		printf 'reverse_proxy %s:%s\n' "$UPHOST" "$C_PORT"
	else
		printf 'reverse_proxy https://%s:%s {\n\ttransport http {\n\t\ttls_insecure_skip_verify\n\t}\n}\n' "$UPHOST" "$C_PORT"
	fi
}

# caddy_plan: decides what to do with Caddy. Sets C_HOST, C_ORIGINS,
# C_PROXIES, CADDY_ACTION (repoint, append, manual or none), CADDY_DOMAIN.
caddy_plan() {
	CADDY_ACTION=manual CADDY_DOMAIN='' CADDY_ORIGIN='' CB_FIRST='' CB_LAST='' CADDY_WHY=''
	C_TLS=self-signed
	case $CADDY_KIND in
	native)
		UPHOST=127.0.0.1
		C_HOST=127.0.0.1
		C_TLS=http
		;;
	docker)
		if [ "$CD_HOSTNET" = 1 ]; then
			UPHOST=127.0.0.1
			C_HOST=127.0.0.1
			C_TLS=http
		elif [ -n "$CD_GW" ]; then
			# Inside the container 127.0.0.1 is the container itself: Caddy has to
			# reach the host through the Docker bridge, so LinuxAdmin listens on the
			# bridge's gateway address only (not on every interface, where other
			# machines could reach it). The bridge exists once docker.service has
			# started: a drop-in orders linuxadmin.service after it.
			C_HOST=$CD_GW
			DOCKER_DROPIN=1
			# Not host.docker.internal: host-gateway is docker0's address, which may
			# not be the gateway of Caddy's network that LinuxAdmin listens on.
			UPHOST=$CD_GW
			[ -n "$CD_SUBNET" ] && C_PROXIES="$(list_add "$C_PROXIES" "$CD_SUBNET")"
		else
			UPHOST=HOST_ADDRESS
			CADDY_WHY="$CD_NOTE"
		fi
		;;
	esac
	if [ "$CADDY_KIND" = native ] && ! have caddy; then
		CADDY_WHY="The caddy binary is not in PATH, so the Caddyfile cannot be validated."
	fi
	C_PROXIES="$(list_add "${C_PROXIES:-127.0.0.0/8}" 127.0.0.0/8)"
	C_PROXIES="$(list_add "$C_PROXIES" ::1/128)"

	cp_blocks="$TMPD/caddy.blocks"
	: >"$cp_blocks"
	if [ -n "$CADDY_CF" ]; then
		caddy_scan "$CADDY_CF" >"$cp_blocks"
		if grep -q '^UNSURE' "$cp_blocks"; then
			CADDY_WHY="This Caddyfile has something I do not parse safely ($(sed -n 's/^UNSURE.//p' "$cp_blocks" | head -n 1))."
			: >"$cp_blocks"
		fi
	elif [ -z "$CADDY_WHY" ]; then
		CADDY_WHY="The Caddyfile of the container is not a file on this machine (no bind mount for $CADDY_CF_IN)."
	fi

	cp_addr=
	cp_hit=
	if [ -n "$F_DOMAIN" ]; then
		cp_addr=$F_DOMAIN
		cp_hit="$(awk -F'\t' -v d="$F_DOMAIN" '$1 == "BLOCK" && ($4 == d || $4 == "https://" d || $4 == "http://" d) { print; exit }' "$cp_blocks")"
	else
		cp_hit="$(awk -F'\t' '$1 == "BLOCK" && tolower($4) ~ /cockpit/ && $4 !~ /[ ,]/ { print; exit }' "$cp_blocks")"
		if [ -n "$cp_hit" ]; then
			cp_addr="$(printf '%s\n' "$cp_hit" | cut -f4)"
			say "The Caddyfile has a site for Cockpit: $cp_addr (Cockpit listens on port 9090 by default)."
			if [ "$TTY_OK" = 1 ]; then
				ask "Point $cp_addr to LinuxAdmin (port $C_PORT) instead? [Y/n]" y || {
					cp_hit=
					cp_addr=
				}
			elif [ "$YES" = 0 ] && [ "$CADDY_MODE" != yes ]; then
				cp_hit=
				cp_addr=
			fi
		fi
	fi
	if [ -z "$cp_addr" ]; then
		cp_base="$(awk -F'\t' '$1 == "BLOCK" { a = $4; sub(/^https?:\/\//, "", a); sub(/[:,\/ ].*$/, "", a); n = split(a, p, "."); if (n >= 2 && a !~ /^[0-9.]+$/ && a !~ /[*]/) { print p[n-1] "." p[n]; exit } }' "$cp_blocks")"
		cp_def=
		[ -n "$cp_base" ] && cp_def="linuxadmin.$cp_base"
		if [ "$TTY_OK" = 1 ]; then
			while :; do
				ask_value "Domain for LinuxAdmin, such as linuxadmin.example.org (it must point to this Caddy)" "${cp_def:-linuxadmin.example.org}"
				valid_domain "$REPLY" && break
				say "'$REPLY' is not a domain name."
			done
			cp_addr=$REPLY
		else
			say "Caddy: no domain given. Use --domain NAME to set it up; the snippet is printed instead."
			CADDY_ACTION=manual
			CADDY_DOMAIN=linuxadmin.example.org
			CADDY_ORIGIN=https://$CADDY_DOMAIN
			C_ORIGINS="$(list_add "$C_ORIGINS" "$CADDY_ORIGIN")"
			return 0
		fi
		cp_hit="$(awk -F'\t' -v d="$cp_addr" '$1 == "BLOCK" && ($4 == d || $4 == "https://" d || $4 == "http://" d) { print; exit }' "$cp_blocks")"
		if [ -n "$cp_hit" ] && [ "$TTY_OK" = 1 ]; then
			ask "The Caddyfile already has a site $cp_addr. Point it to LinuxAdmin? [y/N]" n || cp_hit=skip
		fi
	fi
	case $cp_addr in
	http://*) CADDY_ORIGIN=$cp_addr CADDY_DOMAIN="${cp_addr#http://}" ;;
	https://*) CADDY_ORIGIN=$cp_addr CADDY_DOMAIN="${cp_addr#https://}" ;;
	*) CADDY_DOMAIN=$cp_addr CADDY_ORIGIN="https://$cp_addr" ;;
	esac
	CADDY_DOMAIN="${CADDY_DOMAIN%:443}"
	CADDY_ORIGIN="${CADDY_ORIGIN%:443}"
	C_ORIGINS="$(list_add "$C_ORIGINS" "$CADDY_ORIGIN")"
	[ -n "$CADDY_WHY" ] && return 0
	[ -n "$CADDY_CF" ] || return 0
	if [ "$cp_hit" = skip ]; then
		CADDY_WHY="Left as it is."
		return 0
	fi
	if [ -n "$cp_hit" ]; then
		CB_FIRST="$(printf '%s\n' "$cp_hit" | cut -f2)"
		CB_LAST="$(printf '%s\n' "$cp_hit" | cut -f3)"
		cp_rp="$(caddy_find_proxy "$CADDY_CF" "$CB_FIRST" "$CB_LAST")"
		case $cp_rp in
		RP*)
			CADDY_ACTION=repoint
			# shellcheck disable=SC2086
			set -- $cp_rp
			RP_FIRST=$2 RP_LAST=$3
			;;
		*) CADDY_WHY="The site block of $cp_addr has no single plain reverse_proxy line I can replace ($cp_rp)." ;;
		esac
	else
		CADDY_ACTION=append
	fi
	return 0
}

# caddy_err: the error line of the last failed check.
caddy_err() {
	grep -i 'error' "$TMPD/caddy.err" 2>/dev/null | tail -n 1 | cut -c1-240
}

# caddy_check: validates the Caddyfile (as Caddy sees it).
caddy_check() {
	case $CADDY_KIND in
	native)
		if have caddy; then
			caddy validate --config "$CADDY_CF" --adapter caddyfile >/dev/null 2>"$TMPD/caddy.err"
		else
			return 0
		fi
		;;
	docker) dk exec "$CADDY_CTR" caddy validate --config "$CADDY_CF_IN" --adapter caddyfile >/dev/null 2>"$TMPD/caddy.err" ;;
	esac
}

caddy_reload() {
	case $CADDY_KIND in
	native)
		if systemctl is-active --quiet caddy 2>/dev/null; then
			systemctl reload caddy
		else
			say "caddy.service is not running: it will use the new Caddyfile when you start it ('systemctl enable --now caddy')."
			return 1
		fi
		;;
	docker)
		if ! dk exec "$CADDY_CTR" caddy reload --config "$CADDY_CF_IN" --adapter caddyfile >"$TMPD/caddy.err" 2>&1; then
			say "caddy reload failed: $(caddy_err)"
			return 1
		fi
		;;
	esac
}

caddy_print_manual() {
	say "Add this to the Caddyfile${CADDY_CF:+ ($CADDY_CF)}, then reload Caddy:"
	say ""
	say "$CADDY_DOMAIN {"
	caddy_directive | sed "s/^/$TAB/"
	say "}"
	say ""
	case $CADDY_KIND in
	native) say "Reload: caddy validate --config ${CADDY_CF:-/etc/caddy/Caddyfile} --adapter caddyfile && systemctl reload caddy" ;;
	docker) say "Reload: docker exec $CADDY_CTR caddy reload --config $CADDY_CF_IN --adapter caddyfile" ;;
	esac
}

# caddy_apply: edits the Caddyfile (after the LinuxAdmin side is in place).
caddy_apply() {
	[ -n "$CADDY_KIND" ] && [ "$PROXY" = caddy ] || return 0
	step "Caddy"
	if [ "$CADDY_ACTION" = manual ]; then
		[ -n "$CADDY_WHY" ] && say "$CADDY_WHY Nothing was edited."
		caddy_print_manual
		return 0
	fi
	if [ "$DRY" = 1 ]; then
		say "  [dry-run] $CADDY_ACTION in $CADDY_CF for $CADDY_DOMAIN:"
		caddy_directive | sed "s/^/    | /"
		return 0
	fi
	if ! caddy_check; then
		say "The Caddyfile does not validate even before the edit ($(caddy_err)). Nothing was edited."
		caddy_print_manual
		return 0
	fi
	ca_bak="$CADDY_CF.linuxadmin-backup-$(date +%Y%m%d-%H%M%S)"
	cp -p "$CADDY_CF" "$ca_bak" || {
		say "Could not make a backup next to the Caddyfile. Nothing was edited."
		caddy_print_manual
		return 0
	}
	say "Backup: $ca_bak"
	caddy_directive >"$TMPD/caddy.directive"
	if [ "$CADDY_ACTION" = repoint ]; then
		awk -v s="$RP_FIRST" -v e="$RP_LAST" -v rf="$TMPD/caddy.directive" '
		NR == s { match($0, /^[ \t]*/); ind = substr($0, 1, RLENGTH); while ((getline l < rf) > 0) print ind l }
		NR >= s && NR <= e { next }
		{ print }' "$ca_bak" >"$TMPD/caddy.new"
	else
		{
			cat "$ca_bak"
			[ -n "$(tail -c 1 "$ca_bak")" ] && printf '\n'
			printf '\n# LinuxAdmin (added by install.sh)\n%s {\n' "$CADDY_DOMAIN"
			sed "s/^/$TAB/" "$TMPD/caddy.directive"
			printf '}\n'
		} >"$TMPD/caddy.new"
	fi
	# In place, not rename: a single file mounted into a container must keep its inode.
	cat "$TMPD/caddy.new" >"$CADDY_CF"
	if ! caddy_check; then
		cat "$ca_bak" >"$CADDY_CF"
		say "Caddy rejected the edited Caddyfile ($(caddy_err)); the original is back in place."
		caddy_print_manual
		return 0
	fi
	if caddy_reload; then
		say "Caddy reloaded: https://$CADDY_DOMAIN now goes to LinuxAdmin."
		CADDY_DONE=1
	else
		say "The Caddyfile is edited and valid, but Caddy was not reloaded. https://$CADDY_DOMAIN works once Caddy runs with it."
	fi
	if [ "$CADDY_KIND" = docker ] && [ -n "$CD_SUBNET" ]; then
		say "Caddy reaches LinuxAdmin at $UPHOST:$C_PORT from the Docker network $CD_SUBNET. If a firewall blocks that"
		say "(ufw does by default), allow it: ufw allow from $CD_SUBNET to any port $C_PORT proto tcp"
	fi
	say "The domain $CADDY_DOMAIN must point to this machine for Caddy to get its certificate."
}

# ---------------------------------------------------------------- questions about the configuration

# decide_config: sets CFG_MODE (new, keep or change) and the C_* choices.
decide_config() {
	load_current
	CFG_MODE=new
	PROXY='' CADDY_KIND='' CADDY_ACTION=none
	C_PORT=$D_PORT C_HOST=$D_HOST C_ROOT=$D_ROOT C_UNLOCK=$D_UNLOCK C_TLS=$D_TLS C_CERT=$D_CERT C_KEY=$D_KEY
	C_ORIGINS=$D_ORIGINS C_PROXIES=$D_PROXIES C_AUSERS=$D_AUSERS C_AGROUPS=$D_AGROUPS C_ADMINS=$D_ADMINS
	ENABLE=1 START=1
	[ "$NO_ENABLE" = 1 ] && ENABLE=0
	[ "$NO_START" = 1 ] && START=0
	if [ "$CFG_EXISTS" = 1 ]; then
		CFG_MODE=keep
		if [ "$CFG_REQUESTED" = 1 ] || [ "$RECONF" = 1 ]; then
			CFG_MODE=change
		elif [ "$TTY_OK" = 1 ]; then
			say "LinuxAdmin is configured already ($CONF_FILE, listening on $D_HOST:$D_PORT); the configuration is kept."
			ask "Change settings now? [y/N]" n && CFG_MODE=change
		fi
		if [ "$CFG_MODE" = keep ]; then
			C_LISTEN="$D_HOST:$D_PORT"
			return 0
		fi
	fi

	step "Configuration"
	if [ "$TTY_OK" = 1 ]; then
		say "Press Enter to take the value in [brackets]."
	else
		say "Nothing is asked (no terminal, or --yes): defaults, plus the options given."
	fi

	choose_port

	# Reverse proxy
	caddy_detect
	if [ "$CADDY_MODE" = yes ] && [ -z "$CADDY_KIND" ]; then
		die "--caddy: no Caddy found (no /etc/caddy/Caddyfile with the caddy binary or caddy.service, and no running container whose image name contains 'caddy')."
	fi
	if [ -n "$CADDY_KIND" ]; then
		case $CADDY_KIND in
		native) cd_what="Caddy on this machine (Caddyfile: $CADDY_CF)" ;;
		docker) cd_what="Caddy in the Docker container '$CADDY_CTR' (Caddyfile: ${CADDY_CF:-not a file on this machine})" ;;
		esac
		say "Found $cd_what."
		if [ "$CADDY_MODE" = yes ]; then
			PROXY=caddy
		elif [ "$TTY_OK" = 1 ]; then
			ask "Put LinuxAdmin behind it, with its own (sub)domain? [Y/n]" y && PROXY=caddy
		else
			say "Caddy is not changed unless you pass --caddy."
		fi
	fi
	if [ "$PROXY" = caddy ]; then
		caddy_plan
	elif [ "$F_PROXY" = 1 ] || { [ "$TTY_OK" = 1 ] && [ -z "$F_LISTEN" ] && ask "Will LinuxAdmin sit behind another reverse proxy (nginx, Apache, Traefik...)? [y/N]" n; }; then
		PROXY=other
		C_TLS=http
		C_HOST=127.0.0.1
		if [ "$TTY_OK" = 1 ] && ! ask "Does the proxy run on this machine? [Y/n]" y; then
			C_HOST=0.0.0.0
			C_TLS=self-signed
			ask_value "Address of the proxy (for trusted_proxies, e.g. 10.0.0.5 or 10.0.0.0/24)" "10.0.0.1"
			valid_proxy_addr "$REPLY" && C_PROXIES="$(list_add "${C_PROXIES:-127.0.0.0/8 ::1/128}" "$REPLY")"
		fi
		if [ "$TTY_OK" = 1 ] && [ -z "$F_ORIGINS" ]; then
			while :; do
				ask_value "Public address of LinuxAdmin, as typed in the browser" "https://linuxadmin.example.org"
				valid_origin "$REPLY" && break
				say "'$REPLY' must look like https://host or https://host:port."
			done
			C_ORIGINS="$(list_add "$C_ORIGINS" "$REPLY")"
		fi
	fi
	# Flags add to what was decided.
	for co_o in $F_ORIGINS; do C_ORIGINS="$(list_add "$C_ORIGINS" "$co_o")"; done
	for co_o in $F_TRUSTED; do C_PROXIES="$(list_add "${C_PROXIES:-127.0.0.0/8 ::1/128}" "$co_o")"; done

	# Listen address and TLS (a proxy decides both)
	if [ -z "$PROXY" ]; then
		case $F_LISTEN in
		all) C_HOST=0.0.0.0 ;;
		local) C_HOST=127.0.0.1 ;;
		'')
			if [ "$TTY_OK" = 1 ]; then
				if [ "$C_HOST" = 127.0.0.1 ]; then cl_def=2; else cl_def=1; fi
				ask_choice "Who can reach LinuxAdmin?" "$cl_def" "every machine that can reach this one (all interfaces, 0.0.0.0)" "only this machine (127.0.0.1: for an SSH tunnel or a proxy)"
				if [ "$CHOICE" = 1 ]; then C_HOST=0.0.0.0; else C_HOST=127.0.0.1; fi
			fi
			;;
		*) C_HOST=$F_LISTEN ;;
		esac
		if [ -n "$F_CERT" ]; then
			C_TLS=custom C_CERT=$F_CERT C_KEY=$F_KEY
		elif [ "$TTY_OK" = 1 ]; then
			if [ "$C_TLS" = custom ]; then ct_def=2; else ct_def=1; fi
			ask_choice "TLS certificate:" "$ct_def" "self-signed, created on first start (the browser warns once; the installer shows the fingerprint)" "my own certificate and key files"
			if [ "$CHOICE" = 2 ]; then
				C_TLS=custom
				while :; do
					ask_value "Certificate file (PEM, full chain)" "${C_CERT:-/etc/ssl/certs/linuxadmin.pem}"
					C_CERT=$REPLY
					ask_value "Private key file" "${C_KEY:-/etc/ssl/private/linuxadmin.key}"
					C_KEY=$REPLY
					valid_path "$C_CERT" && valid_path "$C_KEY" && [ -r "$C_CERT" ] && [ -r "$C_KEY" ] && break
					say "Both files must exist and have plain absolute paths."
				done
			else
				C_TLS=self-signed
			fi
		fi
	fi
	if [ "$C_TLS" = custom ]; then
		if ! valid_path "$C_CERT" || ! valid_path "$C_KEY"; then die "--tls-cert and --tls-key need plain absolute paths."; fi
		{ [ -r "$C_CERT" ] && [ -r "$C_KEY" ]; } || [ "$DRY" = 1 ] || die "Cannot read $C_CERT or $C_KEY."
	fi
	case $C_TLS in
	http)
		case $C_HOST in
		127.* | '[::1]') ;;
		*)
			say "Plain HTTP is only possible on 127.0.0.1; with this listen address LinuxAdmin keeps HTTPS (self-signed)."
			C_TLS=self-signed
			;;
		esac
		;;
	esac
	C_LISTEN="$C_HOST:$C_PORT"

	# Sign-in
	if [ "$TTY_OK" = 1 ]; then
		say ""
		say "Sign-in: any local account with a real login shell can sign in (nologin and restricted"
		say "shells are refused). Administrator rights come from sudo, with the user's own password."
	fi
	if [ -n "$F_ROOT" ]; then
		C_ROOT=$F_ROOT
	elif [ "$TTY_OK" = 1 ]; then
		if [ "$C_ROOT" = true ]; then ar_def=y; else ar_def=n; fi
		if ask "Allow signing in as root? [$(if [ $ar_def = y ]; then echo Y/n; else echo y/N; fi)]" "$ar_def"; then C_ROOT=true; else C_ROOT=false; fi
	fi
	if [ -n "$F_AUSERS$F_AGROUPS" ] || [ "$F_ADMINS" = 1 ]; then
		# Options replace what the file says.
		C_AUSERS=$F_AUSERS C_AGROUPS=$F_AGROUPS C_ADMINS=false
		[ "$F_ADMINS" = 1 ] && C_ADMINS=true
	elif [ "$TTY_OK" = 1 ]; then
		if [ -n "$C_AUSERS$C_AGROUPS" ]; then wa_def=3; elif [ "$C_ADMINS" = true ]; then wa_def=2; else wa_def=1; fi
		ask_choice "Who may sign in?" "$wa_def" "every local account" "only administrators (members of sudo, wheel or admin$(if [ "$C_ROOT" = true ]; then echo ', and root'; fi))" "only these users or groups, which you name next"
		case $CHOICE in
		1) C_AUSERS='' C_AGROUPS='' C_ADMINS=false ;;
		2) C_AUSERS='' C_AGROUPS='' C_ADMINS=true ;;
		3)
			C_ADMINS=false
			wa_u="${C_AUSERS:-${SUDO_USER:-}}"
			[ "$wa_u" = root ] && wa_u=
			wa_g=$C_AGROUPS
			while :; do
				ask_value "Users who may sign in (names separated by spaces or commas, - for none)" "${wa_u:--}"
				wa_ru=$REPLY
				ask_value "Groups whose members may sign in (names separated by spaces or commas, - for none)" "${wa_g:--}"
				wa_rg=$REPLY
				[ "$wa_ru" = - ] && wa_ru=
				[ "$wa_rg" = - ] && wa_rg=
				if ! parse_names "$wa_ru"; then
					say "'$PN_BAD' is not a valid user name."
					continue
				fi
				C_AUSERS=$PN_OUT
				if ! parse_names "$wa_rg"; then
					say "'$PN_BAD' is not a valid group name."
					continue
				fi
				C_AGROUPS=$PN_OUT
				[ -n "$C_AUSERS$C_AGROUPS" ] && break
				say "Name at least one user or group (or go back and choose another answer with Ctrl-C)."
			done
			;;
		esac
	fi
	if [ -n "$F_UNLOCK" ]; then
		C_UNLOCK=$F_UNLOCK
	elif [ "$TTY_OK" = 1 ]; then
		cu_keep=
		case $C_UNLOCK in 5m | 15m | 1h | 0s) ;; *) cu_keep=$C_UNLOCK ;; esac
		case $C_UNLOCK in 15m) cu_def=2 ;; 1h) cu_def=3 ;; 0s) cu_def=4 ;; 5m) cu_def=1 ;; *) cu_def=5 ;; esac
		if [ -n "$cu_keep" ]; then
			ask_choice "How long do administrator rights stay unlocked after the last use?" "$cu_def" "5 minutes" "15 minutes" "1 hour" "until sign-out" "keep $cu_keep"
		else
			ask_choice "How long do administrator rights stay unlocked after the last use?" "$cu_def" "5 minutes" "15 minutes" "1 hour" "until sign-out"
		fi
		case $CHOICE in 1) C_UNLOCK=5m ;; 2) C_UNLOCK=15m ;; 3) C_UNLOCK=1h ;; 4) C_UNLOCK=0s ;; 5) C_UNLOCK=$cu_keep ;; esac
	fi

	if [ "$TTY_OK" = 1 ]; then
		[ "$NO_ENABLE" = 1 ] || { ask "Start LinuxAdmin at boot? [Y/n]" y || ENABLE=0; }
		[ "$NO_START" = 1 ] || { ask "Start it now? [Y/n]" y || START=0; }
	fi

	# Summary
	step "Summary"
	say "  Listen:            $C_HOST:$C_PORT$(if [ "$C_HOST" = 0.0.0.0 ]; then echo ' (all interfaces)'; elif [ "$C_HOST" = 127.0.0.1 ]; then echo ' (this machine only)'; fi)"
	say "  Sign in as root:   $(if [ "$C_ROOT" = true ]; then echo yes; else echo no; fi)"
	say "  Who may sign in:   $(auth_text)"
	auth_warnings
	say "  Admin unlock:      $(if [ "$C_UNLOCK" = 0s ]; then echo 'until sign-out'; else echo "$C_UNLOCK of inactivity"; fi)"
	say "  TLS:               $C_TLS$(if [ "$C_TLS" = custom ]; then echo " ($C_CERT)"; elif [ "$C_TLS" = http ]; then echo ' (plain HTTP on this machine; the proxy adds HTTPS)'; fi)"
	if [ "$PROXY" = caddy ]; then
		if [ "$C_TLS" = http ]; then
			say "  Caddy:             $CADDY_ACTION $CADDY_DOMAIN -> http://$UPHOST:$C_PORT (plain HTTP, never leaves this machine)"
		else
			say "  Caddy:             $CADDY_ACTION $CADDY_DOMAIN -> https://$UPHOST:$C_PORT (certificate not checked: it is this machine)"
		fi
		case $CADDY_ACTION in manual) say "                     (the Caddyfile is not edited: ${CADDY_WHY:-the snippet is printed})" ;; *) say "                     ($CADDY_CF is backed up, validated, rolled back on errors, then Caddy is reloaded)" ;; esac
		[ "${DOCKER_DROPIN:-0}" = 1 ] && say "                     (Caddy is in Docker: LinuxAdmin listens on the Docker bridge $CD_GW and starts after docker.service)"
	elif [ "$PROXY" = other ]; then
		if [ "$C_TLS" = http ]; then
			say "  Reverse proxy:     yes; point it at http://127.0.0.1:$C_PORT (plain HTTP) and let it send X-Forwarded-Proto and X-Forwarded-Host"
		else
			say "  Reverse proxy:     yes; point it at https://$C_HOST:$C_PORT and let it skip certificate checks (self-signed)"
		fi
	fi
	[ -n "$C_ORIGINS" ] && say "  Allowed origins:   $C_ORIGINS"
	[ -n "$PROXY" ] && say "  Trusted proxies:   ${C_PROXIES:-127.0.0.0/8 ::1/128}"
	say "  At boot / now:     $(if [ "$ENABLE" = 1 ]; then echo yes; else echo no; fi) / $(if [ "$START" = 1 ]; then echo yes; else echo no; fi)"
	if [ "$TTY_OK" = 1 ] && ! ask "Apply these settings? [Y/n]" y; then
		say "Nothing changed."
		exit 0
	fi
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
		if [ "$VERIFIER" = python ]; then
			say "Signature of SHA256SUMS: good (LinuxAdmin release key; checked with the built-in Ed25519 verifier on $VBIN, because this openssl is older than 3)."
		else
			say "Signature of SHA256SUMS: good (LinuxAdmin release key, checked with $VBIN)."
		fi
	else
		warn "No tool here can verify ed25519 signatures (OpenSSL 3, python3 or python2 is needed): SHA256SUMS is NOT verified. Continuing because of --insecure-skip-signature."
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

# docker_dropin writes or removes the drop-in that starts LinuxAdmin after
# Docker, needed when it listens on a Docker bridge address.
docker_dropin() {
	dd_f="$UNIT_FILE.d/docker-bridge.conf"
	if [ "${DOCKER_DROPIN:-0}" = 1 ]; then
		printf '%s\n' '# Written by install.sh: LinuxAdmin listens on a Docker bridge address,' \
			'# which exists only once Docker has started.' \
			'[Unit]' 'After=docker.service' 'Wants=docker.service' >"$TMPD/dropin"
		say "Writing $dd_f."
		run install -D -m 644 "$TMPD/dropin" "$dd_f"
	elif [ -f "$dd_f" ] && [ "$CFG_MODE" != keep ]; then
		say "Removing $dd_f (no longer listening on a Docker bridge)."
		run rm -f "$dd_f"
		run rmdir "$UNIT_FILE.d" 2>/dev/null || true
	fi
}

start_service() {
	step "Starting $UNIT"
	docker_dropin
	run systemctl daemon-reload
	if [ "$ENABLE" = 1 ]; then
		run systemctl enable --quiet "$UNIT"
	else
		say "Not enabled at boot: systemctl enable $UNIT"
	fi
	if [ "$START" = 0 ]; then
		say "Not started: systemctl start $UNIT (restart it if it was running, to use the new files)."
		return 0
	fi
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
	pa_s=https
	[ "$(tls_mode)" = http ] && pa_s=http
	step "Open LinuxAdmin"
	[ "$START" = 0 ] && say "(after you start the service)"
	if [ "$CADDY_DONE" = 1 ]; then
		say "  https://$CADDY_DOMAIN"
		[ "$LISTEN_HOST" = 127.0.0.1 ] && LISTEN_HOST=local
	fi
	case $LISTEN_HOST in
	local) ;;
	'' | 0.0.0.0 | '[::]' | '::')
		say "  $pa_s://$(uname -n):$PORT"
		if have ip; then
			ip -o addr show scope global 2>/dev/null | awk '
				$2 ~ /^(docker|br-|veth|virbr|cni|flannel|cali|podman|lxc|tun|wg)/ { next }
				{ split($4, a, "/"); if ($3 == "inet") print a[1]; else if ($3 == "inet6") print "[" a[1] "]" }' |
				while read -r pa_ip; do say "  $pa_s://$pa_ip:$PORT"; done
		fi
		;;
	*) say "  $pa_s://$LISTEN_HOST:$PORT" ;;
	esac
	say "Sign in with a Linux account."
	[ "$DRY" = 1 ] && return 0
	if [ "$(tls_mode)" = http ]; then
		say "LinuxAdmin serves plain HTTP on $LISTEN_HOST for the reverse proxy; the proxy provides HTTPS."
		return 0
	fi
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

	decide_config

	if [ -z "$FROM" ]; then
		verifier_selftest
		if [ "$CAN_VERIFY" = 0 ] && ! have openssl && [ "$DRY" = 0 ]; then
			say "openssl is needed to verify the release signature. Installing it."
			pkg_install openssl || true
			verifier_selftest
		fi
		if [ "$CAN_VERIFY" = 0 ] && [ "$SKIP_SIG" = 0 ]; then
			if [ "$DRY" = 1 ] && ! have openssl && ! have python3 && ! have python2 && ! have python; then
				warn "openssl is missing; a real run installs it first."
			else
				die "Cannot check the release signature: this system's openssl is older than 3 (it cannot verify ed25519) and neither python3 nor python2 is installed. Install python3 (or OpenSSL 3), or pass --insecure-skip-signature to install with the sha256 check only."
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
	write_config
	check_config
	install_unit "$SRC"
	start_service
	report_admin_group
	firewall
	caddy_apply
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
	run rm -f "$UNIT_FILE" "$UNIT_FILE.d/docker-bridge.conf"
	run rmdir "$UNIT_FILE.d" 2>/dev/null || true
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
	F_PORT='' F_LISTEN='' F_ROOT='' F_UNLOCK='' F_CERT='' F_KEY='' F_PROXY=0 F_ORIGINS='' F_TRUSTED='' F_DOMAIN='' F_AUSERS='' F_AGROUPS='' F_ADMINS=0 WC_BAK=''
	CADDY_MODE=auto NO_ENABLE=0 NO_START=0 RECONF=0 CFG_REQUESTED=0
	TTY_OK=0 C_LISTEN='' C_TLS='' PROXY='' CADDY_DOMAIN='' CADDY_DONE=0 ENABLE=1 START=1
	CADDY_KIND='' CADDY_ACTION=none CFG_MODE=keep
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
		--port | --listen | --admin-unlock | --tls-cert | --tls-key | --origin | --trusted-proxy | --domain | --allow-users | --allow-groups)
			[ $# -ge 2 ] || die "$1 needs a value."
			opt_set "$1" "$2"
			shift
			;;
		--port=* | --listen=* | --admin-unlock=* | --tls-cert=* | --tls-key=* | --origin=* | --trusted-proxy=* | --domain=* | --allow-users=* | --allow-groups=*)
			opt_set "${1%%=*}" "${1#*=}"
			;;
		--admins-only) F_ADMINS=1 CFG_REQUESTED=1 ;;
		--allow-root) F_ROOT=true CFG_REQUESTED=1 ;;
		--no-allow-root) F_ROOT=false CFG_REQUESTED=1 ;;
		--behind-proxy) F_PROXY=1 CFG_REQUESTED=1 ;;
		--caddy) CADDY_MODE=yes CFG_REQUESTED=1 ;;
		--no-caddy) CADDY_MODE=no ;;
		--reconfigure) RECONF=1 ;;
		--no-enable) NO_ENABLE=1 ;;
		--no-start) NO_START=1 ;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "Unknown option '$1' (see --help)." ;;
		esac
		shift
	done
	[ "$PURGE" = 1 ] && [ "$UNINSTALL" = 0 ] && die "--purge only goes with --uninstall."
	if [ -n "$F_CERT" ] || [ -n "$F_KEY" ]; then
		if [ -z "$F_CERT" ] || [ -z "$F_KEY" ]; then
			die "--tls-cert and --tls-key go together."
		fi
	fi
	[ "$CADDY_MODE" = yes ] && [ "$F_PROXY" = 1 ] && die "--caddy and --behind-proxy exclude each other (--caddy already sets up the proxy case)."
	[ -n "$F_DOMAIN" ] && [ "$CADDY_MODE" = no ] && die "--domain only goes with Caddy."
	[ -n "$F_DOMAIN" ] && [ "$CADDY_MODE" = auto ] && CADDY_MODE=yes
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
	tty_check
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
# stdin is closed for main: the script may be arriving on it (curl | sh).
main "$@" </dev/null
