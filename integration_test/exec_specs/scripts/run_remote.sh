#!/usr/bin/env bash

set -euo pipefail

SUITE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EEST_DIR="${EEST_DIR:-${SUITE_ROOT}/.cache/execution-specs}"
EEST_FORK="${EEST_FORK:-Prague}"
EEST_TEST_TARGET="${EEST_TEST_TARGET:-}"
EEST_SWEEP_AMOUNT="${EEST_SWEEP_AMOUNT:-100000 ether}"
EEST_MAX_FEE_PER_BLOB_GAS="${EEST_MAX_FEE_PER_BLOB_GAS:-1}"
EEST_EOA_FUND_AMOUNT_DEFAULT="${EEST_EOA_FUND_AMOUNT_DEFAULT:-100000000000000000}"
EEST_SKIP_CLEANUP="${EEST_SKIP_CLEANUP:-1}"
EEST_TOLERATE_MALFORMED_PENDING_TX="${EEST_TOLERATE_MALFORMED_PENDING_TX:-1}"
EEST_POLL_INTERVAL="${EEST_POLL_INTERVAL:-0.2}"
EEST_POST_STATE_WAIT_TIMEOUT="${EEST_POST_STATE_WAIT_TIMEOUT:-2}"
EEST_ORDERED_TX_SUBMISSION="${EEST_ORDERED_TX_SUBMISSION:-1}"
EEST_TX_WAIT_TIMEOUT="${EEST_TX_WAIT_TIMEOUT:-120}"
EEST_RPC_ENDPOINT="${EEST_RPC_ENDPOINT:-${SEI_EVM_RPC:-http://localhost:8545}}"
EEST_RPC_WAIT_SECONDS="${EEST_RPC_WAIT_SECONDS:-120}"
EEST_JUNIT_XML="${EEST_JUNIT_XML:-}"
EEST_PARALLELISM="${EEST_PARALLELISM:-1}"
EEST_SHARD_COUNT="${EEST_SHARD_COUNT:-1}"
EEST_SHARD_INDEX="${EEST_SHARD_INDEX:-0}"

if ! python3 -c \
    'import sys; value = float(sys.argv[1]); assert value > 0' \
    "${EEST_POLL_INTERVAL}" 2>/dev/null; then
    echo "EEST_POLL_INTERVAL must be a positive number of seconds." >&2
    exit 2
fi
if ! python3 -c \
    'import sys; value = float(sys.argv[1]); assert value >= 0' \
    "${EEST_POST_STATE_WAIT_TIMEOUT}" 2>/dev/null; then
    echo "EEST_POST_STATE_WAIT_TIMEOUT must be a non-negative number." >&2
    exit 2
fi
if [[ ! "${EEST_ORDERED_TX_SUBMISSION}" =~ ^[01]$ ]]; then
    echo "EEST_ORDERED_TX_SUBMISSION must be 0 or 1." >&2
    exit 2
fi
if [[ ! "${EEST_TX_WAIT_TIMEOUT}" =~ ^[1-9][0-9]*$ ]]; then
    echo "EEST_TX_WAIT_TIMEOUT must be a positive integer." >&2
    exit 2
fi
if [[ ! "${EEST_PARALLELISM}" =~ ^[1-9][0-9]*$ ]]; then
    echo "EEST_PARALLELISM must be a positive integer." >&2
    exit 2
fi
if [[ ! "${EEST_SHARD_COUNT}" =~ ^[1-9][0-9]*$ ]]; then
    echo "EEST_SHARD_COUNT must be a positive integer." >&2
    exit 2
fi
if [[ ! "${EEST_SHARD_INDEX}" =~ ^[0-9]+$ ]] \
    || (( 10#${EEST_SHARD_INDEX} >= 10#${EEST_SHARD_COUNT} )); then
    echo "EEST_SHARD_INDEX must be between 0 and EEST_SHARD_COUNT - 1." >&2
    exit 2
fi
if [[ ! "${EEST_SEED_KEY:-}" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    echo "EEST_SEED_KEY must be a 0x-prefixed 32-byte private key." >&2
    exit 2
fi

bash "${SUITE_ROOT}/scripts/install.sh"

rpc_ready=0
for _ in $(seq 1 "${EEST_RPC_WAIT_SECONDS}"); do
    if rpc_response="$(
        curl --fail --silent --show-error \
            --header "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
            "${EEST_RPC_ENDPOINT}" 2>/dev/null
    )"; then
        rpc_ready=1
        break
    fi
    sleep 1
done

if [[ "${rpc_ready}" != "1" ]]; then
    echo "EVM RPC did not become ready at ${EEST_RPC_ENDPOINT}." >&2
    exit 2
fi

if [[ -z "${EEST_CHAIN_ID:-}" ]]; then
    EEST_CHAIN_ID="$(
        python3 -c \
            'import json, sys; print(int(json.load(sys.stdin)["result"], 16))' \
            <<<"${rpc_response}"
    )"
fi

eest_revision="$(<"${EEST_DIR}/.sei-eest-fingerprint")"
echo "Running EEST ${eest_revision:0:9} against chain ${EEST_CHAIN_ID} (${EEST_FORK})."

execute_command=(
    "${EEST_DIR}/.venv/bin/execute"
    remote
    --fork="${EEST_FORK}"
    --chain-id="${EEST_CHAIN_ID}"
    --rpc-endpoint="${EEST_RPC_ENDPOINT}"
    --seed-account-sweep-amount="${EEST_SWEEP_AMOUNT}"
    --default-max-fee-per-blob-gas="${EEST_MAX_FEE_PER_BLOB_GAS}"
    --eoa-fund-amount-default="${EEST_EOA_FUND_AMOUNT_DEFAULT}"
    --tx-wait-timeout="${EEST_TX_WAIT_TIMEOUT}"
    -p eest_plugin
)
if [[ "${EEST_SKIP_CLEANUP}" == "1" ]]; then
    execute_command+=(--skip-cleanup)
fi
if [[ "${EEST_PARALLELISM}" -gt 1 ]]; then
    execute_command+=(-n="${EEST_PARALLELISM}")
fi
if [[ -n "${EEST_JUNIT_XML}" ]]; then
    mkdir -p "$(dirname "${EEST_JUNIT_XML}")"
    execute_command+=(--junitxml="${EEST_JUNIT_XML}")
fi
if [[ -n "${EEST_TEST_TARGET}" ]]; then
    execute_command+=("${EEST_TEST_TARGET}")
fi
execute_command+=("$@")

cd "${EEST_DIR}"
PYTHONPATH="${SUITE_ROOT}/plugin${PYTHONPATH:+:${PYTHONPATH}}" \
RPC_SEED_KEY="${EEST_SEED_KEY}" \
EEST_TOLERATE_MALFORMED_PENDING_TX="${EEST_TOLERATE_MALFORMED_PENDING_TX}" \
EEST_POST_STATE_WAIT_TIMEOUT="${EEST_POST_STATE_WAIT_TIMEOUT}" \
EEST_ORDERED_TX_SUBMISSION="${EEST_ORDERED_TX_SUBMISSION}" \
EEST_POLL_INTERVAL="${EEST_POLL_INTERVAL}" \
EEST_SHARD_COUNT="${EEST_SHARD_COUNT}" \
EEST_SHARD_INDEX="${EEST_SHARD_INDEX}" \
exec "${execute_command[@]}"
