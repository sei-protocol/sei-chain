#!/usr/bin/env bash

set -euo pipefail

SUITE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ETHEREUM_TESTS_REPOSITORY="${ETHEREUM_TESTS_REPOSITORY:-https://github.com/ethereum/tests.git}"
ETHEREUM_TESTS_REVISION="${ETHEREUM_TESTS_REVISION:-c67e485ff8b5be9abc8ad15345ec21aa22e290d9}"
ETHEREUM_TESTS_DIR="${ETHEREUM_TESTS_DIR:-${SUITE_ROOT}/.cache/ethereum-tests}"

command -v git >/dev/null 2>&1 || {
    echo "git is required to install ethereum/tests." >&2
    exit 2
}

if [[ ! -d "${ETHEREUM_TESTS_DIR}/.git" ]]; then
    if [[ -e "${ETHEREUM_TESTS_DIR}" ]]; then
        echo "${ETHEREUM_TESTS_DIR} exists but is not an ethereum/tests checkout." >&2
        exit 2
    fi
    mkdir -p "$(dirname "${ETHEREUM_TESTS_DIR}")"
    git init --quiet "${ETHEREUM_TESTS_DIR}"
    git -C "${ETHEREUM_TESTS_DIR}" remote add origin "${ETHEREUM_TESTS_REPOSITORY}"
fi

current_revision="$(git -C "${ETHEREUM_TESTS_DIR}" rev-parse HEAD 2>/dev/null || true)"
if [[ "${current_revision}" != "${ETHEREUM_TESTS_REVISION}" ]]; then
    if [[ -n "$(git -C "${ETHEREUM_TESTS_DIR}" status --short 2>/dev/null)" ]]; then
        echo "Refusing to replace modified ethereum/tests checkout at ${ETHEREUM_TESTS_DIR}." >&2
        exit 2
    fi
    git -C "${ETHEREUM_TESTS_DIR}" fetch --depth 1 origin "${ETHEREUM_TESTS_REVISION}"
    git -C "${ETHEREUM_TESTS_DIR}" checkout --quiet --detach FETCH_HEAD
fi

for suite in TransactionTests RLPTests; do
    if [[ ! -d "${ETHEREUM_TESTS_DIR}/${suite}" ]]; then
        echo "Pinned ethereum/tests checkout is missing ${suite}." >&2
        exit 2
    fi
done

echo "Installed ethereum/tests ${ETHEREUM_TESTS_REVISION} in ${ETHEREUM_TESTS_DIR}."
