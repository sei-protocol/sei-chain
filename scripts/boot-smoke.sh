#!/usr/bin/env bash
# Boot the given seid binary N times against throwaway single-node genesis homes and
# assert every boot gets through the genesis wasm store (the first wasmer JIT compile)
# and completes the ABCI handshake.
#
# The first boot pins RAYON_NUM_THREADS=1. Serializing wasmer's compile makes the
# gcc>=12 unwind b-tree crash DETERMINISTIC on an affected binary (empirically 100% vs
# ~70% with default threading), so the gate rejects a broken binary in a single boot
# rather than relying on probability. A healthy binary boots clean regardless of thread
# count. The remaining boots use default threading for broader timing coverage.
#
# Why "handshake completed" is the success marker: a lone node cannot leave blocksync on
# this codebase (blocksync's IsCaughtUp requires more than one peer), so block production
# is not observable single-node. The handshake only completes after the genesis wasm
# StoreCode calls succeed (Sei stores its built-in pointer contracts at InitChain), which
# is exactly the code path that crashes.
#
# Linux-only (runs the linux/amd64 binary natively; uses GNU timeout).
#
# Usage: boot-smoke.sh <path-to-seid> [boots]
#
# BOOT_TIMEOUT (seconds, default 25) bounds each boot. The default is sized for a
# native boot, where the handshake lands about a second in. Raise it when the binary
# runs under emulation, which the release hook does for arm64 on an amd64 runner:
# wasmer's JIT compile at the genesis wasm store is the slow part and emulation
# multiplies it, so a native-sized window can kill a healthy binary.
set -euo pipefail

BIN=${1:?usage: boot-smoke.sh <path-to-seid> [boots]}
BOOTS=${2:-8}
BOOT_TIMEOUT=${BOOT_TIMEOUT:-25}
CHAIN_ID=boot-smoke-1

# Linux-only: this gate runs the binary natively and uses GNU timeout. Skip cleanly on
# other hosts so local `goreleaser release --snapshot` on macOS still works
# (build-static.sh cross-builds in Docker there, but the ELF can't run on the host).
# The real release path and the CI static-build jobs both run on Linux and always
# execute the gate.
if [ "$(uname -s)" != "Linux" ]; then
  echo "boot-smoke: host is $(uname -s), not Linux; skipping (cannot run a linux binary natively here)"
  exit 0
fi

# Refuse a binary this host cannot execute. Natively that means a matching
# architecture; with binfmt registered a foreign one runs too, which is how the release
# hook boots the arm64 binary on an amd64 runner. Probing execution covers both, and
# without it the run reaches `seid init`, dies with an exec-format error, and reports
# "did not reach the ABCI handshake", which reads as a crashing binary rather than a
# binary this machine was never able to run.
if ! "$BIN" version >/dev/null 2>&1; then
  echo "boot-smoke: ERROR: cannot execute $BIN on $(uname -m)." >&2
  echo "            For a foreign architecture, register binfmt before running this." >&2
  exit 1
fi

for i in $(seq 1 "$BOOTS"); do
  H=$(mktemp -d)
  "$BIN" init smoke --chain-id "$CHAIN_ID" --home "$H" >/dev/null 2>&1
  sed -i 's/"stake"/"usei"/g' "$H/config/genesis.json"
  "$BIN" keys add val --keyring-backend test --home "$H" >/dev/null 2>&1
  ADDR=$("$BIN" keys show val -a --keyring-backend test --home "$H")
  "$BIN" add-genesis-account "$ADDR" 100000000000000usei --home "$H" >/dev/null
  "$BIN" gentx val 10000000000000usei --chain-id "$CHAIN_ID" --keyring-backend test --home "$H" >/dev/null 2>&1
  "$BIN" collect-gentxs --home "$H" >/dev/null 2>&1

  # First boot: force the deterministic single-threaded repro. Others: default threading.
  RAYON=""
  [ "$i" -eq 1 ] && RAYON="RAYON_NUM_THREADS=1"

  LOG="$H/start.log"
  env $RAYON timeout -k 5 "$BOOT_TIMEOUT" "$BIN" start --home "$H" >"$LOG" 2>&1 || true

  if grep -qE "SIGSEGV|SIGILL|SIGBUS|panic:" "$LOG"; then
    echo "boot-smoke: boot $i/$BOOTS CRASHED${RAYON:+ (RAYON_NUM_THREADS=1)}:" >&2
    tail -25 "$LOG" >&2
    exit 1
  fi
  if ! grep -q "Completed ABCI Handshake" "$LOG"; then
    echo "boot-smoke: boot $i/$BOOTS did not reach the ABCI handshake${RAYON:+ (RAYON_NUM_THREADS=1)}:" >&2
    tail -25 "$LOG" >&2
    exit 1
  fi
  echo "boot-smoke: boot $i/$BOOTS ok${RAYON:+ (RAYON_NUM_THREADS=1, deterministic)}"
  rm -rf "$H"
done
echo "boot-smoke: all $BOOTS boots clean"
