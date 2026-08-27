#!/bin/bash
#
# Probe the module surfaces the v6.7 upgrade retires and print a fingerprint of
# how the chain answered each one. The caller runs this on both sides of the
# upgrade and compares the two fingerprints: retiring a module changes what a
# node stores, never how it answers a client that has not noticed yet. A
# fingerprint that shifts at the upgrade height means two nodes mid-rollout
# would disagree about a block.
#
# Each probe reports a fixed token rather than raw output, so that incidental
# differences between two runs (transaction hashes, gas figures, heights) cannot
# make equivalent answers look different. A probe that did not get the expected
# answer reports the output it did get, which both fails the run and says why.
#
# The last line is PROBE_RESULT, a single verdict over all probes, so a caller
# can assert the whole run with one match.
#
# The chain identity and signing key are read from the environment so that one
# probe definition serves both the four-node docker cluster and a single-node
# local chain. Defaults are the docker cluster's, so the YAML suite that calls
# this needs no arguments.
#
#   CHAIN_ID    chain id to sign against          (default sei)
#   ADMIN_KEY   key name to sign and query with   (default node_admin)
#
# Usage: probe_retired_modules.sh <distinct-fee-granter-addr> [spam_count]

set -uo pipefail

FEE_GRANTER=${1:-}
SPAM_COUNT=${2:-5}

if [ -z "$FEE_GRANTER" ]; then
    echo "Usage: $0 <distinct-fee-granter-addr> [spam_count]" >&2
    exit 1
fi

seidbin=seid
chainid=${CHAIN_ID:-sei}
adminkey=${ADMIN_KEY:-node_admin}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../utils/_tx_helpers.sh
source "$SCRIPT_DIR/../../utils/_tx_helpers.sh"

ADMIN_ADDR=$(_get_key_address "$adminkey")
ADMIN_VAL_ADDR=$(printf "12345678\n" | $seidbin keys show "$adminkey" --bech val -a 2>/dev/null)

if [ -z "$ADMIN_ADDR" ] || [ -z "$ADMIN_VAL_ADDR" ]; then
    echo "could not resolve key '$adminkey'; set ADMIN_KEY to a key in this node's keyring" >&2
    exit 1
fi

MISSED=""

# report prints one fingerprint line and records whether the probe matched.
report() {
    echo "$1=$2"
    case "$2" in MATCH) ;; *) MISSED="$MISSED $1" ;; esac
}

# token collapses an output to MATCH when it carries the expected text, or to a
# truncated single-line echo of what came back instead.
token() {
    case "$1" in
        *"$2"*) echo "MATCH" ;;
        *) echo "MISS[$(echo "$1" | tr '\n' ' ' | cut -c1-160)]" ;;
    esac
}

# Retired oracle message handlers, reached through simulation. --dry-run does
# not open the keyring, so --from has to be the address rather than the key name.
report ORACLE_VOTE_SIM "$(token "$(printf '12345678\n' | $seidbin tx oracle aggregate-vote \
    1.5ueth "$ADMIN_VAL_ADDR" --from "$ADMIN_ADDR" --chain-id "$chainid" --dry-run 2>&1)" \
    'oracle module is deprecated')"
report ORACLE_FEEDER_SIM "$(token "$(printf '12345678\n' | $seidbin tx oracle set-feeder \
    "$ADMIN_ADDR" --from "$ADMIN_ADDR" --chain-id "$chainid" --dry-run 2>&1)" \
    'oracle module is deprecated')"

# Retired oracle query handlers.
report ORACLE_PARAMS_QUERY "$(token "$($seidbin q oracle params --output json 2>&1)" \
    'oracle module is deprecated')"
report ORACLE_TWAPS_QUERY "$(token "$($seidbin q oracle twaps 3600 --output json 2>&1)" \
    'oracle module is deprecated')"

# A transaction nominating a fee granter other than its signer. The feegrant
# module is gone, so the chain refuses it, and refuses it in ValidateBasic
# before any fee is taken.
report FEE_GRANTER_DISTINCT "$(token "$(printf '12345678\n' | $seidbin tx bank send \
    "$adminkey" "$ADMIN_ADDR" 1usei --fee-account "$FEE_GRANTER" --from "$adminkey" \
    --chain-id "$chainid" --fees 2000usei -b sync -y --output json 2>&1)" \
    'fee grants are not enabled')"

# The granter field itself stays on the wire: a transaction naming its own
# signer as granter is still accepted, which is what keeps older clients
# working. CheckTx code 0 is the accepted answer.
report FEE_GRANTER_SELF "$(token "$(printf '12345678\n' | $seidbin tx bank send "$adminkey" \
    "$ADMIN_ADDR" 1usei --fee-account "$ADMIN_ADDR" --from "$adminkey" --chain-id "$chainid" \
    --fees 2000usei -b sync -y --output json 2>&1 | jq -r '.code // "unparsable"')" '0')"

# Spam the retired oracle handler with real broadcasts. Each one is rejected but
# still committed, so the sender's sequence advances and its balance drops: a
# retired handler must not become a free way to fill blocks.
balance_before=$($seidbin q bank balances "$ADMIN_ADDR" --denom usei --output json 2>/dev/null | jq -r '.amount // 0')
seq_before=$(_get_account_sequence "$ADMIN_ADDR")

for ((i = 0; i < SPAM_COUNT; i++)); do
    printf '12345678\n' | $seidbin tx oracle aggregate-vote "1.${i}ueth" "$ADMIN_VAL_ADDR" \
        --from "$adminkey" --chain-id "$chainid" --fees 2000usei -b sync -y --output json >/dev/null 2>&1
    _wait_until "$adminkey sequence > $((seq_before + i))" \
        "[ \$(_get_account_sequence $ADMIN_ADDR) -gt $((seq_before + i)) ]" >/dev/null 2>&1
done

seq_after=$(_get_account_sequence "$ADMIN_ADDR")
balance_after=$($seidbin q bank balances "$ADMIN_ADDR" --denom usei --output json 2>/dev/null | jq -r '.amount // 0')

if [ "$seq_after" -gt "$seq_before" ]; then
    report ORACLE_SPAM_COMMITTED "MATCH"
else
    report ORACLE_SPAM_COMMITTED "MISS[sequence stayed at $seq_before]"
fi

if [ "$balance_after" -lt "$balance_before" ]; then
    report ORACLE_SPAM_CHARGED "MATCH"
else
    report ORACLE_SPAM_CHARGED "MISS[balance $balance_before to $balance_after]"
fi

if [ -z "$MISSED" ]; then
    echo "PROBE_RESULT=ALL_MATCH"
else
    echo "PROBE_RESULT=MISSED[$(echo "$MISSED" | xargs)]"
fi
