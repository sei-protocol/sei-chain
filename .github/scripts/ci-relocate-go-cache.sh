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
# GOPATH is deliberately left untouched: the Makefile mounts a hardcoded
# $HOME/go/pkg/mod into the build container but pre-creates the dir via
# `go env GOPATH`; overriding GOPATH would decouple those two and leave the
# container writing to a root-owned auto-created mount. No-op when /mnt is
# unavailable.
set -euo pipefail

if [ ! -d /mnt ] || ! sudo test -w /mnt; then
  echo "/mnt is not available/writable; leaving Go caches on '/'."
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
