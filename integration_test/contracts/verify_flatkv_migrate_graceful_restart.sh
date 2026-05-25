#!/bin/bash
#
# Start v0 -> v1 migration, send SIGTERM (not SIGKILL) to one validator
# while the boundary is in-flight, wait for it to exit cleanly, then
# restart and require deterministic completion.
#
# Counterpart to verify_flatkv_migrate_resume_after_crash.sh: that
# script proves SIGKILL-during-migration is recoverable; this one
# proves the graceful-stop path -- the one operators will actually
# use during scheduled restarts -- has the same end state and does
# not leak partial commits / locked DB handles into the next start.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

VICTIM_NODE=${MIGRATE_GRACEFUL_VICTIM_NODE:-sei-node-1}
GRACEFUL_BATCH=${MIGRATE_GRACEFUL_KEYS_PER_BLOCK:-25}
SHUTDOWN_TIMEOUT=${MIGRATE_GRACEFUL_SHUTDOWN_TIMEOUT:-45}

echo "verify_flatkv_migrate_graceful_restart: victim=$VICTIM_NODE batch=$GRACEFUL_BATCH"

assert_all_nodes_in_mode "memiavl_only"
coordinated_stop_at_common_height "memiavl_only" "$(fixture_height_floor)"
flip_sc_write_mode "migrate_evm" "$GRACEFUL_BATCH"
coordinated_restart_with_settle "migrate_evm"

wait_for_migration_in_progress 90
echo "Sending SIGTERM to $VICTIM_NODE mid-migration"
graceful_stop_node "$VICTIM_NODE" "$SHUTDOWN_TIMEOUT"
sleep 3
restart_node "$VICTIM_NODE"
wait_for_all_seid_start "after mid-migration SIGTERM restart"

wait_for_migrate_status_complete
assert_all_nodes_alive
assert_no_panics_in_logs "graceful restart window"
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: FlatKV EVM migration resumed deterministically after $VICTIM_NODE graceful restart"
