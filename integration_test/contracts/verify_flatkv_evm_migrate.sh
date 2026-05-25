#!/bin/bash
#
# Drives the operator-style v0 -> v1 FlatKV EVM migration:
# memiavl_only -> migrate_evm.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

MIN_KEYS_MIGRATED=${MIGRATE_MIN_KEYS_MIGRATED:-3500}

echo "verify_flatkv_evm_migrate: node_count=$NODE_COUNT"

run_v0_to_v1_migration "$KEYS_TO_MIGRATE_PER_BLOCK"
print_migration_summaries
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: FlatKV EVM migrate completed on all $NODE_COUNT validators and FlatKV EVM digests agree"
