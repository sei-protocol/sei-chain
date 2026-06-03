#!/usr/bin/env bash
#
# One-shot runner for the Sei EVM JSON-RPC test suite.
#
#   1. Starts the local 4-node Sei docker cluster (make docker-cluster-start).
#   2. Starts the geth --dev reference node (npm run rpc:geth).
#   3. Runs the suite (bootstrap + parallel run) producing mochawesome JSON.
#   4. Merges the per-phase JSON into a single combined mochawesome HTML report.
#
# The geth node we start is always torn down on exit. The docker cluster is left
# running by default (set STOP_CLUSTER=true to tear it down too); a subsequent run
# is still safe because `docker-cluster-start` stops any existing cluster first.
#
# Env knobs:
#   CLUSTER_TIMEOUT   seconds to wait for the cluster to come up      (default 900)
#   GETH_TIMEOUT      seconds to wait for geth to listen on :9547     (default 120)
#   SEI_TIMEOUT       seconds to wait for Sei EVM RPC on :8545        (default 300)
#   STOP_CLUSTER      "true" to run docker-cluster-stop on exit       (default false)
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RPC_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$RPC_DIR/../.." && pwd)"

GETH_PORT=9547
SEI_EVM_RPC_URL="${SEI_EVM_RPC:-http://localhost:8545}"
GETH_RPC_URL="${RPC_ETH_GETH:-http://127.0.0.1:${GETH_PORT}}"
CLUSTER_TIMEOUT="${CLUSTER_TIMEOUT:-900}"
GETH_TIMEOUT="${GETH_TIMEOUT:-120}"
SEI_TIMEOUT="${SEI_TIMEOUT:-300}"
STOP_CLUSTER="${STOP_CLUSTER:-false}"
# RPC_SERIAL=true runs the suite in a single mocha process instead of the default
# process-sharded parallel run.
RPC_SERIAL="${RPC_SERIAL:-false}"

REPORT_DIR="$RPC_DIR/reports/new_rpc"
GETH_LOG="$RPC_DIR/reports/geth.log"
GETH_PID=""

log()  { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

cleanup() {
    local code=$?
    log "Cleaning up"
    if [ -n "$GETH_PID" ] && kill -0 "$GETH_PID" 2>/dev/null; then
        kill "$GETH_PID" 2>/dev/null || true
    fi
    # Make sure nothing is left bound to the geth port.
    local stray
    stray="$(lsof -ti tcp:${GETH_PORT} 2>/dev/null || true)"
    [ -n "$stray" ] && kill $stray 2>/dev/null || true
    if [ "$STOP_CLUSTER" = "true" ]; then
        log "Stopping docker cluster"
        ( cd "$REPO_ROOT" && make docker-cluster-stop ) || warn "docker-cluster-stop failed"
    fi
    exit $code
}
trap cleanup EXIT INT TERM

# Read a single eth_blockNumber (decimal) from an EVM RPC, or empty on failure.
eth_block_number() {
    local url="$1" hex
    hex="$(curl -s -m 3 -X POST -H 'content-type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
        "$url" 2>/dev/null | sed -n 's/.*"result":"\(0x[0-9a-fA-F]*\)".*/\1/p')"
    [ -n "$hex" ] && printf '%d' "$hex" 2>/dev/null || true
}

# Poll an EVM JSON-RPC endpoint until it answers eth_chainId or times out.
wait_for_rpc() {
    local url="$1" name="$2" timeout="$3" elapsed=0
    log "Waiting for $name at $url (timeout ${timeout}s)"
    while [ "$elapsed" -lt "$timeout" ]; do
        if curl -s -m 3 -X POST -H 'content-type: application/json' \
            --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
            "$url" 2>/dev/null | grep -q '"result"'; then
            log "$name is up (after ${elapsed}s)"
            return 0
        fi
        sleep 2; elapsed=$((elapsed + 2))
    done
    return 1
}

wait_for_block_production() {
    local url="$1" name="$2" timeout="$3" elapsed=0 first second
    log "Waiting for $name to produce blocks (timeout ${timeout}s)"
    while [ "$elapsed" -lt "$timeout" ]; do
        first="$(eth_block_number "$url")"
        if [ -n "$first" ] && [ "$first" -gt 0 ] 2>/dev/null; then
            sleep 3
            second="$(eth_block_number "$url")"
            if [ -n "$second" ] && [ "$second" -gt "$first" ] 2>/dev/null; then
                log "$name is minting blocks (height $first -> $second, after ${elapsed}s)"
                return 0
            fi
            elapsed=$((elapsed + 3))
        fi
        sleep 2; elapsed=$((elapsed + 2))
    done
    return 1
}

