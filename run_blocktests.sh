#!/bin/bash
# Runs the EEST Osaka blockchain fixtures through `seid blocktest`.
#
# usage: run_blocktests.sh <fixtures/blockchain_tests/for_osaka> [runner_index runner_total]
# LIST_ONLY=1 prints the selected cases without running them.

set -e

fixtures_dir=$1
runner_index=${2:-0}
runner_total=${3:-1}
case_timeout=${CASE_TIMEOUT:-600}

if [ -z "$fixtures_dir" ] || [ ! -d "$fixtures_dir" ]; then
    echo "usage: $0 <fixtures/blockchain_tests/for_osaka> [runner_index runner_total]" >&2
    exit 2
fi

# Fixture directories not run, relative to the fixtures dir.
declare -a skip_dirs=(
    # Blobs (EIP-4844 family) are not supported
    "cancun/eip4844_blobs/"
    "cancun/eip7516_blobgasfee/"
    "ported_static/stEIP4844_blobtransactions/"
    "osaka/eip7594_peerdas/"
    "osaka/eip7918_blob_reserve_price/"

    # Beacon-chain system contracts and requests are not supported
    "cancun/eip4788_beacon_root/"
    "prague/eip6110_deposits/"
    "prague/eip7002_el_triggerable_withdrawals/"
    "prague/eip7251_consolidations/"
    "prague/eip7685_general_purpose_el_requests/"

    # Withdrawals are not supported
    "shanghai/eip4895_withdrawals/"
    "mpt/withdrawals_trie_structure/"

    # Block size is set by consensus
    "osaka/eip7934_block_rlp_limit/"

    # Forks after Osaka
    "amsterdam/"

    # Disabled: EIP-2935 is planned for Sei; enable when it ships
    "prague/eip2935_historical_block_hashes_from_state/"
)

# Test ids not run, matched as substrings.
declare -a skip_tests=(
    # SeiAmsterdam: EIP-7954 raises the initcode limit to 128 KiB
    "initcode_name_over_limit_"
    "init_code_size_limit[fork_Osaka-blockchain_test_from_state_test-invalid]"
    "initcode_outcome_code-too-big-"
    "test_random_statetest646["

    # SeiAmsterdam: EIP-8246 changes SELFDESTRUCT
    "test_tx_selfdestruct_balance_bug["

    # The block gas limit is a consensus parameter, not the header gas limit
    "frontier/validation/test_transaction.py::test_tx_gas_limit["
    "test_tx_gas_allowance_after_larger_tx["
    "test_tx_gas_larger_than_block_gas_limit["
    "eip7623_increase_calldata_cost/test_block_gas_limit.py::"

    # Invalid txs are dropped one at a time; the rest of the block still runs
    "test_invalid_transaction_after_authorization["
    "stEIP1559/test_low_gas_limit.py::test_low_gas_limit[fork_Osaka-blockchain_test_from_state_test--g0]"

    # The only block holds a set-code tx with no recipient, which does not decode
    "eip7702_set_code_tx/test_set_code_txs.py::test_contract_create["

    # Failing, also under plain Osaka rules
    "test_multi_selfdestruct[fork_Osaka-blockchain_test_from_state_test-d4]"

    # Pass, but take 7-9 minutes each
    "stQuadraticComplexityTest/test_return50000.py::"
    "stQuadraticComplexityTest/test_return50000_2.py::"
    "stStaticCall/test_static_return50000_2.py::"
)

work_dir=$(mktemp -d)
cleanup() {
    rm -rf "$work_dir"
}
trap cleanup EXIT

# list_cases prints "<file>\t<test id>" for every selected case. Cases with
# blob txs or with pre-state accounts in the Sei precompile range
# (0x1001-0x10ff) are left out.
list_cases() {
    local dir_pattern=$work_dir/skip_dirs
    local test_pattern=$work_dir/skip_tests
    printf '%s\n' "${skip_dirs[@]}" | sed "s|^|$fixtures_dir/|" > "$dir_pattern"
    printf '%s\n' "${skip_tests[@]}" > "$test_pattern"
    find "$fixtures_dir" -name '*.json' | sort | grep -v -F -f "$dir_pattern" | while read -r file; do
        jq -r --arg file "$file" '
            to_entries[]
            | select(any(.value.pre | keys[];
                (ascii_downcase | ltrimstr("0x")) as $a
                | $a >= "0000000000000000000000000000000000001001"
                  and $a <= "00000000000000000000000000000000000010ff") | not)
            | select(any(.value.blocks[]?.transactions[]?; .type == "0x03") | not)
            | [$file, .key] | @tsv' "$file"
    done | grep -v -F -f "$test_pattern"
}

# Wrap gentx so the local chain init binds to loopback.
seid_wrapper_dir=$work_dir/bin
mkdir -p "$seid_wrapper_dir"
cat > "$seid_wrapper_dir/seid" <<'EOF'
#!/bin/bash
target_seid="$HOME/go/bin/seid"
cmd="$1"
if [ "$cmd" = "gentx" ]; then
    shift
    has_ip=false
    for arg in "$@"; do
        if [ "$arg" = "--ip" ]; then
            has_ip=true
            break
        fi
    done
    if [ "$has_ip" = false ]; then
        set -- "$@" --ip 127.0.0.1
    fi
    exec "$target_seid" gentx "$@"
else
    shift
    exec "$target_seid" "$cmd" "$@"
fi
EOF
chmod +x "$seid_wrapper_dir/seid"
export PATH="$seid_wrapper_dir:$PATH"

if [ -n "$LIST_ONLY" ]; then
    list_cases
    exit 0
fi

list_cases | awk -v idx="$runner_index" -v total="$runner_total" '(NR - 1) % total == idx' > "$work_dir/cases.tsv"
echo "running $(wc -l < "$work_dir/cases.tsv" | tr -d ' ') cases on runner $runner_index of $runner_total"

# Initialize one chain home and copy it for each case.
rm -rf ~/.sei || true
NO_RUN=1 ./scripts/initialize_local_chain.sh
template_home=$work_dir/template
mkdir -p "$template_home"
cp -R ~/.sei "$template_home/.sei"

export SEID_BIN="$HOME/go/bin/seid"
export TEMPLATE_HOME="$template_home"
export FAILED_LIST="$work_dir/failed"
export CASE_TIMEOUT="$case_timeout"
touch "$FAILED_LIST"

run_case() {
    local file=$1 test_id=$2
    local home
    home=$(mktemp -d)
    cp -R "$TEMPLATE_HOME/.sei" "$home/.sei"
    local timeout_cmd=""
    if command -v timeout > /dev/null; then
        timeout_cmd="timeout $CASE_TIMEOUT"
    fi
    if HOME="$home" $timeout_cmd "$SEID_BIN" blocktest --home "$home/.sei" --block-test "$file" --test-name "$test_id" > "$home/log" 2>&1; then
        echo "PASS $test_id"
    else
        echo "FAIL $test_id ($file)"
        grep -m1 '^panic:' "$home/log" || tail -5 "$home/log"
        echo "$test_id" >> "$FAILED_LIST"
    fi
    rm -rf "$home"
}
export -f run_case

jobs=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)
tr '\t\n' '\0\0' < "$work_dir/cases.tsv" | xargs -0 -n 2 -P "$jobs" bash -c 'run_case "$1" "$2"' _ || true

failed=$(wc -l < "$FAILED_LIST" | tr -d ' ')
if [ "$failed" -ne 0 ]; then
    echo "$failed failed:"
    cat "$FAILED_LIST"
    exit 1
fi
echo "all cases passed"
