#!/usr/bin/env bash
#
# Installs node-exporter on 9100 as a systemd service on a validator host.
# Validator, wallet and bank metrics need no exporter: seid reports them as
# sei_chain_cosmos_* on its own Prometheus endpoint when [cosmos_metrics] is
# enabled in app.toml.
#
# Usage: ./install-exporters.sh
#
# Requirements: wget, tar and sudo. Linux only — node-exporter is fetched
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

echo -e "\e[1m\e[32mInstalling node-exporter... \e[0m" && sleep 1

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
echo -e "\e[1m\e[32mPlease make sure port 9100 is open \e[0m" && sleep 1
