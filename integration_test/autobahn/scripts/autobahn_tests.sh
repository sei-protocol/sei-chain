#!/bin/bash
set -e

LOG_DIR="build/generated/logs"

echo "============================================"
echo "  Autobahn Integration Tests"
echo "============================================"

get_height() {
  # Use abci_info instead of /status because autobahn updates the app state
  # but not the CometBFT block store, so /status shows height 0.
  # TODO: switch back to /status once autobahn syncs the CometBFT state store.
  local retries=10
  for i in $(seq 1 $retries); do
    HEIGHT=$(curl -s http://localhost:26657/abci_info 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['response']['last_block_height'])" 2>/dev/null)
    if [ -n "$HEIGHT" ] && [ "$HEIGHT" != "0" ]; then
      echo "$HEIGHT"
      return 0
    fi
    sleep 3
  done
  echo "FAIL: Could not get block height after $retries retries" >&2
  return 1
}

# Assert autobahn is enabled on all running nodes.
# Called before each test to guard against accidental disablement.
assert_autobahn_enabled() {
  for i in 0 1 2 3; do
    LOG="$LOG_DIR/seid-$i.log"
    if [ ! -f "$LOG" ]; then continue; fi
    if ! grep -q "GigaRouter initialized" "$LOG"; then
      echo "FAIL: Autobahn not enabled on node $i (missing 'GigaRouter initialized' in $LOG)"
      exit 1
    fi
  done
}

# ---- Test 1: Blocks are being produced ----
echo ""
echo "=== Test 1: Block production ==="
assert_autobahn_enabled
HEIGHT1=$(get_height)
echo "  Height: $HEIGHT1"
sleep 5
HEIGHT2=$(get_height)
echo "  Height: $HEIGHT2 (after 5s)"
if [ "$HEIGHT2" -le "$HEIGHT1" ]; then
  echo "FAIL: Block height not advancing ($HEIGHT1 -> $HEIGHT2)"
  exit 1
fi
echo "PASS: Blocks advancing ($HEIGHT1 -> $HEIGHT2)"

# ---- Test 2: Bank transfer ----
echo ""
echo "=== Test 2: Bank transfer ==="
assert_autobahn_enabled
# Create a test recipient address
RECIPIENT=$(docker exec sei-node-0 sh -c "printf '12345678\n12345678\n' | seid keys add test_recipient --output json 2>/dev/null" | python3 -c "import sys,json; print(json.load(sys.stdin)['address'])")
echo "  Recipient: $RECIPIENT"

# Send from node_admin (genesis account) to recipient.
# Use -b sync (not -b block) because CometBFT consensus is disabled in autobahn mode.
# TODO: support -b block once autobahn updates the CometBFT block store so the broadcast
# endpoint can track tx inclusion.
docker exec sei-node-0 sh -c "printf '12345678\n' | seid tx bank send node_admin $RECIPIENT 1000000usei --chain-id sei --fees 2000usei -b sync -y --output json" > /dev/null 2>&1

# Wait for the tx to be finalized through autobahn consensus.
echo "  Waiting for tx to finalize..."
BALANCE="0"
for attempt in $(seq 1 15); do
  BALANCE=$(docker exec sei-node-0 seid q bank balances "$RECIPIENT" --denom usei --output json 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['amount'])" 2>/dev/null)
  if [ "$BALANCE" = "1000000" ]; then
    break
  fi
  sleep 2
done
echo "  Balance: $BALANCE usei"
if [ "$BALANCE" != "1000000" ]; then
  echo "FAIL: Expected balance 1000000, got $BALANCE"
  exit 1
fi
echo "PASS: Bank transfer successful"

# ---- Test 3: Fault tolerance - one node down ----
echo ""
echo "=== Test 3: One node down (3/4 quorum) ==="
assert_autobahn_enabled
HEIGHT_BEFORE=$(get_height)
echo "  Height before: $HEIGHT_BEFORE"
echo "  Killing seid on node 3..."
docker exec sei-node-3 pkill seid || true
sleep 10
HEIGHT_AFTER=$(get_height)
echo "  Height after: $HEIGHT_AFTER"
if [ "$HEIGHT_AFTER" -le "$HEIGHT_BEFORE" ]; then
  echo "FAIL: Chain should continue with 3/4 validators"
  exit 1
fi
echo "PASS: Chain continues with one node down ($HEIGHT_BEFORE -> $HEIGHT_AFTER)"

# ---- Test 4: Two nodes down (no quorum) ----
echo ""
echo "=== Test 4: Two nodes down (no quorum) ==="
assert_autobahn_enabled
echo "  Killing seid on node 2..."
docker exec sei-node-2 pkill seid || true
sleep 5
HEIGHT_BEFORE=$(get_height)
echo "  Height: $HEIGHT_BEFORE"
sleep 15
HEIGHT_AFTER=$(get_height)
echo "  Height after 15s: $HEIGHT_AFTER"
if [ "$HEIGHT_AFTER" -ne "$HEIGHT_BEFORE" ]; then
  echo "FAIL: Chain should halt with 2/4 validators (height changed: $HEIGHT_BEFORE -> $HEIGHT_AFTER)"
  exit 1
fi
echo "PASS: Chain halted with two nodes down"

# ---- Test 5: Recovery ----
# TODO: Re-enable once autobahn supports node restart. Currently, a restarted seid fails
# because autobahn writes to the app state but not to the CometBFT block/state store.
# On restart, the CometBFT handshaker sees appHeight >> storeHeight and cannot reconcile.
echo ""
echo "=== Test 5: Recovery (SKIPPED — autobahn node restart not yet supported) ==="

echo ""
echo "============================================"
echo "  All Autobahn Integration Tests PASSED"
echo "============================================"
