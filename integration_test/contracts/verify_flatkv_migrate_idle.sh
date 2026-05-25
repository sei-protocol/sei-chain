#!/bin/bash
#
# Run v0 -> v1 with no required caller workload. The migration must still
# complete on idle blocks, which pins empty-changeset forwarding at the
# docker level.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

echo "verify_flatkv_migrate_idle: node_count=$NODE_COUNT"

MIN_KEYS_MIGRATED=${MIGRATE_MIN_KEYS_MIGRATED:-0}
FIXTURE_HEIGHT_FILE=/tmp/nonexistent-flatkv-idle-fixture-height
run_v0_to_v1_migration "${MIGRATE_IDLE_KEYS_PER_BLOCK:-1}"
assert_all_nodes_alive
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: FlatKV EVM migration completed on idle blocks"
