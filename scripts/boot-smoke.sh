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
# Linux-only (uses GNU timeout). Runs the binary natively, or through binfmt when it
# was built for another architecture, which is how the release hook boots the arm64
# binary on an amd64 runner.
#
# Usage: boot-smoke.sh <path-to-seid> [boots]
#
# BOOT_TIMEOUT (seconds, default 25) is the ceiling for one boot, not its cost: a boot
# ends shortly after the handshake appears. Raise it when the binary runs under
# emulation, which the release hook does for arm64 on an amd64 runner, since wasmer's
# JIT compile at the genesis wasm store is the slow part and emulation multiplies it.
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

# Classify the binary before running it, so each failure reports its own cause. The
# execution probe alone cannot: a missing path, a non-executable file, a foreign
# architecture and a seid that crashes in `version` all fail it, and reporting them all
# as a binfmt problem hides the one this gate exists to catch.
[ -e "$BIN" ] || { echo "boot-smoke: ERROR: $BIN does not exist." >&2; exit 1; }
[ -x "$BIN" ] || { echo "boot-smoke: ERROR: $BIN is not executable." >&2; exit 1; }

# Read the ELF header once and length-check it, so a non-ELF or truncated file reports
# itself rather than yielding a garbage machine value or dying inside od.
elf_header=$(od -An -tx1 -N20 "$BIN" | tr -d ' \n')
if [ "${#elf_header}" -ne 40 ] || [ "${elf_header:0:8}" != "7f454c46" ]; then
  echo "boot-smoke: ERROR: $BIN is not an ELF binary, or its header is truncated." >&2
  exit 1
fi
elf_machine=${elf_header:36:4}
case "$(uname -m)" in
  x86_64)         host_machine=3e00 ;;
  aarch64|arm64)  host_machine=b700 ;;
  *) echo "boot-smoke: unsupported host architecture $(uname -m)" >&2; exit 1 ;;
esac

# The probe runs for every binary, so a native one that cannot run its own subcommands is
# still reported. Only the message branches on architecture: a foreign binary that fails
# here needs binfmt, a native one that fails is the defect this gate exists to catch.
if ! "$BIN" version >/dev/null 2>&1; then
  if [ "$elf_machine" != "$host_machine" ]; then
    echo "boot-smoke: ERROR: $BIN is a foreign-architecture binary (e_machine $elf_machine)" >&2
    echo "            that this host ($(uname -m)) cannot run; register binfmt first." >&2
  else
    echo "boot-smoke: ERROR: $BIN is a native $(uname -m) binary but 'seid version' failed:" >&2
    "$BIN" version >&2 2>&1 || true
  fi
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
  # Stop as soon as the handshake lands rather than waiting out the budget: BOOT_TIMEOUT
  # is the ceiling for a slow boot, not the price of a healthy one. Without this an
  # emulated budget large enough to be safe would also be the guaranteed cost.
  rc=0
  saw_handshake=0
  crash_log="$LOG"
  env $RAYON timeout -k 5 "$BOOT_TIMEOUT" "$BIN" start --home "$H" >"$LOG" 2>&1 &
  boot_pid=$!
  while kill -0 "$boot_pid" 2>/dev/null; do
    if grep -q "Completed ABCI Handshake" "$LOG" 2>/dev/null; then
      saw_handshake=1
      # Brief dwell so a crash immediately after the handshake still reaches the log.
      sleep 3
      # Snapshot before the TERM. The signal now arrives seconds after the handshake,
      # while blocksync and the RPC servers are still starting, so a panic on a
      # half-initialised shutdown path would otherwise be read as a crash.
      cp "$LOG" "$LOG.run" && crash_log="$LOG.run"
      kill "$boot_pid" 2>/dev/null || true
      break
    fi
    sleep 1
  done
  wait "$boot_pid" 2>/dev/null || rc=$?

  if grep -qE "SIGSEGV|SIGILL|SIGBUS|panic:" "$crash_log"; then
    echo "boot-smoke: boot $i/$BOOTS CRASHED${RAYON:+ (RAYON_NUM_THREADS=1)}:" >&2
    tail -25 "$LOG" >&2
    exit 1
  fi
  # A healthy boot is killed by `timeout` too, since the node keeps running once the
  # handshake completes, so the exit status alone cannot tell the two apart. The
  # handshake decides; the status then says whether the budget ran out or the process
  # left on its own, which are different problems with different fixes.
  if [ "$saw_handshake" -eq 0 ] && ! grep -q "Completed ABCI Handshake" "$LOG"; then
    case "$rc" in
      124|137)
        echo "boot-smoke: boot $i/$BOOTS TIMED OUT after ${BOOT_TIMEOUT}s before the ABCI handshake${RAYON:+ (RAYON_NUM_THREADS=1)}." >&2
        echo "            The binary may be healthy but too slow for this budget; raise BOOT_TIMEOUT" >&2
        echo "            if it is running under emulation." >&2
        ;;
      *)
        echo "boot-smoke: boot $i/$BOOTS exited (status $rc) before the ABCI handshake${RAYON:+ (RAYON_NUM_THREADS=1)}:" >&2
        ;;
    esac
    tail -25 "$LOG" >&2
    exit 1
  fi
  echo "boot-smoke: boot $i/$BOOTS ok${RAYON:+ (RAYON_NUM_THREADS=1, deterministic)}"
  rm -rf "$H"
done
echo "boot-smoke: all $BOOTS boots clean"
