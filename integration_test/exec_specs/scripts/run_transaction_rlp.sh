#!/usr/bin/env bash

set -euo pipefail

SUITE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SEI_CHAIN_DIR="${SEI_CHAIN_DIR:-$(cd "${SUITE_ROOT}/../.." && pwd)}"
ETHEREUM_TESTS_DIR="${ETHEREUM_TESTS_DIR:-${SUITE_ROOT}/.cache/ethereum-tests}"
TEST_PATTERN="${ETHEREUM_LEGACY_TEST_PATTERN:-TestTransaction|TestRLP}"
TEST_TIMEOUT="${ETHEREUM_LEGACY_TEST_TIMEOUT:-5m}"

if [[ ! -f "${SEI_CHAIN_DIR}/go.mod" ]]; then
    echo "Sei chain checkout not found at ${SEI_CHAIN_DIR}." >&2
    exit 2
fi

bash "${SUITE_ROOT}/scripts/install_legacy_tests.sh"

resolved_geth_version="$(
    cd "${SEI_CHAIN_DIR}"
    go list -m -f \
        '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' \
        github.com/ethereum/go-ethereum
)"

geth_dir="$(
    cd "${SEI_CHAIN_DIR}"
    go mod download -json github.com/ethereum/go-ethereum |
        python3 -c \
            'import json, sys; print(json.load(sys.stdin).get("Dir", ""))'
)"
if [[ ! -d "${geth_dir}/tests" ]]; then
    echo "Pinned Sei go-ethereum test package not found at ${geth_dir}/tests." >&2
    exit 2
fi

temp_dir="$(mktemp -d "${SUITE_ROOT}/.geth-tests.XXXXXX")"
cleanup() {
    rm -rf "${temp_dir}"
}
trap cleanup EXIT

cp -R "${geth_dir}/tests/." "${temp_dir}"
# Go's module cache is read-only. Make the private copy removable so cleanup
# cannot turn a successful test run into a failed CI job.
chmod -R u+w "${temp_dir}"
rm -rf "${temp_dir}/testdata"
mkdir -p "${temp_dir}/testdata"
ln -s "${ETHEREUM_TESTS_DIR}/TransactionTests" \
    "${temp_dir}/testdata/TransactionTests"
ln -s "${ETHEREUM_TESTS_DIR}/RLPTests" \
    "${temp_dir}/testdata/RLPTests"

temp_package="./integration_test/exec_specs/$(basename "${temp_dir}")"
echo "Running ${TEST_PATTERN} with Sei go-ethereum ${resolved_geth_version}."
(
    cd "${SEI_CHAIN_DIR}"
    go test -count=1 -timeout="${TEST_TIMEOUT}" -run "${TEST_PATTERN}" \
        "${temp_package}" "$@"
)
