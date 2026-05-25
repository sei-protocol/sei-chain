#!/bin/bash
#
# Run EVM traffic while v0 -> v1 migration is actively draining keys.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

LIVE_BATCH=${MIGRATE_LIVE_KEYS_PER_BLOCK:-25}
LIVE_TIMEOUT=${MIGRATE_LIVE_WORKLOAD_SECS:-45}

echo "verify_flatkv_migrate_with_live_workload: batch=$LIVE_BATCH workload_secs=$LIVE_TIMEOUT"

assert_all_nodes_in_mode "memiavl_only"
coordinated_stop_at_common_height "memiavl_only" "$(fixture_height_floor)"
flip_sc_write_mode "migrate_evm" "$LIVE_BATCH"
coordinated_restart_with_settle "migrate_evm"
wait_for_migration_in_progress 90

echo "Starting live EVM contract-storage stress workload"
set +e
timeout "${LIVE_TIMEOUT}s" go run ./scripts/evm_stress -mode contract-storage
stress_rc=$?
set -e
if [ "$stress_rc" -ne 0 ] && [ "$stress_rc" -ne 124 ]; then
  echo "ERROR: evm_stress failed with exit code $stress_rc" >&2
  dump_all_node_logs
  exit 1
fi

wait_for_migrate_status_complete
assert_all_nodes_alive
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: FlatKV EVM migration completed while live EVM workload was running"
