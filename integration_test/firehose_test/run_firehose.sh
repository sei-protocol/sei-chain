
ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

source "$ROOT/lib.sh"

set -e

main() {
    check_fireeth
    check_sd

    seid_args="start --home \"$HOME/.sei\" --trace --chain-id sei-chain"
    parallel_tx_enabled=${PARALLEL_TX_ENABLED:-"true"}

    occ_enabled="true"
    if [[ "${parallel_tx_enabled}" != "true" ]]; then
        occ_enabled="false"
    fi

    echo "Running Sei node with Firehose tracer activated via 'fireeth' and parallel tx enabled: $parallel_tx_enabled"
    rm -rf "$firehose_data_dir"

    NO_RUN=1 "$ROOT/../../scripts/initialize_local_chain.sh"

    sd '\[evm\]' "[evm]\nlive_evm_tracer = \"firehose\"" "$HOME/.sei/config/app.toml"
    sd 'occ-enabled *=.*' "occ-enabled = ${occ_enabled}" "$HOME/.sei/config/app.toml"

    exec "$fireeth" \
        start \
        reader-node,relayer,merger,firehose \
        -c '' \
        -d "$firehose_data_dir" \
        --advertise-chain-name=battlefield \
        --ignore-advertise-validation \
        --common-first-streamable-block=1 \
        --reader-node-path="$seid" \
        --reader-node-arguments="$seid_args" \
        --firehose-grpc-listen-addr="localhost:8089"
}

main "$@"
