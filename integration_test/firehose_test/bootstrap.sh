
ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
SCRIPTS="$ROOT/../../scripts"

set -e

main() {
    parallel_tx_enabled=${PARALLEL_TX_ENABLED:-"true"}

    echo "Initializing local chain"
    echo "- Parallel tx enabled: $parallel_tx_enabled"
    echo ""

    NO_RUN=1 $SCRIPTS/initialize_local_chain.sh

    if ! command -v "sd" &> /dev/null; then
        echo "The 'sd' command is required for this script, please install it"
        echo "by following instructions at https://github.com/chmln/sd?tab=readme-ov-file#installation"
        exit 1
    fi

    sd '\[evm\]' "[evm]\nlive_evm_tracer = \"firehose\"" "$HOME/.sei/config/app.toml"

    occ_enabled="true"
    if [[ "${parallel_tx_enabled}" != "true" ]]; then
        occ_enabled="false"
    fi

    sd 'occ-enabled *=.*' "occ-enabled = ${occ_enabled}" "$HOME/.sei/config/app.toml"
}

main "$@"
