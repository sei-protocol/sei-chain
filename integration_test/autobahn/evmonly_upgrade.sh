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
# then run this script from the repository root. It drives the chain with each
# node's own seid over its EVM JSON-RPC, and needs only docker and jq.
set -euo pipefail

NODES=4
# The test mnemonic. Anvil accounts 0 to 3 are the validators' evm_voter keys;
# account 9 sends the transfers that produce blocks, as Autobahn makes no empty
# blocks.
MNEMONIC="test test test test test test test test test test test junk"
KEY_INDEXES="0 1 2 3 9"
# The keyring, in sei-node-0, that signs every transaction.
KEY_HOME=/tmp/evmonly-upgrade-keys
EVM_RPC=http://127.0.0.1:8545
# gov.DefaultParams' minimum expedited deposit.
EXPEDITED_DEPOSIT=20000000usei
TEST_UPGRADE=vX.Y-test
TEST_MINOR_UPGRADE=vX.Y-minor-test
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

node_sh() { docker exec "sei-node-$1" sh -c "$2"; }

# seid_q runs a seid query in node $1 against that node's EVM JSON-RPC.
seid_q() {
  local n=$1
  shift
  docker exec "sei-node-$n" seid "$@" --evm-rpc "$EVM_RPC"
}

# seid_tx runs a seid transaction in node 0, signed by key $1 of KEY_HOME.
seid_tx() {
  local key=$1
  shift
  docker exec sei-node-0 seid "$@" --from "$key" --keyring-backend test --home "$KEY_HOME" --evm-rpc "$EVM_RPC"
}

# gov_tx sends a seid tx gov command from key $1 and prints its result as JSON.
gov_tx() {
  local key=$1
  shift
  seid_tx "$key" tx gov "$@" -y -o json
}

height() {
  local h
  h=$(seid_q "$1" q block 2>/dev/null | jq -r .number 2>/dev/null) || h=
  case "$h" in 0x*) echo $((h)) ;; *) echo unreachable ;; esac
}

# tick sends one transfer, which seals a block, and waits up to 20s for it.
tick() {
  local before i
  before=$(height 0)
  [ "$before" != unreachable ] || return 1
  seid_tx key9 tx evm send 0x0000000000000000000000000000000000000001 1 >/dev/null 2>&1 || return 1
  for i in $(seq 1 20); do
    [ "$(height 0)" != unreachable ] && [ "$(height 0)" -gt "$before" ] && return 0
    sleep 1
  done
  return 1
}

# proposal_id prints the id in a submit-proposal result.
proposal_id() {
  jq -r '.logs[0].events[] | select(.type == "submit_proposal") | .attributes[] | select(.key == "proposal_id") | .value'
}

# submit_upgrade submits an expedited software upgrade of plan $1 at height $2
# with info $3, and prints its proposal id.
submit_upgrade() {
  gov_tx key0 submit-proposal software-upgrade "$1" --upgrade-height "$2" --upgrade-info "$3" \
    --title upgrade --description "schedules $1" --is-expedited --deposit "$EXPEDITED_DEPOSIT" | proposal_id
}

# submit_cancel submits an expedited cancel of plan $1 and prints its result.
submit_cancel() {
  gov_tx key0 submit-proposal cancel-software-upgrade --name "$1" \
    --title cancel --description "cancels $1" --is-expedited --deposit "$EXPEDITED_DEPOSIT"
}

# pass_proposal has every voter vote yes on proposal $1 and ticks until it
# leaves the voting period, printing its final status.
pass_proposal() {
  local id=$1 i status
  for i in 0 1 2 3; do
    gov_tx "key$i" vote "$id" yes >/dev/null
  done
  for i in $(seq 1 60); do
    tick || true
    status=$(seid_q 0 q gov proposal "$id" -o json | jq -r .status)
    if [ "$status" != PROPOSAL_STATUS_VOTING_PERIOD ]; then
      echo "$status"
      return
    fi
    sleep 1
  done
  echo timeout
}

