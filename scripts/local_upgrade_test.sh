#!/usr/bin/env bash
#
# Drive a real governance software upgrade on a single-node local chain, and
# compare how the chain answers for the modules the upgrade retires on either
# side of it.
#
# This is the cheapest layer that reaches the real upgrade machinery. The
# in-process Go tests call ApplyUpgrade directly, which never writes
# upgrade-info.json, never panics, and never reloads the stores, so the code
# that decides whether a store is dropped at an upgrade height
# (App.SetStoreUpgradeHandlers) is not reached by any of them. The docker suite
# does reach it but needs a cluster. This reaches it with one node on a laptop:
#
#   init -> start on a version list without the plan -> probe -> pass a
#   software-upgrade proposal -> node panics at the height -> restart with the
#   plan in the list, which is what runs SetStoreUpgradeHandlers and the upgrade
#   handler -> probe -> require both probes to agree.
#
# Note what this does NOT do: both processes are this binary, so the modules the
# upgrade retires are already gone before the chain starts and the pre-upgrade
# state is empty. Use scripts/cross_version_upgrade_test.sh when the point is to
# seed state with modules that were alive on the previous release.
#
# Everything lives under an isolated home directory. It never touches ~/.sei.
#
# Usage:
#   scripts/local_upgrade_test.sh [--version v6.7] [--keep] [--skip-build]

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="v6.7"
KEEP=0
SKIP_BUILD=0
while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        --keep) KEEP=1; shift ;;
        --skip-build) SKIP_BUILD=1; shift ;;
        -h|--help) sed -n '2,27p' "$0"; exit 0 ;;
        *) echo "unknown argument: $1" >&2; exit 64 ;;
    esac
done

# The version list the node starts on. It deliberately excludes $VERSION so the
# first process does not know the upgrade: that is what makes it panic at the
# height and write upgrade-info.json, exactly as a validator on an old release
# does. Overridable so the halt guard itself can be exercised — starting with
# $VERSION already present must be reported, not accepted.
BASE_VERSION_LIST=${LOCAL_UPGRADE_BASE_LIST:-v1.0.0}

# Modules $VERSION removes from the module version map. Each must answer
# not-found afterwards; one that survives means the handler is missing a
# DeleteModuleVersion call for it.
RETIRED_MODULES=${LOCAL_UPGRADE_RETIRED_MODULES:-"capability feegrant ibc transfer"}

