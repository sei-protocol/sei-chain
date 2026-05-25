#!/bin/bash
#
# Start v0 -> v1 migration, SIGKILL one validator while the boundary is
# in-flight, then restart and require deterministic completion.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

VICTIM_NODE=${MIGRATE_CRASH_VICTIM_NODE:-sei-node-1}
CRASH_BATCH=${MIGRATE_CRASH_KEYS_PER_BLOCK:-25}

echo "verify_flatkv_migrate_resume_after_crash: victim=$VICTIM_NODE batch=$CRASH_BATCH"

assert_all_nodes_in_mode "memiavl_only"
coordinated_stop_at_common_height "memiavl_only" "$(fixture_height_floor)"
flip_sc_write_mode "migrate_evm" "$CRASH_BATCH"
coordinated_restart_with_settle "migrate_evm"

wait_for_migration_in_progress 90
echo "Killing $VICTIM_NODE mid-migration"
kill_node "$VICTIM_NODE"
sleep 5
restart_node "$VICTIM_NODE"
wait_for_all_seid_start "after mid-migration SIGKILL restart"

wait_for_migrate_status_complete
assert_all_nodes_alive
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: FlatKV EVM migration resumed deterministically after $VICTIM_NODE crash"
