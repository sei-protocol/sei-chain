#!/bin/bash
#
# Tx side-effect wait helpers for the deploy bash scripts. Submit cosmos
# txs with -b sync, then poll the actual on-chain side effect rather
# than relying on -b block: under Autobahn the KV indexer that
# BroadcastTxCommit polls isn't populated, so -b block can hang to its
# 60s timeout. Mirrors the lib.js helpers introduced in #3406
# (storeWasm, instantiateWasm, getAccountSequence) ported to bash.
#
# Source from a deploy script after setting $seidbin, $keyname, $chainid.

: "${TX_WAIT_TIMEOUT:=30}"
: "${TX_WAIT_INTERVAL:=0.5}"

# Cosmos account sequence for an address; 0 if the account doesn't exist
# yet. Causal "tx committed" signal: a sender's sequence advances
# atomically when its tx is included in a block, regardless of whether
# the tx's intended side effect happened.
_get_account_sequence() {
    $seidbin q account "$1" -o json 2>/dev/null | jq -r '.sequence // 0'
}

# Poll an arbitrary check until it exits 0. Mirrors lib.js's
# waitForCondition.
# Usage: _wait_until <description> <check_cmd>
_wait_until() {
    local description="$1"
    local check_cmd="$2"
    local deadline=$(($(date +%s) + TX_WAIT_TIMEOUT))
    while [ "$(date +%s)" -lt "$deadline" ]; do
        if eval "$check_cmd" >/dev/null 2>&1; then return 0; fi
        sleep "$TX_WAIT_INTERVAL"
    done
    echo "timed out waiting for $description within ${TX_WAIT_TIMEOUT}s" >&2
    return 1
}

# Submit `bank send` via -b sync and echo the chain height observed
# when the sender's account sequence advances — the post-commit height
# from the test's perspective. Use when callers need a height for
# subsequent state-at-height queries (e.g., historical balance lookups).
# Usage: height=$(bank_send_and_get_height <from-key> <to-addr> <amount-with-denom>) || exit 1
bank_send_and_get_height() {
    local from_key="$1"
    local to_addr="$2"
    local amount_denom="$3"
    local from_addr; from_addr=$(printf "12345678\n" | $seidbin keys show "$from_key" -a 2>/dev/null)
    local seq_before; seq_before=$(_get_account_sequence "$from_addr")
    local resp; resp=$(printf "12345678\n" | $seidbin tx bank send "$from_key" "$to_addr" "$amount_denom" \
        -y --chain-id="$chainid" --gas=5000000 --fees=1000000usei \
        --broadcast-mode=sync --output=json)
    local code; code=$(echo "$resp" | jq -r '.code // 0')
    if [ "$code" != "0" ]; then
        echo "bank_send_and_get_height CheckTx rejected: $(echo "$resp" | jq -r '.raw_log')" >&2
        return 1
    fi
    _wait_until "$from_addr sequence > $seq_before" \
        "[ \$(_get_account_sequence $from_addr) -gt $seq_before ]" || return 1
    $seidbin status | jq -r ".SyncInfo.latest_block_height"
}

# Poll until the chain's latest height exceeds the given height. Useful
# as a barrier between two sends from the same key when callers need
# them to land in distinct blocks (e.g., historical balance queries).
# Usage: wait_until_height_exceeds <min-height>
wait_until_height_exceeds() {
    local min_height="$1"
    _wait_until "chain height > $min_height" \
        "[ \$($seidbin status | jq -r .SyncInfo.latest_block_height) -gt $min_height ]"
}
