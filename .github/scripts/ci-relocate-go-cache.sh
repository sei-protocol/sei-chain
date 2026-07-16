#!/usr/bin/env bash
#
# Point the Go build/module/tmp caches at the runner's large ephemeral disk
# (/mnt) so that compiling the full tree — `go vet ./...` on the lint job, the
# in-container seid build on prepare-cluster — does not fill the small root
# filesystem.
#
# Writes the new locations to $GITHUB_ENV so every subsequent step inherits
# them. GOCACHE is the dominant consumer (the build cache); GOMODCACHE and
# GOTMPDIR are moved too.
#
# The Makefile derives its container/compose module-cache mount from
# `go env GOMODCACHE` (via GO_PKG_PATH), so relocating GOMODCACHE here also
# moves the in-container module cache to /mnt and keeps it aligned with the
# cache actions/setup-go restores. GOPATH is deliberately left untouched (it
# only affects binary/tool install paths we do not relocate). No-op when /mnt
# is unavailable.
set -euo pipefail

if [ ! -d /mnt ] || ! sudo test -w /mnt; then
  echo "/mnt is not available/writable; leaving Go caches on '/'."
  exit 0
fi
# GitHub larger runners (e.g. ubuntu-large) expose /mnt as a directory on the
# single OS disk, not a separate ephemeral volume. Relocating within the same
# filesystem frees no capacity, so skip it when /mnt and / are the same device.
if [ "$(stat -c '%d' /mnt 2>/dev/null)" = "$(stat -c '%d' / 2>/dev/null)" ]; then
  echo "/mnt shares the root filesystem; leaving Go caches on '/'."
  exit 0
fi

base=/mnt/go
sudo mkdir -p "${base}/pkg/mod" "${base}/cache" "${base}/tmp"
sudo chown -R "$(id -u):$(id -g)" "${base}"

{
  echo "GOMODCACHE=${base}/pkg/mod"
  echo "GOCACHE=${base}/cache"
  echo "GOTMPDIR=${base}/tmp"
} >> "${GITHUB_ENV}"

echo "Go caches relocated to ${base} (GOCACHE/GOMODCACHE/GOTMPDIR)."
