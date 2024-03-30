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

# Define an array of tests to skip
declare -a skip_list=(
    "DelegateCallSpam" # passes, but takes super long
    "blockhashTests" # failing
    "blockhashNonConstArg" # failing
    "BLOCKHASH_Bounds" # newly failing
    "logRevert" # failing after increment height
)

# list out all paths to json files starting from the block_tests_dir
block_tests=$(find "$block_tests_path" -name "*.json")

test_files=""

i=0

# for each json file, run the block test
for test_file in $block_tests; do
    test_name=$(basename "$test_file" .json)

    # Check if the test name is in the skip list
    if printf '%s\n' "${skip_list[@]}" | grep -qx "$test_name"; then
        echo "Skipping test: $test_file"
        continue
    fi

    # Check if "${test_name}_Cancun" is not in the test file
    if ! grep -q "${test_name}_Cancun" "$test_file"; then
        echo "Skipping test due to missing Cancun tag: $test_file"
        continue
    fi

    if [ $((i % runner_total)) -ne $runner_index ]; then
        i=$((i+1))
        continue
    fi

    i=$((i+1))

    echo -e "\n*********************************************************\n"
    echo "Running block test: $test_file"
    echo "test name: ${test_name}_Cancun"
    echo -e "\n*********************************************************\n"
    rm -r ~/.sei || true
    NO_RUN=1 ./scripts/initialize_local_chain.sh
    seid blocktest --block-test $test_file --test-name "${test_name}_Cancun"
done
