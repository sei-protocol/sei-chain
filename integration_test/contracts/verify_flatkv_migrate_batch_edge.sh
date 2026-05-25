#!/bin/bash
#
# Exercise the v0 -> v1 FlatKV EVM migration at batch-size extremes:
#
#   MIGRATE_BATCH_EDGE=min      -- batch=1 keys/block, forces the
#                                  per-key boundary-advance path to run
#                                  on every block until the drain completes.
#   MIGRATE_BATCH_EDGE=oneshot  -- batch >> fixture key count, forces the
#                                  whole drain into a single block so
#                                  boundary latch + completion-on-first-
#                                  batch transitions get exercised.
#
# Either extreme is a stricter probe of MigrationManager's per-block
# accounting than the default batch size used by verify_flatkv_evm_migrate.sh.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

mode=${MIGRATE_BATCH_EDGE:-}
case "$mode" in
  min)
    batch=${MIGRATE_BATCH_EDGE_MIN:-1}
    ;;
  oneshot)
    batch=${MIGRATE_BATCH_EDGE_ONESHOT:-100000}
    ;;
  "")
    echo "MIGRATE_BATCH_EDGE must be set to 'min' or 'oneshot'" >&2
    exit 2
    ;;
  *)
    echo "Unknown MIGRATE_BATCH_EDGE=$mode (expected min|oneshot)" >&2
    exit 2
    ;;
esac

echo "verify_flatkv_migrate_batch_edge: mode=$mode batch=$batch node_count=$NODE_COUNT"

run_v0_to_v1_migration "$batch"
print_migration_summaries
assert_all_nodes_alive
assert_no_panics_in_logs "batch-edge migration ($mode)"
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: FlatKV EVM v0->v1 migration completed at batch=$batch ($mode)"
