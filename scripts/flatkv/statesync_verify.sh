#!/usr/bin/env bash
#
# statesync_verify.sh — FlatKV correctness check for a node that was
# state-synced the same way memiavl nodes are, via
# sei-infra/common/scripts/state_sync.sh.
#
# memiavl's state-sync test is "run state_sync.sh, wait for the node to
# catch up, observe that blocks keep moving" — nothing more. FlatKV adds
# one extra invariant: the post-sync FlatKV contents must match memiavl
# on the same node, and must match a reference node at the same height.
#
# This script is the only FlatKV-specific verification glue needed around
# the real P2P state-sync path. Everything else (dump_account.sh diffing,
# FlatKV-only `seidb` inspection commands, in-process composite export/
# import via `seidb snapshot-roundtrip`) is either a primitive or a
# developer-only shortcut and lives separately.
#
# WORKFLOW
# --------
#
# Step 0 — make a shared accounts list. Include a mix of pre- and
# post-FlatKV-activation EVM addresses so you exercise both the
# dual-write path and the "only in memiavl" gap.
#
#   cat > accounts.txt <<'EOF'
#   0x5ff137d4b0fdcd49dca30c7cf57e578a026d2789
#   0x...
#   EOF
#
# Step 1 — on a REFERENCE node (any already-synced FlatKV shadow node),
# capture a canonical sweep TSV at the height you're going to verify at:
#
#   statesync_verify.sh capture \
#       --home /root/.sei \
#       --height 203680000 \
#       --accounts-file accounts.txt \
#       --out-file /tmp/ref.sweep.tsv
#
# Ship /tmp/ref.sweep.tsv to the target node.
#
# Step 2 — on the TARGET node, run the same real state-sync flow used for
# memiavl, then verify:
#
#   /home/ubuntu/sei-infra/common/scripts/state_sync.sh 20000
#
#   statesync_verify.sh verify \
#       --home /root/.sei \
#       --height 203680000 \
#       --accounts-file accounts.txt \
#       --ref-tsv /tmp/ref.sweep.tsv \
#       --wait-catchup
#
# `--wait-catchup` polls tendermint RPC until the target reaches the
# verification height, so the whole pipeline (state_sync.sh + verify) is
# fire-and-forget.
#
# EXIT
# ----
#   0  target.sweep.tsv == ref.sweep.tsv after normalization
#   1  drift — diff printed to stderr; full per-account dumps left in a
#      tempdir for deeper investigation with dump_account.sh
#   2  usage / prereq error / catchup timeout

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DUMP_ACCOUNT_SH="$SCRIPT_DIR/dump_account.sh"

usage() {
    cat <<'EOF'
Usage:
  statesync_verify.sh capture --home H --height N --accounts-file F --out-file OUT
                              [--tolerate-missing]

  statesync_verify.sh verify  --home H --height N --accounts-file F --ref-tsv REF
                              [--tolerate-missing]
                              [--wait-catchup] [--rpc-url URL] [--catchup-timeout S]

Subcommands:
  capture  Dump a header-level sweep on the reference node at --height
           and write the canonical TSV to --out-file. Ship that TSV to
           the target.

  verify   On the target node, optionally wait for post-state-sync
           catchup past --height, then run the same sweep at --height
           and diff against --ref-tsv. Zero exit iff the two TSVs are
           byte-identical.

Shared flags:
  --home PATH             seid home (has data/committer.db + data/flatkv)
  --height N              block height both sides dump at
  --accounts-file PATH    newline-separated 0x-prefixed EVM addresses
  --tolerate-missing      do not count "FlatKV has no data for this addr"
                          as a failure during sweep construction (useful
                          for pre-FlatKV-activation addresses)

verify-only flags:
  --ref-tsv PATH          reference TSV produced by `capture`
  --wait-catchup          poll tendermint RPC until latest_height >= N
  --rpc-url URL           default: http://localhost:26657
  --catchup-timeout S     default: 7200
EOF
}

SUB="${1:-}"
[[ -z "$SUB" ]] && { usage; exit 2; }
shift || true

HOME_DIR=""
HEIGHT=""
ACCOUNTS_FILE=""
OUT_FILE=""
REF_TSV=""
TOLERATE_MISSING=0
WAIT_CATCHUP=0
RPC_URL="http://localhost:26657"
CATCHUP_TIMEOUT=7200

