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

_get_key_address() {
    printf "12345678\n" | $seidbin keys show "$1" -a 2>/dev/null
}

# Cosmos account sequence for an address at a historical height. Echoes
# the sequence number (0 if the account didn't exist yet at <height>),
# or empty on query failure — distinct from 0 so the caller can retry
# instead of misreading a transient RPC error as "pre-tx sequence".
_get_account_sequence_at_height() {
    local raw; raw=$($seidbin q account "$1" --height "$2" -o json 2>/dev/null)
    [ -z "$raw" ] && return
    echo "$raw" | jq -r '.sequence // 0'
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

# Submit `bank send` via -b sync and echo the exact block height at
# which the tx committed. Useful when callers need the inclusion
# height for state-at-height queries (e.g., historical balance lookups
# at height-1 vs height to validate per-block granularity). Some
# callers intentionally self-send a dust amount purely to force the
# chain to produce a real block under allow_empty_blocks=false.
# Implementation: after the sender's sequence advances, walk back from
# the observed height querying historical sequence — the largest H
# where sequence(H) is still pre-tx is one below the inclusion height.
# Usage: height=$(bank_send_and_get_height <from-key> <to-addr> <amount-with-denom>) || exit 1
bank_send_and_get_height() {
    local from_key="$1"
    local to_addr="$2"
    local amount_denom="$3"
    local from_addr; from_addr=$(_get_key_address "$from_key")
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
    local h_obs; h_obs=$($seidbin status | jq -r ".SyncInfo.latest_block_height")
    local h="$h_obs"
    local empty_retries=0
    while [ "$h" -gt 0 ]; do
        local s; s=$(_get_account_sequence_at_height "$from_addr" "$h")
        if [ -z "$s" ]; then
            empty_retries=$((empty_retries + 1))
            if [ "$empty_retries" -ge 10 ]; then
                echo "bank_send_and_get_height: 10 consecutive empty reads at h=$h, falling back to $h_obs" >&2
                echo "$h_obs"
                return 0
            fi
            sleep "$TX_WAIT_INTERVAL"
            continue
        fi
        empty_retries=0
        if [ "$s" -le "$seq_before" ]; then
            # First iteration cannot legitimately see pre-tx sequence:
            # _wait_until just confirmed the live sequence advanced past
            # seq_before, and h_obs is the latest height. A pre-tx read
            # here means the historical query is racing the indexer;
            # fall back to h_obs (still >= the true inclusion height).
            if [ "$h" = "$h_obs" ]; then echo "$h_obs"; return 0; fi
            echo $((h + 1))
            return 0
        fi
        h=$((h - 1))
    done
    echo "$h_obs"
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

get_proposal_status() {
    $seidbin q gov proposal "$1" --output json 2>/dev/null | jq -r '.status // ""'
}

wait_for_proposal_status() {
    local proposal_id="$1"
    local target_status="$2"
    local from_key="${3:-admin}"
    local timeout_secs="${4:-120}"
    local from_addr; from_addr=$(_get_key_address "$from_key")
    local deadline=$(($(date +%s) + timeout_secs))
    local status=""
    while [ "$(date +%s)" -lt "$deadline" ]; do
        local raw; raw=$($seidbin q gov proposal "$proposal_id" --output json 2>/dev/null || true)
        status=$(echo "$raw" | jq -r '.status // ""' 2>/dev/null)
        if [ "$status" = "$target_status" ]; then
            echo "$status"
            return 0
        fi
        if [ "$status" = "PROPOSAL_STATUS_REJECTED" ] || [ "$status" = "PROPOSAL_STATUS_FAILED" ]; then
            echo "proposal $proposal_id reached terminal status $status while waiting for $target_status" >&2
            return 1
        fi
        local voting_end
        voting_end=$(echo "$raw" | jq -r '.voting_end_time // ""' 2>/dev/null)
        local voting_end_epoch=""
        if [ -n "$voting_end" ]; then
            voting_end_epoch=$(date -d "$voting_end" +%s 2>/dev/null || true)
        fi
        if [ -z "$voting_end_epoch" ]; then
            echo "proposal $proposal_id missing valid voting_end_time while waiting for $target_status" >&2
            return 1
        fi
        if [ "$(date +%s)" -ge $((voting_end_epoch + 1)) ]; then
            # Progress-only self-send: once the currently observed voting end
            # time has passed, force one committed block so tallying can
            # advance. Expedited proposals can convert to regular and extend
            # voting_end_time, so re-read proposal state after every kick.
            bank_send_and_wait "$from_key" "$from_addr" "1usei" >/dev/null || return 1
        fi
        sleep 1
    done
    echo "timed out waiting for proposal $proposal_id to reach $target_status (last status=${status:-unknown})" >&2
    return 1
}

# Submit `tx <subcmd> <args...>` via -b sync from <from-key>, wait for
# that sender's account sequence to advance, and echo the CheckTx code
# (0 on success). On CheckTx rejection the rejection log is written to
# stderr and the non-zero code is echoed without waiting, so callers
# whose verifiers inspect the code see the rejection instead of the
# test hanging on a sequence advance that will never happen.
# Usage: code=$(submit_tx_and_wait <from-key> <subcmd-and-args...>)
submit_tx_and_wait() {
    local from_key="$1"; shift
    local from_addr; from_addr=$(_get_key_address "$from_key")
    local seq_before; seq_before=$(_get_account_sequence "$from_addr")
    local resp; resp=$(printf "12345678\n" | $seidbin tx "$@" --from "$from_key" \
        -y --chain-id="$chainid" --broadcast-mode=sync --output=json)
    local code; code=$(echo "$resp" | jq -r '.code // 0')
    if [ "$code" != "0" ]; then
        echo "submit_tx_and_wait CheckTx rejected: $(echo "$resp" | jq -r '.raw_log')" >&2
        echo "$code"
        return 0
    fi
    _wait_until "$from_addr sequence > $seq_before" \
        "[ \$(_get_account_sequence $from_addr) -gt $seq_before ]" >/dev/null || return 1
    echo "$code"
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
    local from_addr; from_addr=$(_get_key_address "$from_key")
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

# Highest existing gov proposal id (0 if none / on transient query
# failure). `seid q gov proposals` exits non-zero with "no proposals
# found" on an empty gov set, in which case the jq pipeline reads
# empty stdin and emits nothing — explicit fallback to 0 keeps callers
# from hitting "integer expression expected" on empty $().
_get_max_proposal_id() {
    local out; out=$($seidbin q gov proposals --reverse --limit 1 -o json 2>/dev/null)
    if [ -z "$out" ]; then echo 0; return; fi
    local id; id=$(echo "$out" | jq -r '.proposals[0].proposal_id // .proposals[0].id // 0' 2>/dev/null)
    echo "${id:-0}"
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
            local query_failed=0
            for ((id=max_id_before+1; id<=cur; id++)); do
                local raw; raw=$($seidbin q gov proposal "$id" -o json 2>/dev/null)
                if [ -z "$raw" ]; then
                    # Transient query failure (RPC blip, indexer lag,
                    # etc.) — leave the id for re-scan on the next
                    # iteration rather than treating empty-response as
                    # "wrong title" and skipping past it.
                    query_failed=1
                    continue
                fi
                local observed; observed=$(echo "$raw" | jq -r '.content.title // .title // ""')
                if [ "$observed" = "$title" ]; then echo "$id"; return 0; fi
            done
            # Only advance the window if every id in the range was
            # successfully scanned. A transient miss leaves max_id_before
            # unchanged so the failed id is re-checked next iteration —
            # without this, the proposal we just submitted could be
            # permanently skipped by a single failed query.
            if [ "$query_failed" = 0 ]; then
                max_id_before=$cur
            fi
        fi
        sleep "$TX_WAIT_INTERVAL"
    done
    echo "find_proposal_by_title: proposal (tx $tx_hash) with title '$title' did not appear in gov state within ${TX_WAIT_TIMEOUT}s" >&2
    return 1
}

# Submit a gov proposal via -b sync and echo the new proposal id once
# it appears in gov state. Bundles _get_max_proposal_id, the submit,
# and find_proposal_by_title so yaml tests can capture PROPOSAL_ID in
# a single cmd: without parsing DeliverTx event logs.
# Usage: id=$(submit_gov_proposal <from-key> <title> <subcmd-and-args...>)
submit_gov_proposal() {
    local from_key="$1"; shift
    local title="$1"; shift
    local max_before; max_before=$(_get_max_proposal_id)
    local resp; resp=$(printf "12345678\n" | $seidbin tx "$@" --from "$from_key" \
        -y --chain-id="$chainid" --broadcast-mode=sync --output=json)
    local code; code=$(echo "$resp" | jq -r '.code // 0')
    if [ "$code" != "0" ]; then
        echo "submit_gov_proposal CheckTx rejected: $(echo "$resp" | jq -r '.raw_log')" >&2
        return 1
    fi
    local txhash; txhash=$(echo "$resp" | jq -r '.txhash')
    find_proposal_by_title "$title" "$max_before" "$txhash"
}
