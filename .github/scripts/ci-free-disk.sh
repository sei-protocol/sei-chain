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
