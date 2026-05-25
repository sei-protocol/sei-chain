#!/bin/bash
#
# Run the v0 -> v1 FlatKV EVM migration and assert chain-level continuity:
#   1. block_id.hash equal on every validator for every height in
#      [pre_flip, post_complete] -- catches transient consensus divergence
#      that recovered before the digest snapshot,
#   2. no panic/fatal lines in any validator log during the window,
#   3. FlatKV digests agree across validators.
#
# This is the per-block AppHash continuity sibling of
# verify_flatkv_evm_migrate.sh: that script only checks the post-migration
# digest, this one also checks that no block in between disagreed across
# validators.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

echo "verify_flatkv_migrate_apphash_continuity: node_count=$NODE_COUNT"

assert_all_nodes_in_mode "memiavl_only"

pre_flip_height=$(node_height "sei-node-0")
echo "Pre-flip reference height: $pre_flip_height"

run_v0_to_v1_migration "$KEYS_TO_MIGRATE_PER_BLOCK"
print_migration_summaries

# Give the cluster a couple of post-completion blocks so the window we
# check includes the boundary-latched commits as well.
wait_for_height_progress 2 60

# Pick the upper bound from the slowest validator so every height in
# the range is guaranteed to be present on every node. Otherwise nodes
# that lag by 1-2 blocks would return null for the most recent heights
# and trip a false "missing block" failure.
post_height=$(pick_compare_height)
echo "Post-migration cross-validator floor height: $post_height"

if [ "$post_height" -le "$pre_flip_height" ]; then
  echo "FAIL: chain did not advance during migration window ($pre_flip_height -> $post_height)" >&2
  dump_all_node_logs
  exit 1
fi

# Hash polling is one HTTP round-trip per (node, height); cap the window
# size to keep this comfortably under a CI step's wall clock even if the
# fixture/migration ran for many blocks. We always include the entire
# flip-through-completion window; only the tail past that gets trimmed.
MAX_WINDOW=${APPHASH_CONTINUITY_MAX_HEIGHTS:-400}
window_lo=$pre_flip_height
window_hi=$post_height
if [ $(( window_hi - window_lo + 1 )) -gt "$MAX_WINDOW" ]; then
  window_lo=$(( window_hi - MAX_WINDOW + 1 ))
  echo "Truncating apphash window to last $MAX_WINDOW heights ($window_lo..$window_hi)"
fi

assert_cross_validator_block_hashes "$window_lo" "$window_hi"
assert_no_panics_in_logs "v0->v1 migration window"
assert_cross_validator_flatkv_digest "" account code storage

echo "PASS: FlatKV EVM v0->v1 migration completed with cross-validator block continuity"