# Wait for the cluster's launch.complete sentinel (>=4 nodes ready).
wait_for_cluster() {
    local sentinel="$REPO_ROOT/build/generated/launch.complete" elapsed=0
    log "Waiting for Sei cluster readiness ($sentinel, timeout ${CLUSTER_TIMEOUT}s)"
    while [ "$elapsed" -lt "$CLUSTER_TIMEOUT" ]; do
        if [ -f "$sentinel" ] && [ "$(wc -l < "$sentinel" 2>/dev/null || echo 0)" -ge 4 ]; then
            log "All 4 nodes reported ready (after ${elapsed}s)"
            return 0
        fi
        sleep 5; elapsed=$((elapsed + 5))
    done
    return 1
}

command -v geth >/dev/null 2>&1 || die "geth not found on PATH; install go-ethereum to run the reference node."
command -v curl >/dev/null 2>&1 || die "curl is required."

mkdir -p "$REPORT_DIR"

# --- 1. Start the Sei docker cluster (detached) ---------------------------------
log "Starting Sei docker cluster (DOCKER_DETACH=true make docker-cluster-start)"
( cd "$REPO_ROOT" && DOCKER_DETACH=true make docker-cluster-start ) \
    || die "make docker-cluster-start failed"

wait_for_cluster || die "Sei cluster did not become ready within ${CLUSTER_TIMEOUT}s"
wait_for_rpc "$SEI_EVM_RPC_URL" "Sei EVM RPC" "$SEI_TIMEOUT" \
    || die "Sei EVM RPC at $SEI_EVM_RPC_URL never came up"
# The cluster sentinel only means the nodes booted; the bootstrap's funding +
# association txs need the chain to actually be committing blocks, so gate on that.
wait_for_block_production "$SEI_EVM_RPC_URL" "Sei chain" "$SEI_TIMEOUT" \
    || die "Sei chain at $SEI_EVM_RPC_URL is up but not producing blocks within ${SEI_TIMEOUT}s"

# --- 2. Start the geth --dev reference node -------------------------------------
log "Starting geth reference node (npm run rpc:geth) -> $GETH_LOG"
( cd "$RPC_DIR" && npm run --silent rpc:geth ) > "$GETH_LOG" 2>&1 &
GETH_PID=$!

wait_for_rpc "$GETH_RPC_URL" "geth reference" "$GETH_TIMEOUT" \
    || { warn "geth log tail:"; tail -n 20 "$GETH_LOG" || true; die "geth never came up on $GETH_RPC_URL"; }

# --- 3. Run the suite (don't abort on test failures; we still want a report) -----
cd "$RPC_DIR"
# Clear stale run reports so the merge never mixes a previous run's shards with this
# one (e.g. parallel run-*.json left over before a serial run, or vice versa).
rm -f "$REPORT_DIR"/run.json "$REPORT_DIR"/run-*.json

log "Running bootstrap (npm run rpc:bootstrap)"
npm run rpc:bootstrap; BOOT_CODE=$?

if [ "$RPC_SERIAL" = "true" ]; then
    log "Running suite sequentially (npm run rpc:run:serial)"
    npm run rpc:run:serial; RUN_CODE=$?
else
    log "Running suite in parallel (npm run rpc:run)"
    npm run rpc:run; RUN_CODE=$?
fi

# --- 4. Merge mochawesome reports into one combined HTML ------------------------
# mochawesome-merge wants a single glob (multiple explicit file args only reads the
# first). We glob the per-phase JSON in $REPORT_DIR and write the merged output to
# $RPC_DIR/reports so a re-run's glob never re-ingests its own merged.json.
log "Merging mochawesome reports"
if ls "$REPORT_DIR"/*.json >/dev/null 2>&1; then
    npx mochawesome-merge "$REPORT_DIR/*.json" > "$RPC_DIR/reports/merged.json" \
        && npx marge "$RPC_DIR/reports/merged.json" -o "$RPC_DIR/reports/merged" \
            -f rpc-tests --reportTitle "Sei RPC Tests" --charts \
        && log "Combined report: $RPC_DIR/reports/merged/rpc-tests.html"
else
    warn "No mochawesome JSON found to merge (did the suite produce any reports?)"
fi

# --- Result ---------------------------------------------------------------------
if [ "$BOOT_CODE" -ne 0 ] || [ "$RUN_CODE" -ne 0 ]; then
    warn "Test run finished with failures (bootstrap=$BOOT_CODE, run=$RUN_CODE)"
    exit 1
fi
log "All RPC tests passed"
