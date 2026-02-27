#!/usr/bin/env bash
#
# Sets up a clean Ubuntu install for compiling and running the cryptosim benchmark.
#
# Usage: Run as root or with sudo
#   sudo ./setup-ubuntu.sh
#
# Installs:
#   - build-essential, git (for building)
#   - Go (from official tarball)
#   - nano, tree, htop, iotop, tmux, sl (dev/admin tools)
#   - Docker (optional, for Prometheus/Grafana metrics)
#
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
	echo "Error: This script must be run as root (use sudo)" >&2
	exit 1
fi

# Detect architecture for Go tarball
ARCH=$(uname -m)
case "$ARCH" in
	x86_64)  GO_ARCH="amd64" ;;
	aarch64|arm64) GO_ARCH="arm64" ;;
	*) echo "Error: Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

GO_VERSION="1.26.0"
GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
GO_URL="https://go.dev/dl/${GO_TAR}"

echo "=== Updating package lists ==="
apt-get update

echo ""
echo "=== Installing build dependencies (build-essential, git) ==="
apt-get install -y build-essential git

echo ""
echo "=== Installing dev/admin tools (nano, tree, htop, iotop, tmux, sl) ==="
apt-get install -y nano tree htop iotop tmux sl

echo ""
echo "=== Installing Go ${GO_VERSION} ==="
if command -v go &>/dev/null; then
	INSTALLED=$(go version 2>/dev/null || true)
	echo "Go already installed: $INSTALLED"
	echo "Skipping Go installation. Remove existing Go and re-run if you need a different version."
else
	TMPDIR=$(mktemp -d)
	trap "rm -rf $TMPDIR" EXIT
	cd "$TMPDIR"
	wget -q --show-progress "$GO_URL" -O "$GO_TAR"
	rm -rf /usr/local/go
	tar -C /usr/local -xzf "$GO_TAR"
	cd - >/dev/null

	# Add Go to PATH for all users
	if ! grep -q '/usr/local/go/bin' /etc/profile.d/go.sh 2>/dev/null; then
		echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
		chmod 644 /etc/profile.d/go.sh
	fi
	export PATH=$PATH:/usr/local/go/bin
	echo "Go ${GO_VERSION} installed to /usr/local/go"
fi

echo ""
echo "=== Installing Docker (for Prometheus/Grafana metrics; optional) ==="
if command -v docker &>/dev/null; then
	echo "Docker already installed: $(docker --version)"
else
	apt-get install -y ca-certificates curl
	install -m 0755 -d /etc/apt/keyrings
	curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
	chmod a+r /etc/apt/keyrings/docker.asc
	echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
		> /etc/apt/sources.list.d/docker.list
	apt-get update
	apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
	echo "Docker installed. Add users to the 'docker' group for non-root access: usermod -aG docker <username>"
fi

echo ""
echo "=== Setup complete ==="
echo ""
echo "To use Go in the current shell, run:"
echo "  export PATH=\$PATH:/usr/local/go/bin"
echo "  (or log out and back in, or start a new tmux session)"
echo ""
echo "To run the cryptosim benchmark:"
echo "  1. Clone the sei-chain repo (or copy it to the machine)"
echo "  2. cd <sei-chain>/sei-db/state_db/bench/cryptosim"
echo "  3. ./run.sh ./config/basic-config.json"
echo ""
echo "For Prometheus/Grafana metrics:"
echo "  ./metrics/start-prometheus.sh"
echo "  ./metrics/start-grafana.sh"
echo ""
