
ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

set -e

main() {
    PARALLEL_TX_ENABLED=false bash "$ROOT/run_test_case.sh"
    PARALLEL_TX_ENABLED=true bash "$ROOT/run_test_case.sh"
}

main "$@"