WORK_DIR="$REPO_ROOT/build/generated/local-upgrade"
CHAIN_HOME="$WORK_DIR/home"
SHIM_DIR="$WORK_DIR/bin"
LOG="$WORK_DIR/seid.log"
CHAIN_ID="sei-local-upgrade"
ADMIN_KEY="admin"
GRANTER_KEY="granter"
SEID="$REPO_ROOT/build/seid"

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
note() { printf '    %s\n' "$1"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$1" >&2; exit 1; }

node_pid() { pgrep -f "seid start --home $CHAIN_HOME" || true; }

stop_node() {
    local pid
    pid=$(node_pid)
    [ -n "$pid" ] && kill $pid 2>/dev/null
    for _ in $(seq 1 30); do
        [ -z "$(node_pid)" ] && return 0
        sleep 1
    done
    [ -n "$(node_pid)" ] && kill -9 $(node_pid) 2>/dev/null
    return 0
}

cleanup() {
    if [ "$KEEP" = 1 ]; then
        note "--keep: node left running against $CHAIN_HOME"
        note "  stop it with: pkill -f 'seid start --home $CHAIN_HOME'"
        return
    fi
    stop_node
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
step "Building seid"
# ---------------------------------------------------------------------------
if [ "$SKIP_BUILD" = 1 ] && [ -x "$SEID" ]; then
    note "reusing $SEID"
else
    make build >/dev/null || fail "make build"
fi
[ -x "$SEID" ] || fail "no binary at $SEID"

# A shim on PATH that pins --home, so the probe script and the helpers it
# sources can call a bare `seid` without knowing about this chain's home. This
# is the same indirection the in-process YAML runner uses.
mkdir -p "$SHIM_DIR"
cat > "$SHIM_DIR/seid" <<SHIM
#!/bin/sh
exec "$SEID" --home "$CHAIN_HOME" "\$@"
SHIM
chmod +x "$SHIM_DIR/seid"
export PATH="$SHIM_DIR:$PATH"

# ---------------------------------------------------------------------------
step "Initialising an isolated chain at $CHAIN_HOME"
# ---------------------------------------------------------------------------
stop_node
rm -rf "$CHAIN_HOME"
mkdir -p "$CHAIN_HOME" "$(dirname "$LOG")"

# Truncate rather than append. The halt check greps this log for the upgrade
# panic, and a previous run's panic left in place would let a run that never
# halted report success.
: > "$LOG"

seid init local-upgrade --chain-id "$CHAIN_ID" --overwrite >/dev/null 2>&1
seid config keyring-backend test >/dev/null 2>&1
seid config chain-id "$CHAIN_ID" >/dev/null 2>&1

for key in "$ADMIN_KEY" "$GRANTER_KEY"; do
    seid keys add "$key" --keyring-backend test >/dev/null 2>&1 \
        || fail "could not create key $key"
done
ADMIN_ADDR=$(seid keys show "$ADMIN_KEY" -a --keyring-backend test)
GRANTER_ADDR=$(seid keys show "$GRANTER_KEY" -a --keyring-backend test)

seid add-genesis-account "$ADMIN_ADDR" 1000000000000000000usei --keyring-backend test >/dev/null
seid add-genesis-account "$GRANTER_ADDR" 1000000000000usei --keyring-backend test >/dev/null
seid gentx "$ADMIN_KEY" 70000000000000usei --chain-id "$CHAIN_ID" --keyring-backend test >/dev/null 2>&1

# Pin the single validator into genesis, matching scripts/initialize_local_chain.sh.
GENESIS="$CHAIN_HOME/config/genesis.json"
VAL_KEY=$(jq '.pub_key' "$CHAIN_HOME/config/priv_validator_key.json" -c)
jq --argjson k "$VAL_KEY" '.validators = [{"power":"70000000","pub_key":$k}]' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"
seid collect-gentxs >/dev/null 2>&1

# Governance has to resolve in seconds, not days, for this to be a local test.
jq '
  .app_state.gov.deposit_params.max_deposit_period = "60s"
  | .app_state.gov.voting_params.voting_period = "12s"
  | .app_state.gov.voting_params.expedited_voting_period = "6s"
  | .app_state.gov.tally_params.quorum = "0.100000000000000000"
  | .app_state.gov.tally_params.expedited_quorum = "0.100000000000000000"
  | .app_state.distribution.params.community_tax = "0.000000000000000000"
  | .consensus_params.block.max_gas = "35000000"
  | .consensus_params.block.max_gas_wanted = "50000000"
  | .consensus_params.timeout.commit = "500000000"
' "$GENESIS" > "$GENESIS.tmp" && mv "$GENESIS.tmp" "$GENESIS"

APP_TOML="$CHAIN_HOME/config/app.toml"
sed -i.bak -e 's/^sc-enable = .*/sc-enable = true/' -e 's/^ss-enable = .*/ss-enable = true/' "$APP_TOML"
sed -i.bak -e 's/^occ-enabled = .*/occ-enabled = true/' "$APP_TOML"

# A lone validator has no peers to block-sync from, so in the default "full"
# mode it waits for a peer height that never arrives and never produces a block.
CONFIG_TOML="$CHAIN_HOME/config/config.toml"
sed -i.bak -e 's/^mode = "full"/mode = "validator"/' "$CONFIG_TOML"
sed -i.bak -e 's/^indexer = \["null"\]/indexer = ["kv"]/' "$CONFIG_TOML"
grep -q '^mode = "validator"' "$CONFIG_TOML" || fail "could not switch the node to validator mode"

start_node() {
    local version_list="$1"
    UPGRADE_VERSION_LIST="$version_list" \
        "$SEID" start --home "$CHAIN_HOME" --chain-id "$CHAIN_ID" >> "$LOG" 2>&1 &
    disown
    for _ in $(seq 1 60); do
        if seid status 2>/dev/null | jq -e '.SyncInfo.latest_block_height' >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

height() { seid status 2>/dev/null | jq -r '.SyncInfo.latest_block_height' 2>/dev/null; }

wait_for_height() {
    local target="$1" deadline=$((SECONDS + ${2:-120}))
    while [ "$SECONDS" -lt "$deadline" ]; do
        local h; h=$(height)
        [ -z "$(node_pid)" ] && return 2   # the node died, which may be the point
        if [[ "$h" =~ ^[0-9]+$ ]] && [ "$h" -ge "$target" ]; then return 0; fi
        sleep 1
    done
    return 1
}

# ---------------------------------------------------------------------------
step "Starting the node without $VERSION in its version list"
# ---------------------------------------------------------------------------
start_node "$BASE_VERSION_LIST" || fail "node did not start; see $LOG"
wait_for_height 3 60 || fail "node did not reach height 3; see $LOG"
note "running at height $(height) on UPGRADE_VERSION_LIST=$BASE_VERSION_LIST"

# ---------------------------------------------------------------------------
step "Probing retired modules BEFORE the upgrade"
# ---------------------------------------------------------------------------
PROBE=integration_test/upgrade_module/scripts/probe_retired_modules.sh
BEFORE=$(CHAIN_ID="$CHAIN_ID" ADMIN_KEY="$ADMIN_KEY" bash "$PROBE" "$GRANTER_ADDR" 3)
echo "$BEFORE"
echo "$BEFORE" | grep -q '^PROBE_RESULT=ALL_MATCH$' \
    || fail "the pre-upgrade probe did not get the answers it expected, so the comparison below would be meaningless"

# ---------------------------------------------------------------------------
step "Passing a software-upgrade proposal for $VERSION"
# ---------------------------------------------------------------------------
TARGET_HEIGHT=$(( $(height) + 25 ))
seid tx gov submit-proposal software-upgrade "$VERSION" \
    --title "$VERSION" --description "local upgrade test" \
    --upgrade-height "$TARGET_HEIGHT" --deposit 20000000usei --is-expedited \
    --from "$ADMIN_KEY" --keyring-backend test --chain-id "$CHAIN_ID" \
    --fees 200000usei -b sync -y --output json >/dev/null 2>&1 \
    || fail "could not submit the upgrade proposal"

sleep 3
PROPOSAL_ID=$(seid q gov proposals --reverse --limit 1 -o json 2>/dev/null | jq -r '.proposals[0].proposal_id // .proposals[0].id')
[ -n "$PROPOSAL_ID" ] && [ "$PROPOSAL_ID" != "null" ] || fail "no proposal appeared in gov state"
note "proposal $PROPOSAL_ID targets height $TARGET_HEIGHT"

seid tx gov vote "$PROPOSAL_ID" yes --from "$ADMIN_KEY" --keyring-backend test \
    --chain-id "$CHAIN_ID" --fees 200000usei -b sync -y --output json >/dev/null 2>&1 \
    || fail "could not vote"

STATUS=""
for _ in $(seq 1 40); do
    STATUS=$(seid q gov proposal "$PROPOSAL_ID" -o json 2>/dev/null | jq -r '.status // ""')
    [ "$STATUS" = "PROPOSAL_STATUS_PASSED" ] && break
    case "$STATUS" in
        PROPOSAL_STATUS_REJECTED|PROPOSAL_STATUS_FAILED) fail "proposal reached $STATUS" ;;
    esac
    sleep 1
done
[ "$STATUS" = "PROPOSAL_STATUS_PASSED" ] || fail "proposal did not pass (last status ${STATUS:-unknown})"
note "proposal passed"

# ---------------------------------------------------------------------------
step "Letting the node reach $TARGET_HEIGHT without the upgrade"
# ---------------------------------------------------------------------------
# A node that does not know the plan name must halt here rather than produce a
# block. Continuing past it would mean the upgrade was silently skipped.
wait_for_height "$TARGET_HEIGHT" 180
case $? in
    2) note "node halted as expected" ;;
    0) fail "node produced block $TARGET_HEIGHT without knowing $VERSION; the upgrade was skipped" ;;
    *) fail "node neither halted nor reached $TARGET_HEIGHT; see $LOG" ;;
