
ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
CONTRACTS="$ROOT/../../contracts"
TOP_PID=$$

set -e

main() {
    trap "exit 1" TERM

    if ! command -v "sd" &> /dev/null; then
        echo "The 'sd' command is required for this script, please install it"
        echo "by following instructions at https://github.com/chmln/sd?tab=readme-ov-file#installation"
        exit 1
    fi

    data_dir="$ROOT/.firehose-data"
    seid="${SEID:-seid}"
    seid_args="start --home \"$HOME/.sei\" --trace --chain-id sei-chain"
    fireeth_log="$ROOT/.fireeth.log"
    start_firehose="true"
    parallel_tx_enabled=${PARALLEL_TX_ENABLED:-"true"}

    while getopts "so:" opt; do
        case $opt in
        s) start_firehose=false;;
        o) run_only=$OPTARG;;
        esac
    done
    shift $((OPTIND-1))

    fireeth="fireeth"
    if ! command -v "$fireeth" &> /dev/null; then
        echo "The '$fireeth' binary could not be found, you can install it through one of those means:"
        echo ""
        echo "- By running 'brew install streamingfast/tap/firehose-ethereum' on Mac or Linux system (with Homebrew installed)"
        echo "- By building it from source cloning https://github.com/streamingfast/firehose-ethereum.git and then 'go install ./cmd/fireeth'"
        echo "- By downloading a pre-compiled binary from https://github.com/streamingfast/firehose-ethereum/releases"
        exit 1
    fi

    if [[ $start_firehose == "true" ]]; then
        echo "Running Sei node with Firehose tracer activated via 'fireeth' and parallel tx enabled: $parallel_tx_enabled"
        rm -rf "$data_dir"

        ("$fireeth" \
            start \
            reader-node,relayer,merger,firehose \
            -c '' \
            -d "$data_dir" \
            --advertise-chain-name=battlefield \
            --ignore-advertise-validation \
            --common-first-streamable-block=1 \
            --reader-node-path="$seid" \
            --reader-node-arguments="$seid_args" \
            --reader-node-bootstrap-data-url="bash://$ROOT/bootstrap.sh?env_PARALLEL_TX_ENABLED=${parallel_tx_enabled}" \
            --firehose-grpc-listen-addr="localhost:8089" &> "$fireeth_log") &
        fireeth_pid=$!
        trap "cleanup" EXIT

        monitor "fireeth" $fireeth_pid "$fireeth_log" &

        echo "Waiting for Firehose instance to be ready"
        wait_for_firehose_ready "$fireeth_log"

        echo "Firehose instance is ready"
    fi

    echo "Running Firehose tests"
    cd "$CONTRACTS"

    if [[ "$run_only" != "" || "$run_only" == "tracer" ]]; then
        echo "-o <value> flag is not working right now"
        exit 1
    fi

    npx hardhat test --network seilocal test/tracer/firehose/FirehoseTracerTest.js
}

cleanup() {
    for job in `jobs -p`; do
        kill $job &> /dev/null
        wait $job &> /dev/null || true
    done
}

wait_for_firehose_ready() {
    firehose_log="$1"

    for i in {1..10}; do
        if grep -q '(firehose) launching gRPC server' "$firehose_log"; then
            break
        fi

        if [[ $i -eq 10 ]]; then
            >&2 echo "The 'fireeth' instance did not start within ~45s which is not expected."
            >&2 echo ""
            show_logs_preview "$firehose_log"
            kill -s TERM $TOP_PID
        fi

        sleep $i
    done

    # Sleep a bit again to ensure the server is fully started
    sleep 1
}

# usage <name> <pid> <process_log>
monitor() {
  name="$1"
  pid="$2"
  process_log="$3"

  while true; do
    if ! kill -0 $pid &> /dev/null; then
      sleep 2

      echo "Process $name ($pid) died, exiting parent"
      if [[ "$process_log" != "" ]]; then
        show_logs_preview "$process_log"
      fi

      kill -s TERM $TOP_PID &> /dev/null
      exit 0
    fi

    sleep 1
  done
}

show_logs_preview() {
    log_file="$1"

    >&2 echo "Here the first 25 lines followed by the last 25 lines of the log:"
    >&2 echo ""

    >&2 echo "  ..."
    head -n 25 "$log_file" | >&2 sd '^(.)' '  |    $1'
    >&2 echo "  .\n  ."
    tail -n 25 "$log_file" | >&2 sd '^(.)' '  |    $1'

    >&2 echo ""
    >&2 echo "See full logs with 'less `relpath $log_file`'"
}

extract() {
    set +e
    output=`echo "$1" | jq -r "$2"`
    if [ $? -ne 0 ]; then
        >&2 echo "Failed to extract from: $1"
        >&2 echo "JQ: $2"
        kill -s TERM $TOP_PID
    fi

    echo "$output"
    set -e
}

relpath() {
  if [[ $1 =~ /* ]]; then
    # Works only if path is already absolute and do not contain ,
    echo "$1" | sed s,$PWD,.,g
  else
    # Print as-is
    echo $1
  fi
}

main "$@"
