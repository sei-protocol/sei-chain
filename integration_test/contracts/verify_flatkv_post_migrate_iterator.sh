#!/bin/bash
#
# After v0 -> v1 completes, drive additional EVM contract activity to
# exercise post-migration EndBlock / storage-iteration paths and assert
# no validator logs a panic. This pins the fix for the post-migration
# EVM iterator panic regression (Cody review issue 1) and is intentionally
# stricter than verify_flatkv_post_migrate_evm_workload.sh, which only
# checks digest agreement.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

ROUNDS=${POST_MIGRATE_ITERATOR_ROUNDS:-3}
ROUND_KEYS=${POST_MIGRATE_ITERATOR_KEYS_PER_ROUND:-25}

echo "verify_flatkv_post_migrate_iterator: rounds=$ROUNDS keys/round=$ROUND_KEYS"

wait_for_migrate_status_complete
assert_all_nodes_alive

for round in $(seq 1 "$ROUNDS"); do
  echo "Post-migration EVM workload round $round/$ROUNDS"
  docker exec sei-node-0 bash -lc \
    "FLATKV_EVM_BULK_STORAGE_KEYS=${ROUND_KEYS} integration_test/contracts/deploy_flatkv_evm_fixture.sh"
  wait_for_height_progress 2 120
  assert_all_nodes_alive
  assert_no_panics_in_logs "post-migration EVM round $round"
done

# Final digest check to make sure the post-migration writes were
# applied identically across validators (and that the iterator paths
# we just exercised did not silently corrupt any bucket).
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: post-migration EVM iterator paths exercised across $ROUNDS rounds without panic"