esac
grep -q "UPGRADE \"$VERSION\" NEEDED" "$LOG" \
    || fail "the halt was not an upgrade panic; see $LOG"
# A node that already knows the plan refuses to start at all, with "BINARY
# UPDATED BEFORE TRIGGER". Reaching that here would mean the version list was
# wrong and this run proved nothing about the upgrade.
grep -q "BINARY UPDATED BEFORE TRIGGER" "$LOG" \
    && fail "the node knew $VERSION before the chain executed it; start it on a version list without $VERSION"
[ -f "$CHAIN_HOME/data/upgrade-info.json" ] \
    || fail "the halted node did not write upgrade-info.json, so a restart cannot apply store upgrades"
note "upgrade-info.json: $(cat "$CHAIN_HOME/data/upgrade-info.json")"

# ---------------------------------------------------------------------------
step "Restarting with $VERSION, which runs the handler and the store upgrades"
# ---------------------------------------------------------------------------
start_node "$BASE_VERSION_LIST,$VERSION" || fail "node did not come back up; see $LOG"
wait_for_height $((TARGET_HEIGHT + 3)) 120 \
    || fail "node did not advance past the upgrade height; see $LOG"
note "advanced to height $(height)"

# `q upgrade applied` prints the block header of the height the plan ran at, so
# the height is nested. A zero or unreadable answer means the handler never ran,
# which would make every comparison below a comparison against itself.
APPLIED=$(seid q upgrade applied "$VERSION" 2>/dev/null | jq -r '.header.height // 0')
[[ "$APPLIED" =~ ^[0-9]+$ ]] && [ "$APPLIED" -gt 0 ] \
    || fail "the chain does not report $VERSION as applied, so the upgrade handler never ran"