plan_name() { seid_q "$1" q upgrade plan -o json 2>/dev/null | jq -r .name || true; }
applied() {
  local h
  h=$(seid_q "$1" q upgrade applied "$2" 2>/dev/null | jq -r .number) || h=
  case "$h" in 0x*) echo $((h)) ;; *) echo 0 ;; esac
}
max_deposit_period() { seid_q "$1" q gov params -o json | jq -r .deposit_params.max_deposit_period; }
block_hash() { seid_q "$1" q block "$2" | jq -r .hash; }

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

# serving reports whether node $1 answers over its EVM JSON-RPC, which it does
# from genesis, before it has a block.
serving() {
  local out
  if out=$(seid_q "$1" q upgrade plan 2>&1); then return 0; fi
  case "$out" in *"no upgrade scheduled"*) return 0 ;; esac
  return 1
}

# wait_ready waits for node $1 to serve RPC, polling every 2s up to $2 times
# (default 60).
wait_ready() {
  local i
  for i in $(seq 1 "${2:-60}"); do
    if serving "$1"; then return 0; fi
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

log "Importing the test keys into $KEY_HOME in sei-node-0"
node_sh 0 "rm -rf $KEY_HOME"
for i in $KEY_INDEXES; do
  echo "$MNEMONIC" | docker exec -i sei-node-0 seid keys add "key$i" --recover \
    --hd-path "m/44'/60'/0'/0/$i" --keyring-backend test --home "$KEY_HOME" >/dev/null
done

log "Sending a teaser transfer so the chain leaves genesis"
# Autobahn makes no empty blocks, and a node reports no latest block before
# the first one, so a transfer produces block 1 before anything reads a block.
seid_tx key9 tx evm send 0x0000000000000000000000000000000000000001 1 >/dev/null
for i in $(seq 1 20); do
  [ "$(height 0)" != unreachable ] && break
  sleep 1
done
[ "$(height 0)" != unreachable ] || { echo "the chain produced no first block" >&2; exit 1; }

log "Building the giga_test_upgrade binary in sei-node-0"
node_sh 0 "cd /sei-protocol/sei-chain && export PATH=\$PATH:/usr/local/go/bin && \
  LEDGER_ENABLED=false go build -tags 'netgo giga_test_upgrade' -o $TEST_BINARY ./cmd/seid"
node_sh 0 "cp \$HOME/go/bin/seid /sei-protocol/sei-chain/build/seid-old"

log "Scheduling $TEST_UPGRADE"
start=$(height 0)
target=$((start + 40))
id=$(submit_upgrade "$TEST_UPGRADE" "$target" docker-run)
echo "proposal $id plans $TEST_UPGRADE at block $target"
check "upgrade proposal passes" "$(pass_proposal "$id")" PROPOSAL_STATUS_PASSED
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
cancelled=$(($(height 0) + 40))
id=$(submit_upgrade never-applied "$cancelled" none)
check "second upgrade proposal passes" "$(pass_proposal "$id")" PROPOSAL_STATUS_PASSED
check "the second plan is scheduled" "$(plan_name 0)" never-applied
# seid estimates gas before sending, so the gov precompile's refusal stops the
# cancel before any transaction is sent.
check "a cancel naming another plan fails" "$(submit_cancel not-scheduled >/dev/null 2>&1 && echo sent || echo refused)" refused
id=$(submit_cancel never-applied | proposal_id)
check "cancel proposal passes" "$(pass_proposal "$id")" PROPOSAL_STATUS_PASSED
for n in 0 1 2 3; do check "node $n has no plan after the cancel" "$(plan_name "$n")" ""; done
tick_past $((cancelled + 1))
for n in 0 1 2 3; do
  check "node $n runs past the cancelled height" "$(seid_running "$n")" yes
  check "node $n did not apply the cancelled plan" "$(applied "$n" never-applied)" 0
done

log "Applying minor upgrade $TEST_MINOR_UPGRADE"
tick || fail "no block before the minor upgrade"
minor=$(($(height 0) + 40))
id=$(submit_upgrade "$TEST_MINOR_UPGRADE" "$minor" '{"upgradeType":"minor"}')
check "minor upgrade proposal passes" "$(pass_proposal "$id")" PROPOSAL_STATUS_PASSED
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