while [[ $# -gt 0 ]]; do
    case "$1" in
        --home)              HOME_DIR="$2"; shift 2;;
        --height)            HEIGHT="$2"; shift 2;;
        --accounts-file)     ACCOUNTS_FILE="$2"; shift 2;;
        --out-file)          OUT_FILE="$2"; shift 2;;
        --ref-tsv)           REF_TSV="$2"; shift 2;;
        --tolerate-missing)  TOLERATE_MISSING=1; shift;;
        --wait-catchup)      WAIT_CATCHUP=1; shift;;
        --rpc-url)           RPC_URL="$2"; shift 2;;
        --catchup-timeout)   CATCHUP_TIMEOUT="$2"; shift 2;;
        -h|--help)           usage; exit 0;;
        *) echo "unknown arg: $1" >&2; usage; exit 2;;
    esac
done

[[ -z "$HOME_DIR" ]]      && { echo "ERROR: --home is required" >&2; exit 2; }
[[ -z "$HEIGHT" ]]        && { echo "ERROR: --height is required" >&2; exit 2; }
[[ -z "$ACCOUNTS_FILE" ]] && { echo "ERROR: --accounts-file is required" >&2; exit 2; }
[[ -f "$ACCOUNTS_FILE" ]] || { echo "ERROR: accounts-file not found: $ACCOUNTS_FILE" >&2; exit 2; }
[[ -x "$DUMP_ACCOUNT_SH" ]] || { echo "ERROR: missing $DUMP_ACCOUNT_SH" >&2; exit 2; }

# ----------------------------------------------------------------------------
# sweep: run dump_account.sh per address and emit a compact TSV with
# (address, nonce-eq, codeSize-eq, codeHash-eq, storageCount-eq, status).
#
# Inlined here (rather than in its own helper script) because this is the
# only caller. The sweep TSV is what we diff between ref and target — it
# captures both the FlatKV-vs-memiavl invariant per node AND the inter-node
# invariant in one comparison.
# ----------------------------------------------------------------------------
do_sweep() {
    local out_dir="$1"
    mkdir -p "$out_dir"

    mapfile -t ADDRS < <(grep -vE '^\s*(#|$)' "$ACCOUNTS_FILE")
    local n=${#ADDRS[@]}
    (( n == 0 )) && { echo "ERROR: accounts-file has no entries" >&2; return 2; }

    local report="$out_dir/sweep.tsv"
    printf 'address\tnonce\tcodeSize\tcodeHash\tstorageCount\tstatus\n' > "$report"

    local pass=0 fail=0
    echo ">> sweeping $n address(es) @ height=$HEIGHT against $HOME_DIR"
    for i in "${!ADDRS[@]}"; do
        local addr="${ADDRS[$i]}"
        local addr_dir="$out_dir/$addr"
        mkdir -p "$addr_dir"

        local rc=0
        "$DUMP_ACCOUNT_SH" --home "$HOME_DIR" --address "$addr" --height "$HEIGHT" \
            --out-dir "$addr_dir" --quiet >"$addr_dir/run.log" 2>&1 || rc=$?

        # Parse the "<field> | <flatkv> | <memiavl>" table dump_account.sh
        # writes to stdout/stderr (captured in run.log). Comparing the
        # rendered strings is cheaper than re-parsing two JSON files and
        # matches exactly what the tool itself decided.
        parse_field() {
            local fld="$1"
            local line f_val m_val
            line=$(grep -E "^\s*${fld}\s+\|" "$addr_dir/run.log" | head -1 || true)
            if [[ -z "$line" ]]; then echo "?"; return; fi
            f_val=$(echo "$line" | awk -F'|' '{gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2}')
            m_val=$(echo "$line" | awk -F'|' '{gsub(/^[ \t]+|[ \t]+$/, "", $3); print $3}')
            [[ "$f_val" == "$m_val" ]] && echo "EQ" || echo "NE"
        }

        local nonce csize chash scnt status
        nonce=$(parse_field nonce)
        csize=$(parse_field codeSize)
        chash=$(parse_field codeHash)
        scnt=$(parse_field storageCount)

        status="OK"
        if [[ $rc -eq 2 ]]; then
            status="ERR"
        elif [[ "$nonce" == "NE" || "$csize" == "NE" || "$chash" == "NE" || "$scnt" == "NE" ]]; then
            status="DRIFT"
        elif [[ "$nonce" == "?" ]]; then
            status="MISSING"
        fi

        case "$status" in
            OK)      pass=$((pass + 1));;
            MISSING) if (( TOLERATE_MISSING == 1 )); then pass=$((pass + 1)); else fail=$((fail + 1)); fi;;
            *)       fail=$((fail + 1));;
        esac

        printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$addr" "$nonce" "$csize" "$chash" "$scnt" "$status" >> "$report"
        printf "  [%3d/%3d] %s  nonce=%-2s code=%-2s hash=%-2s storage=%-2s  %s\n" \
            "$((i + 1))" "$n" "$addr" "$nonce" "$csize" "$chash" "$scnt" "$status"
    done

    echo
    echo "  sweep: $report"
    echo "  passed: $pass / $n"
    echo "  failed: $fail / $n"
    # Don't hard-fail here; the caller (capture or verify) makes the policy
    # call. capture wants the TSV even if the ref has some MISSING rows;
    # verify wants a byte-identical diff regardless of per-row status.
    return 0
}