note "applied plan $VERSION at height $APPLIED"

# ---------------------------------------------------------------------------
step "Probing retired modules AFTER the upgrade"
# ---------------------------------------------------------------------------
AFTER=$(CHAIN_ID="$CHAIN_ID" ADMIN_KEY="$ADMIN_KEY" bash "$PROBE" "$GRANTER_ADDR" 3)
echo "$AFTER"

# ---------------------------------------------------------------------------
step "Result"
# ---------------------------------------------------------------------------
echo "$AFTER" | grep -q '^PROBE_RESULT=ALL_MATCH$' \
    || fail "a retired surface stopped answering as expected after the upgrade"

if [ "$BEFORE" != "$AFTER" ]; then
    printf '\033[31mthe chain answered differently after the upgrade:\033[0m\n'
    diff <(echo "$BEFORE") <(echo "$AFTER") || true
    fail "retiring a module changed how the chain answers a client"
fi

# The modules this upgrade retires must be gone from the module version map. The
# keeper answers a named lookup for a missing module with a not-found error, so
# that error is the expected post-removal answer.
for module in $RETIRED_MODULES; do
    ANSWER=$(seid q upgrade module_versions "$module" -o json 2>&1 || true)
    case "$ANSWER" in
        *"not found"*) note "$module module version removed" ;;
        *) fail "$module still holds a module version after $VERSION: $ANSWER" ;;
    esac
done

printf '\n\033[32mPASS\033[0m  %s applied at height %s; every retired surface answered identically before and after.\n' \
    "$VERSION" "$APPLIED"
