#!/usr/bin/env bash
#
# CI orchestrator for the Sei EVM JSON-RPC parity suite.
#
# Env knobs:
#   SEI_EVM_RPC     Sei EVM RPC URL                         (default http://localhost:8545)
#   RPC_ETH_GETH    geth reference URL                      (default http://127.0.0.1:9547)
#   SEI_TIMEOUT     seconds to wait for Sei RPC/blocks      (default 300)
#   GETH_TIMEOUT    seconds to wait for geth to listen      (default 120)
#   SKIP_NPM_CI     "true" to reuse an existing node_modules (default false)
#   GETH_VERSION    pinned go-ethereum tag installed on CI  (default v1.17.0)
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RPC_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

GETH_PORT=9547
SEI_EVM_RPC_URL="${SEI_EVM_RPC:-http://localhost:8545}"
GETH_RPC_URL="${RPC_ETH_GETH:-http://127.0.0.1:${GETH_PORT}}"
SEI_TIMEOUT="${SEI_TIMEOUT:-300}"
GETH_TIMEOUT="${GETH_TIMEOUT:-120}"
SKIP_NPM_CI="${SKIP_NPM_CI:-false}"
GETH_VERSION="${GETH_VERSION:-v1.17.0}"

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
    local stray
    stray="$(lsof -ti tcp:${GETH_PORT} 2>/dev/null || true)"
    [ -n "$stray" ] && kill $stray 2>/dev/null || true
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

# The cluster sentinel only means nodes booted; the bootstrap's funding + association
# txs need the chain to actually be committing blocks, so gate on observed progress.
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

# Make a geth binary available, pinned to GETH_VERSION for reproducible parity output.
ensure_geth() {
    local pin="${GETH_VERSION#v}"
    if command -v geth >/dev/null 2>&1; then
        local have
        have="$(geth version 2>/dev/null | sed -n 's/^Version: //p' | head -1)"
        log "Using already-installed geth: $(command -v geth) (version ${have:-unknown}, pinned target ${pin})"
        case "$have" in
            "$pin"*) : ;;
            *) warn "installed geth ${have:-unknown} != pinned ${pin}; comparing against the local binary (install geth@${GETH_VERSION} or set GETH_VERSION to match CI)" ;;
        esac
        return 0
    fi
    command -v go >/dev/null 2>&1 || die "geth not found on PATH and no go toolchain to install the pinned ${GETH_VERSION}."
    log "geth not found; installing pinned go-ethereum ${GETH_VERSION} via go install"
    GOFLAGS=-mod=mod go install "github.com/ethereum/go-ethereum/cmd/geth@${GETH_VERSION}" \
        || die "go install geth@${GETH_VERSION} failed"
    local gobin
    gobin="$(go env GOBIN)"; [ -n "$gobin" ] || gobin="$(go env GOPATH)/bin"
    case ":$PATH:" in *":${gobin}:"*) : ;; *) export PATH="${gobin}:$PATH" ;; esac
    command -v geth >/dev/null 2>&1 || die "geth still not on PATH after go install (looked in ${gobin})"
    log "Installed geth: $(command -v geth) ($(geth version 2>/dev/null | sed -n 's/^Version: //p' | head -1))"
}

command -v curl >/dev/null 2>&1 || die "curl is required."
command -v node >/dev/null 2>&1 || die "node is required (the workflow sets up Node 22)."

cd "$RPC_DIR"
mkdir -p "$REPORT_DIR"

if [ "$SKIP_NPM_CI" = "true" ] && [ -d node_modules ]; then
    log "Reusing existing node_modules (SKIP_NPM_CI=true)"
else
    log "Installing dependencies (npm ci)"
    npm ci || die "npm ci failed"
fi

log "Compiling contracts (npm run compile)"
npm run --silent compile || die "contract compile failed"

wait_for_rpc "$SEI_EVM_RPC_URL" "Sei EVM RPC" "$SEI_TIMEOUT" \
    || die "Sei EVM RPC at $SEI_EVM_RPC_URL never came up (is the cluster started?)"
wait_for_block_production "$SEI_EVM_RPC_URL" "Sei chain" "$SEI_TIMEOUT" \
    || die "Sei chain at $SEI_EVM_RPC_URL is up but not producing blocks within ${SEI_TIMEOUT}s"

ensure_geth
log "Starting geth reference node (npm run rpc:geth) -> $GETH_LOG"
npm run --silent rpc:geth > "$GETH_LOG" 2>&1 &
GETH_PID=$!
wait_for_rpc "$GETH_RPC_URL" "geth reference" "$GETH_TIMEOUT" \
    || { warn "geth log tail:"; tail -n 20 "$GETH_LOG" || true; die "geth never came up on $GETH_RPC_URL"; }

# The suite runs in a single process: every spec shares the one Sei chain, so a
# parallel run would have specs contend on the base fee and the funded-account pool.
rm -f "$REPORT_DIR"/run.json "$REPORT_DIR"/run-*.json
rm -f "$RPC_DIR/runtime/runtime.json"

log "Running bootstrap (npm run rpc:bootstrap)"
npm run rpc:bootstrap; BOOT_CODE=$?

if [ "$BOOT_CODE" -ne 0 ]; then
    warn "bootstrap failed (exit $BOOT_CODE); skipping the spec run so it can't run against stale fixtures"
    RUN_CODE=0
else
    log "Running suite (npm run rpc:run)"
    npm run rpc:run; RUN_CODE=$?
fi

log "Merging mochawesome reports (npm run report:merge) -> $RPC_DIR/reports/merged"
npm run --silent report:merge || warn "report merge failed (continuing so the rest of cleanup runs)"

if [ "$BOOT_CODE" -ne 0 ] || [ "$RUN_CODE" -ne 0 ]; then
    die "RPC test run finished with failures (bootstrap=$BOOT_CODE, run=$RUN_CODE)"
fi
log "All RPC tests passed"
