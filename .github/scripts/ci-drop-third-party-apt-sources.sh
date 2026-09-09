#!/usr/bin/env bash
#
# Drop third-party apt sources that GitHub's ubuntu-* images ship and that we
# never install from. Those repos (Google Chrome, Microsoft) periodically serve
# a Packages.gz whose hash does not match the signed Release file, so any
# `apt-get update` — even one that only wants build-essential — fails the job.
#
# Safe on any ubuntu-* runner: every removal is guarded so a change to the
# runner image layout can never fail the job. Filenames are globbed so a
# renamed chrome/microsoft list still gets dropped.
set -euo pipefail

# Unmatched globs must not be treated as literal paths.
shopt -s nullglob

for src in /etc/apt/sources.list.d/*google*.list /etc/apt/sources.list.d/*google*.sources \
  /etc/apt/sources.list.d/*microsoft*.list /etc/apt/sources.list.d/*microsoft*.sources; do
  echo "Removing $src"
  sudo rm -f "$src" || true
done
