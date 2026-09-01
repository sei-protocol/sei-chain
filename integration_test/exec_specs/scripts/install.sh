#!/usr/bin/env bash

set -euo pipefail

SUITE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EEST_REPOSITORY="${EEST_REPOSITORY:-https://github.com/ethereum/execution-specs.git}"
EEST_REVISION="${EEST_REVISION:-2ce2191562f76ec6e64a82f82165d821e1c781fc}"
EEST_DIR="${EEST_DIR:-${SUITE_ROOT}/.cache/execution-specs}"
EEST_PATCH="${EEST_PATCH:-${SUITE_ROOT}/patches/sei-compat.patch}"
EEST_UV_BIN="${EEST_UV_BIN:-uv}"
EEST_PYTHON="${EEST_PYTHON:-3.12}"

if [[ ! -f "${EEST_PATCH}" ]]; then
    echo "EEST compatibility patch not found at ${EEST_PATCH}." >&2
    exit 2
fi

file_digest() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d ' ' -f 1
    else
        shasum -a 256 "$1" | cut -d ' ' -f 1
    fi
}

fingerprint="${EEST_REVISION} $(file_digest "${EEST_PATCH}")"
fingerprint_file="${EEST_DIR}/.sei-eest-fingerprint"

if [[ -x "${EEST_DIR}/.venv/bin/execute" && -f "${fingerprint_file}" ]] \
    && [[ "$(<"${fingerprint_file}")" == "${fingerprint}" ]]; then
    exit 0
fi

command -v git >/dev/null 2>&1 || {
    echo "git is required to install execution-specs." >&2
    exit 2
}
command -v "${EEST_UV_BIN}" >/dev/null 2>&1 || {
    echo "uv is required. Install it from https://docs.astral.sh/uv/." >&2
    exit 2
}

if [[ ! -d "${EEST_DIR}/.git" ]]; then
    if [[ -e "${EEST_DIR}" ]]; then
        echo "${EEST_DIR} exists but is not an execution-specs checkout." >&2
        exit 2
    fi
    mkdir -p "$(dirname "${EEST_DIR}")"
    git init --quiet "${EEST_DIR}"
    git -C "${EEST_DIR}" remote add origin "${EEST_REPOSITORY}"
fi

current_revision="$(git -C "${EEST_DIR}" rev-parse HEAD 2>/dev/null || true)"
if [[ "${current_revision}" != "${EEST_REVISION}" ]]; then
    git -C "${EEST_DIR}" fetch --depth 1 origin "${EEST_REVISION}"
    git -C "${EEST_DIR}" checkout --quiet --detach --force FETCH_HEAD
fi

# This checkout is a derived cache. Restore upstream before applying the
# versioned Sei compatibility patch.
git -C "${EEST_DIR}" reset --quiet --hard HEAD
git -C "${EEST_DIR}" clean --quiet --force -d
if ! git -C "${EEST_DIR}" apply "${EEST_PATCH}"; then
    echo "EEST compatibility patch does not apply to ${EEST_REVISION}." >&2
    exit 2
fi

"${EEST_UV_BIN}" sync \
    --directory "${EEST_DIR}" \
    --frozen \
    --no-dev \
    --package ethereum-execution-testing \
    --python "${EEST_PYTHON}"

printf '%s\n' "${fingerprint}" >"${fingerprint_file}"
echo "Installed patched execution-specs ${EEST_REVISION} in ${EEST_DIR}."
