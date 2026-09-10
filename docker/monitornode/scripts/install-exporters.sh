#!/usr/bin/env bash

echo -e "\e[1m\e[32m2. Installing sei-cosmos-exporter... \e[0m" && sleep 1
# install node-exporter

git clone https://github.com/sei-protocol/sei-cosmos-exporter.git
cd sei-cosmos-exporter || exit
git fetch
sudo bash ./install-sei-cosmos-exporter.sh

echo -e "\e[1m\e[32m2. Installing node-exporter... \e[0m" && sleep 1
wget https://github.com/prometheus/node_exporter/releases/download/v1.3.1/node_exporter-1.3.1.linux-amd64.tar.gz
tar xvfz node_exporter-*.*-amd64.tar.gz
sudo mv node_exporter-*.*-amd64/node_exporter /usr/local/bin/node-exporter
rm node_exporter-* -rf

sudo useradd -rs /bin/false node-exporter

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
