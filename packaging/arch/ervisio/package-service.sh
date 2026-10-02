#!/bin/sh
# Installed by the distribution packages as /usr/lib/ervisio/package-service
# and run by their install scripts:
#
#   package-service restart   after an upgrade: restart ervisio.service if it runs
#   package-service start     after replacing LinuxAdmin: start ervisio.service
#                             (which stops linuxadmin.service: same port)
#   package-service stop      before removal: stop it
#
# The package manager may have been started from Ervisio (or LinuxAdmin)
# itself (its terminal, or the Software section): then it is a descendant of
# ervisiod or linuxadmind, and restarting, starting or stopping the service
# now would end it halfway through.
# In that case the action is handed to a transient systemd unit that waits
# until that package manager has exited (at most one hour), then does it.
set -u
unit=ervisio.service
action="${1:-}"
case $action in
restart | start | stop) ;;
*)
	echo "usage: $0 restart|start|stop" >&2
	exit 2
	;;
esac
[ -d /run/systemd/system ] || exit 0

# Walk up the process tree: is Ervisio or LinuxAdmin an ancestor, and which package
# manager process is the outermost one?
under=0 pm_pid='' pm_comm=''
p=$$
while [ "$p" -gt 1 ] 2>/dev/null; do
	comm="$(cat "/proc/$p/comm" 2>/dev/null)" || break
	case $comm in
	ervisiod | ervisio-bridge | linuxadmind | linuxadmin-brid*) under=1 ;;
	apt | apt-get | aptitude | dpkg | unattended-upgr | dnf | dnf5 | yum | microdnf | rpm | zypper | pacman | yay | paru | packagekitd)
		pm_pid=$p pm_comm=$comm
		;;
	esac
	p="$(sed 's/^.*) //' "/proc/$p/stat" 2>/dev/null | cut -d' ' -f2)"
done

if [ "$under" = 0 ]; then
	case $action in
	restart)
		if command -v deb-systemd-invoke >/dev/null 2>&1; then
			# Honours policy-rc.d (chroots, image builds).
			if systemctl is-active --quiet "$unit"; then
				deb-systemd-invoke restart "$unit" >/dev/null 2>&1 || true
			fi
		else
			systemctl try-restart "$unit" >/dev/null 2>&1 || true
		fi
		;;
	start)
		if command -v deb-systemd-invoke >/dev/null 2>&1; then
			deb-systemd-invoke start "$unit" >/dev/null 2>&1 || true
		else
			systemctl start "$unit" >/dev/null 2>&1 || true
		fi
		;;
	stop) systemctl stop "$unit" >/dev/null 2>&1 || true ;;
	esac
	exit 0
fi

verb=try-restart
case $action in
start) verb=start ;;
stop) verb=stop ;;
esac
[ "$verb" = start ] || systemctl is-active --quiet "$unit" || exit 0
if [ -z "$pm_pid" ]; then
	echo "Ervisio: run 'systemctl $verb $unit' when the package transaction has finished."
	exit 0
fi
# The waiting script is passed inline: on removal this file is gone by then.
# shellcheck disable=SC2016
wait_script='
pid=$1 comm=$2 verb=$3 unit=$4 n=0
while [ "$(cat "/proc/$pid/comm" 2>/dev/null)" = "$comm" ] && [ $n -lt 1800 ]; do
	sleep 2
	n=$((n + 1))
done
exec systemctl "$verb" "$unit"'
if systemd-run --quiet --collect --no-block --unit="ervisio-package-$action-$$" \
	--description="Ervisio: $action after the package transaction" \
	/bin/sh -c "$wait_script" sh "$pm_pid" "$pm_comm" "$verb" "$unit" 2>/dev/null; then
	echo "Ervisio: the service will $action when $pm_comm has finished (sign in again afterwards)."
else
	echo "Ervisio: run 'systemctl $verb $unit' when the package transaction has finished."
fi
exit 0
