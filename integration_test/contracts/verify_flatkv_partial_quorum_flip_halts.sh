#!/bin/bash
#
# Negative safety test: flipping only 3 of 4 validators to migrate_evm must
# not allow the chain to keep committing under mixed AppHash semantics.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=integration_test/contracts/lib/flatkv_migration.sh
source "$SCRIPT_DIR/lib/flatkv_migration.sh"

PARTIAL_TIMEOUT=${MIGRATE_PARTIAL_FLIP_OBSERVE_SECS:-30}

echo "verify_flatkv_partial_quorum_flip_halts: observe_secs=$PARTIAL_TIMEOUT"

assert_all_nodes_in_mode "memiavl_only"
pre_height=$(wait_for_cluster_height_sync "$(fixture_height_floor)" "$PRE_FLIP_SYNC_TIMEOUT" | tail -1)
echo "Pre-partial-flip height: $pre_height"

for i in 0 1 2; do
  docker exec "sei-node-$i" pkill -TERM -f "seid start" >/dev/null 2>&1 || true
done
sleep 5

for i in 0 1 2; do
  docker exec "sei-node-$i" bash -c "
    sed -i 's/^sc-write-mode = .*/sc-write-mode = \"migrate_evm\"/' '$APP_CONFIG'
    if grep -q '^sc-keys-to-migrate-per-block' '$APP_CONFIG'; then
      sed -i 's/^sc-keys-to-migrate-per-block = .*/sc-keys-to-migrate-per-block = $KEYS_TO_MIGRATE_PER_BLOCK/' '$APP_CONFIG'
    else
      sed -i '/^sc-write-mode/a sc-keys-to-migrate-per-block = $KEYS_TO_MIGRATE_PER_BLOCK' '$APP_CONFIG'
    fi
  "
  docker exec -d -e "ID=${i}" "sei-node-$i" /usr/bin/start_sei.sh
done

sleep "$PARTIAL_TIMEOUT"
post_height=$(node_height "sei-node-0")
echo "Post-partial-flip observed height on sei-node-0: $post_height"
# Strict halt: with one validator still on memiavl_only the cluster MUST NOT
# commit a single new block, because any block proposed by the migrated
# quorum would carry the post-cutover AppHash and the lagging node would
# fork off. A +1 tolerance is unsafe: it lets exactly the divergence this
# test is meant to catch slip through.
if [ "$post_height" -gt "$pre_height" ]; then
  echo "ERROR: mixed-mode partial quorum advanced from $pre_height to $post_height" >&2
  dump_all_node_logs
  exit 1
fi

# We intentionally do NOT attempt a "recover by flipping everyone" phase
# here. Once the migrated 3-node subset has committed even one block under
# the new AppHash and the 4th node has rejected it, the two sides of the
# cluster are on permanently divergent application state. There is no
# in-test way to reconcile that without wiping data, and pretending the
# cluster can recover masks the real safety bug surfaced above. Leave the
# cluster halted and let teardown clean it up.
echo "PASS: partial-quorum migration flip halted at height $pre_height as required"
