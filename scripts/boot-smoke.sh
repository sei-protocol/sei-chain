#!/usr/bin/env bash
# Boot the given seid binary N times against throwaway single-node genesis homes and
# assert every boot gets through the genesis wasm store (the first wasmer JIT compile)
# and completes the ABCI handshake.
#
# Why repeated boots: the class of defect this guards against crashes probabilistically
# at first wasm use (the gcc>=12 unwind b-tree bug killed ~70% of boots, so any single
# boot check can pass on luck). N clean boots make a lucky pass vanishingly unlikely.
#
# Why "handshake completed" is the success marker: a lone node cannot leave blocksync on
# this codebase (blocksync's IsCaughtUp requires more than one peer), so block production
# is not observable single-node. The handshake only completes after the genesis wasm
# StoreCode calls succeed, which is exactly the code path that crashes.
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

  LOG="$H/start.log"
  timeout -k 5 25 "$BIN" start --home "$H" >"$LOG" 2>&1 || true

  if grep -qE "SIGSEGV|SIGILL|SIGBUS|panic:" "$LOG"; then
    echo "boot-smoke: boot $i/$BOOTS CRASHED:" >&2
    tail -25 "$LOG" >&2
    exit 1
  fi
  if ! grep -q "Completed ABCI Handshake" "$LOG"; then
    echo "boot-smoke: boot $i/$BOOTS did not reach the ABCI handshake:" >&2
    tail -25 "$LOG" >&2
    exit 1
  fi
  echo "boot-smoke: boot $i/$BOOTS ok"
  rm -rf "$H"
done
echo "boot-smoke: all $BOOTS boots clean"
