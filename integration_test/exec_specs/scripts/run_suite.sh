#!/usr/bin/env bash

set -euo pipefail

SUITE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_PATHS="${EEST_TEST_PATHS_FILE:-${SUITE_ROOT}/config/prague-paths.list}"
IGNORED_PATHS="${EEST_IGNORED_PATHS_FILE:-${SUITE_ROOT}/config/prague-ignores.list}"
REMOTE_EXCLUSIONS="${EEST_REMOTE_EXCLUSIONS_FILE:-${SUITE_ROOT}/config/remote-exclusions.list}"
EEST_SWEEP_AMOUNT="${EEST_SWEEP_AMOUNT:-100000 ether}"
EEST_SHARD_COUNT="${EEST_SHARD_COUNT:-1}"
EEST_SHARD_INDEX="${EEST_SHARD_INDEX:-0}"

if [[ "${EEST_SHARD_COUNT}" == "1" ]]; then
    report_name="all"
else
    report_name="shard-${EEST_SHARD_INDEX}"
fi
EEST_JUNIT_XML="${EEST_JUNIT_XML:-${SUITE_ROOT}/reports/junit-${report_name}.xml}"

export EEST_JUNIT_XML
export EEST_SWEEP_AMOUNT

execute_options=()
test_paths=()

while IFS= read -r test_path; do
    if [[ -n "${test_path}" && ! "${test_path}" =~ ^[[:space:]]*# ]]; then
        test_paths+=("${test_path}")
    fi
done <"${TEST_PATHS}"

while IFS= read -r ignored_path; do
    if [[ -n "${ignored_path}" && ! "${ignored_path}" =~ ^[[:space:]]*# ]]; then
        execute_options+=(--ignore="${ignored_path}")
    fi
done <"${IGNORED_PATHS}"

while IFS= read -r test_id; do
    if [[ -n "${test_id}" && ! "${test_id}" =~ ^[[:space:]]*# ]]; then
        execute_options+=(--deselect="${test_id}")
    fi
done <"${REMOTE_EXCLUSIONS}"

execute_command=(bash "${SUITE_ROOT}/scripts/run_remote.sh")
execute_command+=("${execute_options[@]}")
execute_command+=("${test_paths[@]}")
execute_command+=("$@")

set +e
"${execute_command[@]}"
status=$?
set -e

if [[ -f "${EEST_JUNIT_XML}" ]]; then
    python3 "${SUITE_ROOT}/scripts/summarize_reports.py" "${EEST_JUNIT_XML}" || true
fi

exit "${status}"
