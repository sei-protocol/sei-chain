#!/usr/bin/env bash
#
# Installs the two exporters the monitoring stack scrapes on a validator host:
# sei-cosmos-exporter on 9300 and node-exporter on 9100.
#
# Usage: ./install-exporters.sh
#
# Requirements: git, wget, tar and sudo. Linux only — node-exporter is fetched
# as a Linux binary and installed as a systemd unit.

set -euo pipefail

NODE_EXPORTER_VERSION=1.3.1

# node-exporter publishes one binary per architecture, and the wrong one installs
# cleanly and then fails to execute, leaving 9100 silently down.
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "unsupported architecture: $(uname -m)" >&2
	exit 1
	;;
esac

# Downloads unpack here rather than in the working directory, so nothing is left
# behind and the cleanup glob cannot reach files it did not create.
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

echo -e "\e[1m\e[32m1. Installing sei-cosmos-exporter... \e[0m" && sleep 1

# The installer ships in the source tree rather than as a release asset, so this
# clones. Re-running the script finds the checkout already there.
if [ -d "$workdir/sei-cosmos-exporter" ]; then
	git -C "$workdir/sei-cosmos-exporter" fetch --depth 1 origin
else
	git clone --depth 1 https://github.com/sei-protocol/sei-cosmos-exporter.git \
		"$workdir/sei-cosmos-exporter"
fi

installer="$workdir/sei-cosmos-exporter/install-sei-cosmos-exporter.sh"
if [ ! -f "$installer" ]; then
	echo "sei-cosmos-exporter checkout has no $installer" >&2
	exit 1
fi
sudo bash "$installer"

echo -e "\e[1m\e[32m2. Installing node-exporter... \e[0m" && sleep 1

archive="node_exporter-${NODE_EXPORTER_VERSION}.linux-${arch}"
wget -q -O "$workdir/$archive.tar.gz" \
	"https://github.com/prometheus/node_exporter/releases/download/v${NODE_EXPORTER_VERSION}/${archive}.tar.gz"
tar xfz "$workdir/$archive.tar.gz" -C "$workdir"
sudo mv "$workdir/$archive/node_exporter" /usr/local/bin/node-exporter

# useradd fails when the account is already there, which a re-run should survive.
id node-exporter >/dev/null 2>&1 || sudo useradd -rs /bin/false node-exporter

sudo tee <<EOF >/dev/null /etc/systemd/system/node-exporter.service
[Unit]
Description=Node Exporter
After=network.target

[Service]
User=node-exporter
Group=node-exporter
Type=simple
ExecStart=/usr/local/bin/node-exporter

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable node-exporter
sudo systemctl restart node-exporter

echo -e "\e[1m\e[32mInstallation finished... \e[0m" && sleep 1
echo -e "\e[1m\e[32mPlease make sure ports 9100 and 9300 are open \e[0m" && sleep 1
