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
set -euo pipefail

BIN=${1:?usage: boot-smoke.sh <path-to-seid> [boots]}
BOOTS=${2:-8}
CHAIN_ID=boot-smoke-1

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
  env $RAYON timeout -k 5 25 "$BIN" start --home "$H" >"$LOG" 2>&1 || true

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
