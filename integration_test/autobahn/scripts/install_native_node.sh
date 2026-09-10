#!/usr/bin/env bash
set -euo pipefail

archive=${1:?node archive is required}
service_user=${2:?service user is required}
go_max_procs=${3:?GOMAXPROCS value is required}
go_gc=${4:?GOGC value is required}
service_home=$(getent passwd "$service_user" | cut -d: -f6)
if [[ -z "$service_home" ]]; then
  echo "home directory not found for $service_user" >&2
  exit 1
fi

sudo systemctl stop seid.service 2>/dev/null || true
if [[ -e "$service_home/.sei" ]]; then
  mv "$service_home/.sei" "$service_home/.sei.autobahn-backup-$(date +%s)"
fi
tar -C "$service_home" -xzf "$archive"

sudo install -m 0644 /dev/null /etc/seid-native.env
{
  echo 'LD_LIBRARY_PATH=/opt/seid/lib'
  if [[ "$go_max_procs" != "0" ]]; then
    echo "GOMAXPROCS=$go_max_procs"
  fi
  echo "GOGC=$go_gc"
} | sudo tee /etc/seid-native.env >/dev/null

sed \
  -e "s|__SERVICE_USER__|$service_user|g" \
  -e "s|__SERVICE_HOME__|$service_home|g" \
  integration_test/autobahn/systemd/seid.service | \
  sudo tee /etc/systemd/system/seid.service >/dev/null

sudo install -m 0644 integration_test/autobahn/systemd/99-seid-native.conf /etc/sysctl.d/99-seid-native.conf
sudo sysctl --system >/dev/null
sudo systemctl daemon-reload
sudo systemctl enable seid.service >/dev/null
