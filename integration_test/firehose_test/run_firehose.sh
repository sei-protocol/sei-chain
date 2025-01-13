
ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

set -e

main() {
    data_dir="$ROOT/.firehose-data"
    seid="${SEID:-seid}"
    seid_args="start --home \"$HOME/.sei\" --trace --chain-id sei-chain"
    parallel_tx_enabled=${PARALLEL_TX_ENABLED:-"true"}

    fireeth="fireeth"
    if ! command -v "$fireeth" &> /dev/null; then
        echo "The '$fireeth' binary could not be found, you can install it through one of those means:"
        echo ""
        echo "- By running 'brew install streamingfast/tap/firehose-ethereum' on Mac or Linux system (with Homebrew installed)"
        echo "- By building it from source cloning https://github.com/streamingfast/firehose-ethereum.git and then 'go install ./cmd/fireeth'"
        echo "- By downloading a pre-compiled binary from https://github.com/streamingfast/firehose-ethereum/releases"
        exit 1
    fi

    echo "Running Sei node with Firehose tracer activated via 'fireeth' and parallel tx enabled: $parallel_tx_enabled"
    rm -rf "$data_dir"

    "$fireeth" \
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
        --firehose-grpc-listen-addr="localhost:8089"
}

main "$@"