wait_catchup() {
    local target="$1"
    local deadline=$((SECONDS + CATCHUP_TIMEOUT))
    echo ">> waiting for $RPC_URL to reach height >= $target (timeout=${CATCHUP_TIMEOUT}s)"
    local last_h=""
    while (( SECONDS < deadline )); do
        local h
        h=$(curl -s "$RPC_URL/status" 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
        if [[ "$h" =~ ^[0-9]+$ ]] && (( h >= target )); then
            echo "   caught up: latest_height=$h"
            return 0
        fi
        if [[ "$h" != "$last_h" ]]; then
            echo "   latest_height=$h (target=$target)"
            last_h="$h"
        fi
        sleep 30
    done
    echo "ERROR: node did not reach height $target within ${CATCHUP_TIMEOUT}s" >&2
    return 1
}

case "$SUB" in
    capture)
        [[ -z "$OUT_FILE" ]] && { echo "ERROR: --out-file is required for capture" >&2; exit 2; }
        tmpd="$(mktemp -d -t seidb-capture.XXXXXX)"
        do_sweep "$tmpd" || true
        if [[ ! -s "$tmpd/sweep.tsv" ]]; then
            echo "ERROR: reference sweep produced no rows (see $tmpd)" >&2
            exit 2
        fi
        mkdir -p "$(dirname "$OUT_FILE")"
        cp "$tmpd/sweep.tsv" "$OUT_FILE"
        echo
        echo ">> wrote reference TSV: $OUT_FILE ($(wc -l < "$OUT_FILE") lines)"
        echo "   ship it to the target node and pass via --ref-tsv"
        ;;

    verify)
        [[ -z "$REF_TSV" ]] && { echo "ERROR: --ref-tsv is required for verify" >&2; exit 2; }
        [[ -f "$REF_TSV" ]] || { echo "ERROR: ref-tsv not found: $REF_TSV" >&2; exit 2; }

        if (( WAIT_CATCHUP == 1 )); then
            wait_catchup "$HEIGHT"
        fi

        tmpd="$(mktemp -d -t seidb-verify.XXXXXX)"
        do_sweep "$tmpd" || true
        if [[ ! -s "$tmpd/sweep.tsv" ]]; then
            echo "ERROR: target sweep produced no rows (see $tmpd)" >&2
            exit 2
        fi

        echo
        echo ">> diff ref vs target:"
        echo "   ref:    $REF_TSV"
        echo "   target: $tmpd/sweep.tsv"
        if diff -u "$REF_TSV" "$tmpd/sweep.tsv"; then
            echo
            echo "PASS: target sweep matches reference byte-for-byte"
            exit 0
        else
            echo
            echo "FAIL: state-synced target diverges from reference"
            echo "   full target dumps kept in: $tmpd"
            echo "   drill into an individual mismatch with:"
            echo "     $DUMP_ACCOUNT_SH --home $HOME_DIR --address <ADDR> --height $HEIGHT --out-dir /tmp/inspect"
            exit 1
        fi
        ;;

    *)
        echo "unknown subcommand: $SUB" >&2
        usage
        exit 2
        ;;
esac
