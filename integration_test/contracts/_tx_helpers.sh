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

# Highest existing wasm code_id (0 if none). Side-effect signal for
# store_wasm: after a successful store, the max grows by one.
_get_max_wasm_code_id() {
    $seidbin q wasm list-code --reverse --limit 1 -o json 2>/dev/null \
        | jq -r '.code_infos[0].code_id // 0'
}

# Contracts instantiated under a given code_id, one address per line,
# sorted. Side-effect signal for instantiate_wasm.
_list_contracts_by_code() {
    $seidbin q wasm list-contract-by-code "$1" -o json 2>/dev/null \
        | jq -r '.contracts[]?' | sort -u
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

# Submit `wasm store` via -b sync and echo the new code_id after the
# chain reflects it. Mirrors lib.js's storeWasm.
# Usage: code_id=$(store_wasm <path/to.wasm>) || exit 1
store_wasm() {
    local wasm_path="$1"
    local before; before=$(_get_max_wasm_code_id)
    local resp; resp=$(printf "12345678\n" | $seidbin tx wasm store "$wasm_path" \
        -y --from="$keyname" --chain-id="$chainid" \
        --gas=5000000 --fees=1000000usei \
        --broadcast-mode=sync --output=json)
    local code; code=$(echo "$resp" | jq -r '.code // 0')
    if [ "$code" != "0" ]; then
        echo "store_wasm CheckTx rejected: $(echo "$resp" | jq -r '.raw_log')" >&2
        return 1
    fi
    _wait_until "wasm code_id > $before" "[ \$(_get_max_wasm_code_id) -gt $before ]" || return 1
    _get_max_wasm_code_id
}

# Submit `wasm instantiate` via -b sync and echo the new contract
# address after it appears under code_id. Mirrors lib.js's
# instantiateWasm. Extra flags pass through as positional args.
# Usage: addr=$(instantiate_wasm <code_id> <init_msg> <label> [extra flags...]) || exit 1
instantiate_wasm() {
    local code_id="$1"; shift
    local init_msg="$1"; shift
    local label="$1"; shift
    local before; before=$(_list_contracts_by_code "$code_id")
    local resp; resp=$(printf "12345678\n" | $seidbin tx wasm instantiate "$code_id" "$init_msg" \
        --label="$label" -y --from="$keyname" --chain-id="$chainid" \
        --gas=5000000 --fees=1000000usei \
        --broadcast-mode=sync --output=json "$@")
    local code; code=$(echo "$resp" | jq -r '.code // 0')
    if [ "$code" != "0" ]; then
        echo "instantiate_wasm CheckTx rejected: $(echo "$resp" | jq -r '.raw_log')" >&2
        return 1
    fi
    _wait_until "new contract under code $code_id" "
        new=\$(comm -13 <(echo \"$before\") <(_list_contracts_by_code \"$code_id\") | head -1)
        [ -n \"\$new\" ]
    " || return 1
    comm -13 <(echo "$before") <(_list_contracts_by_code "$code_id") | head -1
}

# Submit `bank send` via -b sync and wait for the sender's account
# sequence to advance — a denom-agnostic causal "tx committed" signal.
# Use between consecutive sends from the same key so the next CLI
# invocation reads a fresh sequence rather than racing the prior commit.
# Usage: bank_send_and_wait <from-key> <to-addr> <amount-with-denom>
bank_send_and_wait() {
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
        echo "bank_send_and_wait CheckTx rejected: $(echo "$resp" | jq -r '.raw_log')" >&2
        return 1
    fi
    _wait_until "$from_addr sequence > $seq_before" \
        "[ \$(_get_account_sequence $from_addr) -gt $seq_before ]" || return 1
}

# Highest existing gov proposal id (0 if none).
_get_max_proposal_id() {
    $seidbin q gov proposals --reverse --limit 1 -o json 2>/dev/null \
        | jq -r '.proposals[0].proposal_id // .proposals[0].id // 0'
}

# After a -b sync gov proposal submission, scan gov state for the new
# proposal whose title matches and echo its id. The diff against
# max_id_before pins it to *this* submission rather than a stale
# prior-run proposal with the same title. Mirrors lib.js's
# findProposalByTitle.
# Usage: find_proposal_by_title <title> <max_id_before> [<tx_hash_for_error>]
find_proposal_by_title() {
    local title="$1"
    local max_id_before="$2"
    local tx_hash="${3:-unknown}"
    local deadline=$(($(date +%s) + TX_WAIT_TIMEOUT))
    while [ "$(date +%s)" -lt "$deadline" ]; do
        local cur; cur=$(_get_max_proposal_id)
        if [ "$cur" -gt "$max_id_before" ]; then
            local id
            for ((id=max_id_before+1; id<=cur; id++)); do
                local observed; observed=$($seidbin q gov proposal "$id" -o json 2>/dev/null \
                    | jq -r '.content.title // .title // ""')
                if [ "$observed" = "$title" ]; then echo "$id"; return 0; fi
            done
            # Use max so a transient query failure can't shrink the
            # window and let a prior-run proposal with the same title
            # re-match on the next iteration.
            max_id_before=$cur
        fi
        sleep "$TX_WAIT_INTERVAL"
    done
    echo "find_proposal_by_title: proposal (tx $tx_hash) with title '$title' did not appear in gov state within ${TX_WAIT_TIMEOUT}s" >&2
    return 1
}
