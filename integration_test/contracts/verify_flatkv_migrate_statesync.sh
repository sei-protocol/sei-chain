#!/bin/bash
#
# Start v0 -> v1 migration and exercise state-sync recovery while the
# cluster is in the migration window.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

STATESYNC_BATCH=${MIGRATE_STATESYNC_KEYS_PER_BLOCK:-25}

echo "verify_flatkv_migrate_statesync: batch=$STATESYNC_BATCH"

assert_all_nodes_in_mode "memiavl_only"
coordinated_stop_at_common_height "memiavl_only" "$(fixture_height_floor)"
flip_sc_write_mode "migrate_evm" "$STATESYNC_BATCH"
coordinated_restart_with_settle "migrate_evm"
wait_for_migration_in_progress 90

"$SCRIPT_DIR/verify_flatkv_statesync_crash_recovery.sh"

wait_for_migrate_status_complete
assert_all_nodes_alive
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: FlatKV statesync recovery succeeded around EVM migration"
