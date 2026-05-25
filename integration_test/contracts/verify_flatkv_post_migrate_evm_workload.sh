#!/bin/bash
#
# After v0 -> v1 completes, run EVM writes for a few blocks. This catches
# post-migration EVM iterator panics in EndBlock paths.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

echo "verify_flatkv_post_migrate_evm_workload: node_count=$NODE_COUNT"

wait_for_migrate_status_complete

before=$(node_height "sei-node-0")
echo "Running post-migration EVM fixture at height $before"
docker exec sei-node-0 bash -lc "FLATKV_EVM_BULK_STORAGE_KEYS=${POST_MIGRATE_EVM_BULK_STORAGE_KEYS:-50} integration_test/contracts/deploy_flatkv_evm_fixture.sh"

wait_for_height_progress 2 120
assert_all_nodes_alive
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: post-migration EVM workload completed without validator panic"
