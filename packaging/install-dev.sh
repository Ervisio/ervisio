#!/bin/bash
# Installs a locally built LinuxAdmin for testing on this machine.
# Build first as your user:  make build
# Then:                      sudo ./packaging/install-dev.sh          (install or update)
#                            sudo ./packaging/install-dev.sh --remove (uninstall, keeps /etc/linuxadmin)
set -euo pipefail
cd "$(dirname "$0")/.."

[ "$(id -u)" -eq 0 ] || { echo "Run with sudo."; exit 1; }

if [ "${1:-}" = "--remove" ]; then
  systemctl disable --now linuxadmin.service 2>/dev/null || true
  rm -f /usr/bin/linuxadmind /etc/systemd/system/linuxadmin.service /etc/pam.d/linuxadmin
  rm -rf /usr/lib/linuxadmin /usr/share/linuxadmin
  systemctl daemon-reload
  echo "Removed. Configuration kept in /etc/linuxadmin."
  exit 0
fi

for f in server/bin/linuxadmind server/bin/linuxadmin-bridge web/dist/index.html; do
  [ -e "$f" ] || { echo "Missing $f: run 'make build' as your user first."; exit 1; }
done

install -Dm755 server/bin/linuxadmind        /usr/bin/linuxadmind
install -Dm755 server/bin/linuxadmin-bridge  /usr/lib/linuxadmin/linuxadmin-bridge
rm -rf /usr/share/linuxadmin/web
install -d -m755 /usr/share/linuxadmin
cp -r web/dist /usr/share/linuxadmin/web
install -d -m755 /usr/share/linuxadmin/plugins
rm -rf /usr/share/linuxadmin/plugins/docker
cp -r plugins/docker /usr/share/linuxadmin/plugins/docker
chown -R root:root /usr/share/linuxadmin /usr/lib/linuxadmin
chmod -R go-w /usr/share/linuxadmin
install -Dm644 packaging/pam.d/linuxadmin    /etc/pam.d/linuxadmin
install -Dm644 packaging/linuxadmin.service  /etc/systemd/system/linuxadmin.service
install -d -m755 /etc/linuxadmin

systemctl daemon-reload
systemctl enable linuxadmin.service >/dev/null 2>&1
systemctl restart linuxadmin.service
sleep 1
systemctl --no-pager --lines=5 status linuxadmin.service || true
echo
echo "Open https://localhost:9090 (self-signed certificate: accept the browser warning)."
