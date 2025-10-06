#!/bin/bash
# SeiMesh Local Presence Daemon
# Broadcasts validator SSID + beacon

set -euo pipefail

SSID="${SEIMESH_SSID:-SeiMesh_$(hostname)}"
PORT="${SEIMESH_PORT:-7545}"
IFACE="${SEIMESH_IFACE:-wlan0}"
PASSWORD="${SEIMESH_PASSWORD:-seiwifi123}"
BEACON_FILE="${SEIMESH_BEACON_FILE:-/tmp/sei_beacon.hash}"
VERIFY_HANDLER="${SEIMESH_VERIFY_HANDLER:-$(dirname "$0")/verify_ping.py}"

log() {
  local level="$1"; shift
  printf '[%s] %s\n' "$level" "$*"
}

hash_ssid() {
  printf '%s' "$SSID" | sha256sum | awk '{print $1}'
}

ensure_dependency() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log '-' "Missing dependency: $1"
    return 1
  fi
}

start_beacon() {
  local beacon_hash
  beacon_hash=$(hash_ssid)
  log '+' "Starting SeiMesh Beacon on SSID: $SSID (hash: $beacon_hash)"
  echo "$beacon_hash" >"$BEACON_FILE"

  if command -v nmcli >/dev/null 2>&1; then
    nmcli dev wifi hotspot ifname "$IFACE" ssid "$SSID" band bg password "$PASSWORD"
  elif command -v termux-wifi-enable >/dev/null 2>&1; then
    termux-wifi-enable true || true
    log '+' "termux-wifi-enable invoked; please ensure hotspot is configured manually"
  else
    log '!' "No supported WiFi manager found (nmcli or termux-wifi-enable)."
  fi
}

stop_beacon() {
  if command -v nmcli >/dev/null 2>&1; then
    nmcli connection down Hotspot >/dev/null 2>&1 || true
  fi
  rm -f "$BEACON_FILE"
}

handle_ping() {
  if [ ! -x "$VERIFY_HANDLER" ]; then
    log '!' "Verifier $VERIFY_HANDLER is not executable"
    return 1
  fi

  while true; do
    log '+' "Listening for incoming presence pings on port $PORT"
    if command -v nc >/dev/null 2>&1; then
      nc -lk -p "$PORT" -e "$VERIFY_HANDLER"
    elif command -v busybox >/dev/null 2>&1 && busybox nc >/dev/null 2>&1; then
      busybox nc -lk -p "$PORT" -e "$VERIFY_HANDLER"
    else
      log '-' "Neither nc nor busybox nc is available"
      sleep 10
    fi
  done
}

trap stop_beacon EXIT

ensure_dependency sha256sum || exit 1
start_beacon &
BEACON_PID=$!

handle_ping
wait "$BEACON_PID"
