#!/usr/bin/env bash
#
# Move Docker's data-root onto the runner's large ephemeral disk (/mnt) so that
# image layers and container writable layers (the running Sei cluster's state)
# accumulate off the small root filesystem.
#
# No-op (clean exit) when /mnt is unavailable or Docker is not present, so it is
# safe across runner classes. Must run BEFORE any docker pull/build so images
# land on /mnt from the start.
set -euo pipefail

target=/mnt/docker

if [ ! -d /mnt ] || ! sudo test -w /mnt; then
  echo "/mnt is not available/writable; leaving Docker data-root on '/'."
  exit 0
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "docker not installed; nothing to relocate."
  exit 0
fi

echo "Relocating Docker data-root to ${target}"
sudo mkdir -p "${target}" /etc/docker

# Merge into any existing daemon.json rather than clobbering it. python3 is
# preinstalled on all GitHub-hosted ubuntu images.
sudo python3 - "${target}" <<'PY'
import json, os, sys

path = "/etc/docker/daemon.json"
cfg = {}
if os.path.exists(path):
    try:
        with open(path) as fh:
            cfg = json.load(fh) or {}
    except ValueError:
        cfg = {}
cfg["data-root"] = sys.argv[1]
with open(path, "w") as fh:
    json.dump(cfg, fh, indent=2)
print("daemon.json ->", cfg)
PY

sudo systemctl restart docker

# Wait for the daemon to accept connections again before the next step runs a
# docker command.
for _ in $(seq 1 30); do
  if docker info >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

docker info --format 'Docker data-root is now: {{.DockerRootDir}}'
