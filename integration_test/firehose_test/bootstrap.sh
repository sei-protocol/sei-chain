
ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
SCRIPTS="$ROOT/../../scripts"

set -e

main() {
    NO_RUN=1 $SCRIPTS/initialize_local_chain.sh

    if ! command -v "sd" &> /dev/null; then
        echo "The 'sd' command is required for this script, please install it"
        echo "by following instructions at https://github.com/chmln/sd?tab=readme-ov-file#installation"
        exit 1
    fi

    sd '\[evm\]' "[evm]\nlive_evm_tracer = \"firehose\"" "$HOME/.sei/config/app.toml"
}

main "$@"
