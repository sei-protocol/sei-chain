#!/bin/bash

set -e

# mode options are list, run, or all
block_tests_path=$1
runner_index=$2
runner_total=$3

if [ -z "$runner_index" ]; then
    runner_index=0
    runner_total=1
fi

echo $mode
echo $block_tests_path

# Define an array of test directories to run
declare -a test_path_run_list=(
    # run all valid block tests
    "ValidBlocks/"

    # run only certain invalid block tests
    "InvalidBlocks/bcEIP1559/"
    "InvalidBlocks/bcStateTests/"
)

# Define an array of tests to skip
declare -a test_name_skip_list=(
    # valid block tests
    "DelegateCallSpam" # passes, but takes super long
    "blockhashTests" # failing
    "blockhashNonConstArg" # failing
    "BLOCKHASH_Bounds" # failing
    "logRevert" # uses an invalid opcode (0xBA)
    "blockWithAllTransactionTypes" # recently started failing
    "tipInsideBlock" # failing after turning on eip-1559 and not burning base fee
    "multimpleBalanceInstruction" # failing after turning on eip-1559 and not burning base fee
    "tips" # failing after turning on eip-1559 and not burning base fee
    "burnVerify" # failing after turning on eip-1559 and not burning base fee
    "emptyPostTransfer" # failing after turning on eip-1559 and not burning base fee
    "highDemand" # failing after increasing SSTORE gas limit to 72k
    "randomStatetest319BC" # failing after increasing SSTORE gas limit to 72k
    "refundReset" # failing after increasing SSTORE gas limit to 72k
    "wallet2outOf3txs" # failing after increasing SSTORE gas limit to 72k
    "wallet2outOf3txs2" # failing after increasing SSTORE gas limit to 72k
    "wallet2outOf3txsRevoke" # failing after increasing SSTORE gas limit to 72k
    "wallet2outOf3txsRevokeAndConfirmAgain" # failing after increasing SSTORE gas limit to 72k
    "walletReorganizeOwners" # failing after increasing SSTORE gas limit to 72k
    "eip2930" # failing after increasing SSTORE gas limit to 72k

    # invalid block tests - state tests
    "gasLimitTooHigh" # block header gas limit doesn't apply to us
    "transactionFromSelfDestructedContract" # failing
    "callcodeOutput2" # failing after increasing SSTORE gas limit to 72k
    "createNameRegistratorPerTxsNotEnoughGasAfter" # failing after increasing SSTORE gas limit to 72k
    "createNameRegistratorPerTxsNotEnoughGasAt" # failing after increasing SSTORE gas limit to 72k
    "createNameRegistratorPerTxsNotEnoughGasBefore" # failing after increasing SSTORE gas limit to 72k
    "createRevert" # failing after increasing SSTORE gas limit to 72k
    "CreateTransactionReverted" # failing after increasing SSTORE gas limit to 72k
    "dataTx" # failing after increasing SSTORE gas limit to 72k

    # InvaldBlockTests/bcEIP1559
    "badBlocks" # failing after increasing SSTORE gas limit to 72k
    "badUncles" # reorgs don't apply to us
    "checkGasLimit" # not sure what issue is
    "baseFee" # failing after increasing SSTORE gas limit to 72k
    "feeCap" # failing after increasing SSTORE gas limit to 72k
    "transFail" # failing after increasing SSTORE gas limit to 72k
    "valCausesOOF" # failing after increasing SSTORE gas limit to 72k
    "gasLimit40m" # failing after increasing SSTORE gas limit to 72k
    "gasLimit20m" # failing after increasing SSTORE gas limit to 72k
)

# Skip based on explicit relative paths when name filtering is insufficient
declare -a test_path_skip_list=(
    "./ethtests/BlockchainTests/ValidBlocks/bcStateTests/refundReset.json"
)

seid_wrapper_dir=$(mktemp -d)
cleanup_wrapper() {
    rm -rf "$seid_wrapper_dir"
}
trap cleanup_wrapper EXIT

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

# list out all paths to json files starting from the block_tests_dir
block_tests=$(find "$block_tests_path" -name "*.json" | sort)

i=0

# for each json file, run the block test
for test_path in $block_tests; do
    test_name=$(basename "$test_path" .json)
    match_found=false

    # Iterate through the test_path_run_list to check for a match
    for run_path in "${test_path_run_list[@]}"; do
        if [[ "$test_path" == *"$run_path"* ]]; then
            match_found=true
            break
        fi
    done

    # Skip the test if no match is found
    if [ "$match_found" = false ]; then
        continue
    fi

    echo "test file: $test_path"
    echo "test dir: $test_path"

    # Check if the test name is in the skip list
    if printf '%s\n' "${test_name_skip_list[@]}" | grep -qx "$test_name"; then
        echo "Skipping test in skip list: $test_path"
        continue
    fi

    if printf '%s\n' "${test_path_skip_list[@]}" | grep -qx "$test_path"; then
        echo "Skipping test in skip list: $test_path"
        continue
    fi

    # Check if "${test_name}_Prague" is not in the test file
    if ! grep -q "${test_name}_Prague" "$test_path"; then
        echo "Skipping test due to missing Prague tag: $test_path"
        continue
    fi

    if [ $((i % runner_total)) -ne $runner_index ]; then
        i=$((i+1))
        runner_id=$((i % runner_total))
        echo "Skipping test not in runner index: $test_path, runner index: $runner_id"
        continue
    fi

    i=$((i+1))

    echo -e "\n*********************************************************\n"
    echo "Running block test: $test_path"
    echo "test name: ${test_name}_Prague"
    echo -e "\n*********************************************************\n"
    rm -rf ~/.sei || true
    NO_RUN=1 ./scripts/initialize_local_chain.sh
    seid blocktest --block-test $test_path --test-name "${test_name}_Prague"
done
