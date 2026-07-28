#!/usr/bin/env bash
#
# CI orchestrator for the Sei precompile e2e suite.
#
# Unlike rpc_tests there is no geth reference node: precompiles have no geth
# counterpart, so the parity oracle is the Sei chain's own Cosmos-side state.
#
# Env knobs:
#   SEI_EVM_RPC     Sei EVM RPC URL                          (default http://localhost:8545)
#   SEI_TIMEOUT     seconds to wait for Sei RPC/blocks       (default 300)
#   SKIP_NPM_CI     "true" to reuse an existing node_modules (default false)
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

SEI_EVM_RPC_URL="${SEI_EVM_RPC:-http://localhost:8545}"
SEI_TIMEOUT="${SEI_TIMEOUT:-300}"
SKIP_NPM_CI="${SKIP_NPM_CI:-false}"

REPORT_DIR="$SUITE_DIR/reports/precompile"

log()  { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

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

command -v curl >/dev/null 2>&1 || die "curl is required."
command -v node >/dev/null 2>&1 || die "node is required (the workflow sets up Node 22)."

cd "$SUITE_DIR"
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

# The suite runs in a single process: every spec shares the one Sei chain and the
# bootstrap's funded-account pool (claimPool's cursor is only correct in-process).
rm -f "$REPORT_DIR"/run.json "$REPORT_DIR"/run-*.json
rm -f "$SUITE_DIR/runtime/runtime.json"

log "Running bootstrap (npm run precompile:bootstrap)"
npm run precompile:bootstrap; BOOT_CODE=$?

if [ "$BOOT_CODE" -ne 0 ]; then
    warn "bootstrap failed (exit $BOOT_CODE); skipping the spec run so it can't run against stale fixtures"
    RUN_CODE=0
else
    log "Running suite (npm run precompile:run)"
    npm run precompile:run; RUN_CODE=$?
fi

log "Merging mochawesome reports (npm run report:merge) -> $SUITE_DIR/reports/merged"
npm run --silent report:merge || warn "report merge failed (continuing so the rest of cleanup runs)"

if [ "$BOOT_CODE" -ne 0 ] || [ "$RUN_CODE" -ne 0 ]; then
    die "Precompile test run finished with failures (bootstrap=$BOOT_CODE, run=$RUN_CODE)"
fi
log "All precompile tests passed"
