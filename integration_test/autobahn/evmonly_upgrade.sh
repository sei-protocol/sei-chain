#!/usr/bin/env bash
# Runs a software upgrade on the local four-validator Autobahn cluster:
#
#   1. an expedited proposal schedules vX.Y-test; a validator started early
#      on the new binary halts with BINARY UPDATED BEFORE TRIGGER and rejoins on
#      the old one; every validator halts at the plan's height with
#      UPGRADE "vX.Y-test" NEEDED and writes upgrade-info.json;
#   2. every validator restarts on a giga_test_upgrade build, applies the plan
#      at the height it halted at, its handler sets gov's max deposit period to
#      72h, and the chain continues with one block hash;
#   3. a validator restarted on the old binary halts as the wrong binary;
#   4. a cancel naming another plan is refused, then a second plan is cancelled
#      by name before its height and the chain runs past it;
#   5. a minor plan for vX.Y-minor-test, whose handler every validator already
#      runs, does not stop them before its height and applies there without a
#      halt.
#
# Start the cluster first, from the repository root:
#
#   AUTOBAHN=true EVM_GOVERNANCE_VOTING_PERIOD=30s DOCKER_DETACH=true make docker-cluster-start
#
# then run this script from the repository root. It needs cast and jq.
set -euo pipefail

GAS_PRICE=${GAS_PRICE:-1gwei}
NODES=4
PORTS=(8545 8547 8549 8551)
# The validators' evm_voter keys: anvil accounts 0 to 3 of the test mnemonic.
VOTER_KEYS=(
  0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
  0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d
  0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a
  0x7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6
)
# Anvil account 9 sends the transfers that produce blocks; Autobahn makes no
# empty blocks.
TICK_KEY=0x2a871d0798f97d79848a013d4936a73bf4cc922c825d33c1cf7073dff6d409c6
GOV=0x0000000000000000000000000000000000001006
PARAMS=0x0000000000000000000000000000000000001013
UPGRADE=0x0000000000000000000000000000000000001015
# gov.DefaultParams' minimum expedited deposit: 20,000,000 usei.
EXPEDITED_DEPOSIT=20ether
TEST_UPGRADE=vX.Y-test
TEST_MINOR_UPGRADE=vX.Y-minor-test
SUBMIT_PROPOSAL_TOPIC=$(cast keccak "SubmitProposal(uint64,address,uint8,bool)")
# The 72h max deposit period, in nanoseconds, the test upgrade's handler sets.
DEPOSIT_PERIOD_AFTER=259200000000000
TEST_BINARY=build/seid-test-upgrade

passes=0
failures=0

log() { printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
pass() { passes=$((passes + 1)); echo "PASS: $*"; }
fail() { failures=$((failures + 1)); echo "FAIL: $*"; }
check() {
  local what=$1 got=$2 want=$3
  if [ "$got" = "$want" ]; then pass "$what"; else fail "$what: got '$got', want '$want'"; fi
}

rpc() { echo "http://127.0.0.1:${PORTS[$1]}"; }
height() { cast block-number --rpc-url "$(rpc "$1")" 2>/dev/null || echo unreachable; }

# tick sends one transfer, which seals a block, and waits up to 20s for it.
tick() {
  cast send --rpc-url "$(rpc 0)" --private-key "$TICK_KEY" --legacy --gas-price "$GAS_PRICE" \
    --timeout 20 0x0000000000000000000000000000000000000001 --value 1 >/dev/null 2>&1
}

# gov_send sends a gov call from voter key $1 and prints its receipt as JSON.
gov_send() {
  local key=$1
  shift
  cast send --json --rpc-url "$(rpc 0)" --private-key "$key" --legacy --gas-price "$GAS_PRICE" \
    --gas-limit 2000000 "$GOV" "$@"
}

# proposal_id prints the id a submit receipt's SubmitProposal event carries.
proposal_id() {
  jq -r --arg topic "$SUBMIT_PROPOSAL_TOPIC" '.logs[] | select(.topics[0] == $topic) | .topics[1]' | cast to-dec
}

# pass_proposal has every voter vote yes on proposal $1 and ticks until it
# leaves the voting period.
pass_proposal() {
  local id=$1 i status
  for i in 0 1 2 3; do
    gov_send "${VOTER_KEYS[$i]}" "vote(uint64,int32)" "$id" 1 >/dev/null
  done
  for i in $(seq 1 60); do
    tick || true
    status=$(cast call --rpc-url "$(rpc 0)" "$GOV" "proposal(uint64)((uint64,int32,(uint256,uint256,uint256,uint256),uint64,uint64,uint256,uint64,uint64,bool,uint8,bytes))" "$id" |
      sed -n 's/^(\([0-9]*\), \([0-9]*\),.*/\2/p')
    if [ "$status" != 2 ]; then
      echo "$status"
      return
    fi
    sleep 1
  done
  echo timeout
}

plan_name() { cast call --rpc-url "$(rpc "$1")" "$UPGRADE" "currentPlan()((string,uint64,string))" | sed -n 's/^("\([^"]*\)", .*/\1/p'; }
applied() { cast call --rpc-url "$(rpc "$1")" "$UPGRADE" "appliedPlan(string)(uint64)" "$2"; }
max_deposit_period() {
  cast call --rpc-url "$(rpc "$1")" "$PARAMS" "param(string,string)(bytes)" gov depositparams.max_deposit_period | cast to-dec
}
block_hash() { cast block "$2" --field hash --rpc-url "$(rpc "$1")"; }

node_sh() { docker exec "sei-node-$1" sh -c "$2"; }

# restart_node stops seid on node $1, waiting up to 30s for it to exit, then
# starts it from binary $2 as step5_start_sei.sh does.
restart_node() {
  if ! node_sh "$1" "pkill -x seid; i=0; while pgrep -x seid >/dev/null; do \
      i=\$((i + 1)); [ \$i -le 30 ] || exit 1; sleep 1; done"; then
    fail "seid on node $1 did not exit within 30s"
    return 0
  fi
  node_sh "$1" "cp /sei-protocol/sei-chain/$2 \$HOME/go/bin/seid && \
    cd /sei-protocol/sei-chain && nohup \$HOME/go/bin/seid start --chain-id sei >> build/generated/logs/seid-$1.log 2>&1 &"
}

# tick_past ticks until node 0 is past block $1 or stops serving RPC. It fails
# once 30 ticks in a row leave the height unchanged.
tick_past() {
  local last now stalled=0
  last=$(height 0)
  while [ "$last" != unreachable ] && [ "$last" -le "$1" ]; do
    tick || true
    now=$(height 0)
    if [ "$now" = "$last" ]; then
      stalled=$((stalled + 1))
      if [ "$stalled" -ge 30 ]; then
        fail "no block past $last"
        return 0
      fi
    else
      stalled=0
    fi
    last=$now
  done
}

# wait_ready waits for node $1 to serve RPC, polling every 2s up to $2 times
# (default 60).
wait_ready() {
  local i
  for i in $(seq 1 "${2:-60}"); do
    if cast chain-id --rpc-url "$(rpc "$1")" >/dev/null 2>&1; then return 0; fi
    sleep 2
  done
  return 1
}

seid_running() { node_sh "$1" "pgrep -x seid >/dev/null" && echo yes || echo no; }

log "Waiting for every node to serve RPC"
# The cluster's start installs seid in each node before starting it, so a node
# serving RPC has the binary the build step copies.
for n in $(seq 0 $((NODES - 1))); do
  if ! wait_ready "$n" 150; then
    echo "node $n serves no RPC after 5 minutes; start the cluster first" >&2
    exit 1
  fi
done

log "Building the giga_test_upgrade binary in sei-node-0"
node_sh 0 "cd /sei-protocol/sei-chain && export PATH=\$PATH:/usr/local/go/bin && \
  LEDGER_ENABLED=false go build -tags 'netgo giga_test_upgrade' -o $TEST_BINARY ./cmd/seid"
node_sh 0 "cp \$HOME/go/bin/seid /sei-protocol/sei-chain/build/seid-old"

log "Scheduling $TEST_UPGRADE"
# Gov writes its genesis state at the chain's first block and refuses calls
# until then, so a transaction must produce that block first.
tick || fail "the chain produced no first block"
start=$(height 0)
target=$((start + 40))
id=$(gov_send "${VOTER_KEYS[0]}" "submitSoftwareUpgradeProposal(string,string,bool,(string,uint64,string))" \
  "upgrade" "schedules $TEST_UPGRADE" true "($TEST_UPGRADE,$target,docker-run)" --value "$EXPEDITED_DEPOSIT" | proposal_id)
echo "proposal $id plans $TEST_UPGRADE at block $target"
check "upgrade proposal passes" "$(pass_proposal "$id")" 3
deposit_period_before=$(max_deposit_period 0)
check "the max deposit period is not yet the handler's" "$([ "$deposit_period_before" != "$DEPOSIT_PERIOD_AFTER" ] && echo yes || echo no)" yes
for n in 0 1 2 3; do
  check "node $n has the plan scheduled" "$(plan_name "$n")" "$TEST_UPGRADE"
  check "node $n has the genesis max deposit period" "$(max_deposit_period "$n")" "$deposit_period_before"
done

log "Starting node 3 on the giga_test_upgrade binary before block $target"
restart_node 3 "$TEST_BINARY"
tick || true
sleep 5
check "the early binary stops" "$(seid_running 3)" no
check "the early binary logs BINARY UPDATED BEFORE TRIGGER" \
  "$(node_sh 3 "grep -q 'BINARY UPDATED BEFORE TRIGGER' build/generated/logs/seid-3.log" && echo yes || echo no)" yes
restart_node 3 build/seid-old
wait_ready 3 || fail "node 3 did not come back on the old binary"
tick || true
check "node 3 runs the old binary again" "$(seid_running 3)" yes
check "node 3 is not past the plan's height" "$([ "$(height 3)" -lt "$target" ] && echo yes || echo no)" yes

log "Ticking to block $target"
# The plan's height is reached, and the chain halts, only when a transaction
# produces that block.
tick_past $((target - 2))
tick || true
sleep 5
for n in 0 1 2 3; do
  check "node $n stopped" "$(seid_running "$n")" no
  check "node $n logged the halt" \
    "$(node_sh "$n" "grep -q 'UPGRADE \"$TEST_UPGRADE\" NEEDED at height: $target' build/generated/logs/seid-$n.log" && echo yes || echo no)" yes
  check "node $n wrote upgrade-info.json" \
    "$(node_sh "$n" "cat \$HOME/.sei/data/upgrade-info.json")" "{\"name\":\"$TEST_UPGRADE\",\"height\":$target,\"info\":\"docker-run\"}"
done

log "Restarting every validator on the giga_test_upgrade binary"
for n in 0 1 2 3; do restart_node "$n" "$TEST_BINARY"; done
for n in 0 1 2 3; do wait_ready "$n" || fail "node $n did not come back"; done
tick || fail "no block after the restart"
tick || fail "no second block after the restart"
want_hash=$(block_hash 0 "$target")
for n in 0 1 2 3; do
  check "node $n applied $TEST_UPGRADE at the halt block" "$(applied "$n" "$TEST_UPGRADE")" "$target"
  check "node $n agrees on block $target" "$(block_hash "$n" "$target")" "$want_hash"
  check "node $n has no plan" "$(plan_name "$n")" ""
  check "node $n has the handler's max deposit period" "$(max_deposit_period "$n")" "$DEPOSIT_PERIOD_AFTER"
done

log "Restarting node 3 on the old binary"
restart_node 3 build/seid-old
tick || true
sleep 5
check "the old binary stops" "$(seid_running 3)" no
check "the old binary logs the wrong binary" \
  "$(node_sh 3 "grep -q 'wrong binary: upgrade handler is missing for \"$TEST_UPGRADE\"' build/generated/logs/seid-3.log" && echo yes || echo no)" yes
restart_node 3 "$TEST_BINARY"
wait_ready 3 || fail "node 3 did not come back"

log "Cancelling a plan before its height"
tick || fail "no block before the cancel"
cancelled=$(( $(height 0) + 40 ))
id=$(gov_send "${VOTER_KEYS[0]}" "submitSoftwareUpgradeProposal(string,string,bool,(string,uint64,string))" \
  "upgrade" "schedules never-applied" true "(never-applied,$cancelled,none)" --value "$EXPEDITED_DEPOSIT" | proposal_id)
check "second upgrade proposal passes" "$(pass_proposal "$id")" 3
check "the second plan is scheduled" "$(plan_name 0)" never-applied
status=$(gov_send "${VOTER_KEYS[0]}" "submitCancelSoftwareUpgradeProposal(string,string,bool,string)" \
  "cancel" "cancels another plan" true not-scheduled --value "$EXPEDITED_DEPOSIT" 2>/dev/null | jq -r '.status // empty' 2>/dev/null || true)
check "a cancel naming another plan fails" "$status" 0x0
id=$(gov_send "${VOTER_KEYS[0]}" "submitCancelSoftwareUpgradeProposal(string,string,bool,string)" \
  "cancel" "cancels never-applied" true never-applied --value "$EXPEDITED_DEPOSIT" | proposal_id)
check "cancel proposal passes" "$(pass_proposal "$id")" 3
for n in 0 1 2 3; do check "node $n has no plan after the cancel" "$(plan_name "$n")" ""; done
tick_past $((cancelled + 1))
for n in 0 1 2 3; do
  check "node $n runs past the cancelled height" "$(seid_running "$n")" yes
  check "node $n did not apply the cancelled plan" "$(applied "$n" never-applied)" 0
done

log "Applying minor upgrade $TEST_MINOR_UPGRADE"
tick || fail "no block before the minor upgrade"
minor=$(( $(height 0) + 40 ))
id=$(gov_send "${VOTER_KEYS[0]}" "submitSoftwareUpgradeProposal(string,string,bool,(string,uint64,string))" \
  "upgrade" "schedules $TEST_MINOR_UPGRADE" true "($TEST_MINOR_UPGRADE,$minor,{\"upgradeType\":\"minor\"})" \
  --value "$EXPEDITED_DEPOSIT" | proposal_id)
check "minor upgrade proposal passes" "$(pass_proposal "$id")" 3
tick || true
sleep 5
for n in 0 1 2 3; do
  check "node $n has the minor plan scheduled" "$(plan_name "$n")" "$TEST_MINOR_UPGRADE"
  check "node $n runs its binary before the minor plan's height" "$(seid_running "$n")" yes
done
tick_past $((minor + 1))
want_hash=$(block_hash 0 "$minor")
for n in 0 1 2 3; do
  check "node $n runs past the minor plan's height" "$(seid_running "$n")" yes
  check "node $n applied $TEST_MINOR_UPGRADE at its height" "$(applied "$n" "$TEST_MINOR_UPGRADE")" "$minor"
  check "node $n agrees on block $minor" "$(block_hash "$n" "$minor")" "$want_hash"
  check "node $n has no plan after the minor upgrade" "$(plan_name "$n")" ""
done

log "$passes passed, $failures failed"
[ "$failures" -eq 0 ]
