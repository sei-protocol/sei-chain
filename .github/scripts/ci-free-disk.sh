#!/usr/bin/env bash
#
# Reclaim disk on GitHub-hosted runners before disk-heavy CI work.
#
# Root cause: the integration and lint jobs exhaust the runner's root
# filesystem. When '/' fills, the Actions runner *worker process itself* crashes
# with "No space left on device" mid-step.
#
# Safe on any ubuntu-* runner: every removal is guarded so a change to the
# runner image layout can never fail the job.
set -euo pipefail

echo "::group::Disk usage before reclaim"
df -h / /mnt 2>/dev/null || df -h /
echo "::endgroup::"

# Removing the bundles below costs ~3.5 min of rm -rf. Skip it when the root
# filesystem already has enough headroom (large runners ship with >80 GiB free).
min_free_gib="${CI_FREE_DISK_MIN_GIB:-40}"
avail_gib=$(df -BG --output=avail / | tail -1 | tr -dc '0-9')
if [ "${avail_gib}" -ge "${min_free_gib}" ]; then
  echo "Root filesystem has ${avail_gib}G free (>= ${min_free_gib}G); skipping reclaim."
  exit 0
fi

# Large preinstalled bundles unused by Sei's Go/Docker/Node CI.
junk=(
  /usr/share/dotnet
  /usr/local/lib/android
  /opt/ghc
  /usr/local/.ghcup
  /opt/hostedtoolcache/CodeQL
  /usr/local/share/boost
  /usr/share/swift
)
for dir in "${junk[@]}"; do
  if [ -d "$dir" ]; then
    echo "Removing $dir"
    sudo rm -rf "$dir" || true
  fi
done

echo "::group::Disk usage after reclaim"
df -h / /mnt 2>/dev/null || df -h /
echo "::endgroup::"
