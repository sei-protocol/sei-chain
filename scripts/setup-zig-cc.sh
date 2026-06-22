#!/usr/bin/env bash
# Installs Zig and a `zcc` wrapper (zig cc -target x86_64-linux-musl) for building a
# fully static linux/amd64 seid.
#
# Why: Ubuntu 24.04's musl-gcc wrapper cannot produce a fully static link — glibc's
# libgcc_eh references _dl_find_object (glibc >=2.35), which musl does not provide.
# Zig ships its own musl-targeted compiler-rt, so the static link succeeds. Used by
# both the linux-amd64-static CI guard and the goreleaser release prebuild so they
# build identically.
set -euo pipefail

ZIG_VERSION="${ZIG_VERSION:-0.13.0}"
ZIG_PLATFORM="x86_64-linux"

index="$(curl -fsSL https://ziglang.org/download/index.json)"
url="$(printf '%s' "$index" | jq -r --arg v "$ZIG_VERSION" --arg p "$ZIG_PLATFORM" '.[$v][$p].tarball')"
sha="$(printf '%s' "$index" | jq -r --arg v "$ZIG_VERSION" --arg p "$ZIG_PLATFORM" '.[$v][$p].shasum')"

if [ -z "$url" ] || [ "$url" = "null" ]; then
  echo "::error::Zig $ZIG_VERSION ($ZIG_PLATFORM) not found in ziglang.org download index" >&2
  exit 1
fi

curl -fsSL "$url" -o /tmp/zig.tar.xz
echo "${sha}  /tmp/zig.tar.xz" | sha256sum -c -

sudo rm -rf /opt/zig
sudo mkdir -p /opt/zig
sudo tar -xJf /tmp/zig.tar.xz -C /opt/zig --strip-components=1

sudo tee /usr/local/bin/zcc >/dev/null <<'WRAP'
#!/bin/sh
exec /opt/zig/zig cc -target x86_64-linux-musl "$@"
WRAP
sudo chmod +x /usr/local/bin/zcc

/opt/zig/zig version
zcc --version
