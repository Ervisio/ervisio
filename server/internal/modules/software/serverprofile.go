package software

import (
	"io"
	"os"
	"strings"

	"github.com/ervisio/ervisio/server/internal/sys"
)

func detectedServerProfile() bool {
	return serverProfile(sys.ReadOSRelease(), func(path string) []byte {
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		data, _ := io.ReadAll(io.LimitReader(f, 64<<10))
		return data
	})
}

// Positive edition/package/image markers only. An Ubuntu ID, missing display,
// SSH session, or a hostname is not sufficient to identify Ubuntu Server.
func serverProfile(release sys.OSRelease, read func(string) []byte) bool {
	if release.VariantID == "server" {
		return true
	}
	if release.ID == "windows" {
		return strings.Contains(strings.ToLower(release.Name), "server")
	}
	if release.ID != "ubuntu" {
		return false
	}
	for _, name := range []string{"ubuntu-server", "ubuntu-server-minimal"} {
		if len(read("/var/lib/dpkg/info/"+name+".list")) > 0 {
			return true
		}
	}
	for _, line := range strings.Split(string(read("/etc/cloud/build.info")), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(key) == "build_name" && strings.Trim(strings.TrimSpace(value), "\"'") == "server" {
			return true
		}
	}
	return false
}

// Positive CLI compatibility, independent of whether a package is installed.
// Unknown packages stay unclassified instead of assuming every repo package
// can work without a graphical session.
var cliPackages = strings.Fields("git curl wget rsync openssh-client openssh-server openssh sudo tmux screen htop btop atop ncdu tree jq yq ripgrep fd-find fzf bat vim vim-tiny neovim nano emacs-nox less moreutils unzip zip tar gzip zstd xz-utils p7zip-full ffmpeg imagemagick nginx nginx-core nginx-full apache2 httpd caddy certbot haproxy traefik docker docker.io docker-ce docker-compose docker-compose-v2 docker-buildx-plugin containerd podman buildah skopeo kubectl helm k9s ansible ansible-core terraform opentofu consul vault nomad postgresql postgresql-client mysql-client mysql-server mariadb-client mariadb-server sqlite sqlite3 redis redis-server redis-tools memcached restic borgbackup rclone duplicity python python3 python3-pip pipx uv nodejs npm yarn pnpm golang go rustup cargo rustc cmake make gcc g++ gdb strace lsof tcpdump nmap iperf3 dnsutils bind-utils net-tools iproute2 socat fail2ban ufw nftables iptables smartmontools sysstat iotop iftop duf dust fastfetch neofetch")

func classifyCLI(items []Result) []Result {
	out := append([]Result{}, items...)
	for i := range out {
		if out[i].Kind == KindFlatpak {
			continue
		}
		for _, name := range cliPackages {
			if out[i].Name == name {
				out[i].CLI = true
				break
			}
		}
	}
	return out
}
