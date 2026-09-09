#!/usr/bin/env bash
#
# Drop third-party apt sources that GitHub's ubuntu-* images ship and that we
# never install from. Those repos (Google Chrome, Microsoft) periodically serve
# a Packages.gz whose hash does not match the signed Release file, so any
# `apt-get update` — even one that only wants build-essential — fails the job.
#
# Safe on any ubuntu-* runner: every removal is guarded so a change to the
# runner image layout can never fail the job.
set -euo pipefail

sources=(
  /etc/apt/sources.list.d/google-chrome.list
  /etc/apt/sources.list.d/google-chrome.sources
  /etc/apt/sources.list.d/google.list
  /etc/apt/sources.list.d/microsoft-prod.list
  /etc/apt/sources.list.d/microsoft-prod.sources
)

for src in "${sources[@]}"; do
  if [ -e "$src" ]; then
    echo "Removing $src"
    sudo rm -f "$src"
  fi
done
