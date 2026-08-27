#!/usr/bin/env bash
#
# Upgrade a local chain across two real binaries: seed state using modules that
# are alive on the old release, then swap in the new one and see what the
# upgrade does to it.
#
# Every other layer of upgrade testing in this repo runs one binary, and that
# binary is always the new one. So the "before" state is fake: head-of-tree
# cannot create a feegrant allowance or store an oracle vote, because the code
# that did is gone. The v6.7 handler has therefore only ever been run against
# empty feegrant and capability stores, which is precisely the condition under
# which a migration that mishandles their contents looks fine.
#
# This runs the old release for real:
#
#   v6.6.2 (feegrant, capability and oracle all live)
#     grant a fee allowance, spend it, submit oracle votes, let them tally
#     -> pass a v6.7 software-upgrade proposal
#     -> the old binary has no v6.7 handler, so it halts and writes upgrade-info.json
#   swap the binary
#   head (v6.7: feegrant and capability removed, oracle handlers deprecated)
#     -> applies the upgrade against state those modules actually wrote
#
# Then it asks what changed: does the chain still produce blocks, are the module
# versions gone, is the seeded state still on disk, and does a transaction that
# succeeded before the upgrade now fail?
#
# Uses an isolated home under build/. Never touches ~/.sei.
#
# Usage:
#   scripts/cross_version_upgrade_test.sh [--old-version v6.6.2] [--plan v6.7] [--keep]
#
#   --old-version  git tag to build the pre-upgrade binary from (default v6.6.2)
#   --plan         governance plan name to upgrade to (default v6.7)
#   --keep         leave the chain running and the home dir in place
#   --skip-build   reuse both binaries if already present

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

OLD_VERSION="v6.6.2"
PLAN="v6.7"
KEEP=0
SKIP_BUILD=0
while [ $# -gt 0 ]; do
    case "$1" in
        --old-version) OLD_VERSION="$2"; shift 2 ;;
        --plan) PLAN="$2"; shift 2 ;;
        --keep) KEEP=1; shift ;;
        --skip-build) SKIP_BUILD=1; shift ;;
        -h|--help) sed -n '2,38p' "$0"; exit 0 ;;
        *) echo "unknown argument: $1" >&2; exit 64 ;;
    esac
done

# The module versions $PLAN deletes, as a space-separated set. Kept explicit so
# that a release which removes another module fails here until someone states
# the new expectation, rather than the test quietly accepting whatever it finds.
EXPECTED_REMOVED=${CROSS_UPGRADE_EXPECTED_REMOVED:-"capability feegrant ibc transfer"}

WORK="$REPO_ROOT/build/cross-upgrade"
CHAIN_HOME="$WORK/home"
SHIM_DIR="$WORK/bin"
LOG="$WORK/seid.log"
OUT="$WORK/out"
CHAIN_ID="sei-cross-upgrade"
OLD_BIN="$REPO_ROOT/build/old/seid-$OLD_VERSION"
NEW_BIN="$REPO_ROOT/build/seid"

# The old release is checked out *outside* the repository. A second Go source
# tree anywhere under the repo root gets picked up by `golangci-lint fmt`, whose
# formatters take no path exclusions, so an old release's files would fail
# `make fmtcheck` for everyone. The compiled binary can stay in build/ because
# the formatters only read source.
OLD_SRC_ROOT=${CROSS_UPGRADE_SRC_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/sei-chain/oldsrc}
OLD_SRC="$OLD_SRC_ROOT/$OLD_VERSION"

# Which binary the shim currently points at. Every helper below calls a bare
# `seid`, so the swap is a single file rewrite rather than a variable every call
# site has to remember to use.
ACTIVE_BIN=""

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
note() { printf '    %s\n' "$1"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$1" >&2; exit 1; }

use_binary() {
    ACTIVE_BIN="$1"
    mkdir -p "$SHIM_DIR"
    cat > "$SHIM_DIR/seid" <<SHIM
#!/bin/sh
exec "$ACTIVE_BIN" --home "$CHAIN_HOME" "\$@"
SHIM
    chmod +x "$SHIM_DIR/seid"
}

node_pid() { pgrep -f "seid-.* start --home $CHAIN_HOME|seid start --home $CHAIN_HOME" || true; }

stop_node() {
    local pid
    pid=$(node_pid)
    [ -n "$pid" ] && kill $pid 2>/dev/null
    for _ in $(seq 1 30); do
        [ -z "$(node_pid)" ] && return 0
        sleep 1
    done
    [ -n "$(node_pid)" ] && kill -9 $(node_pid) 2>/dev/null
    sleep 1
    return 0
}

cleanup() {
    if [ "$KEEP" = 1 ]; then
        note "--keep: node left running against $CHAIN_HOME"
        return
    fi
    stop_node
}
trap cleanup EXIT

start_node() {
    "$ACTIVE_BIN" start --home "$CHAIN_HOME" --chain-id "$CHAIN_ID" >> "$LOG" 2>&1 &
    # Detach from job control so the expected kill at the upgrade height does not
    # print a "Terminated" line into the middle of the report.
    disown
    for _ in $(seq 1 90); do
        if seid status 2>/dev/null | jq -e '.SyncInfo.latest_block_height' >/dev/null 2>&1; then
            return 0
        fi
        [ -z "$(node_pid)" ] && return 1
        sleep 1
    done
    return 1
}

height() { seid status 2>/dev/null | jq -r '.SyncInfo.latest_block_height' 2>/dev/null; }

# Returns 0 when the target height is reached, 2 when the node died first.
wait_for_height() {
    local target="$1" deadline=$((SECONDS + ${2:-180}))
    while [ "$SECONDS" -lt "$deadline" ]; do
        [ -z "$(node_pid)" ] && return 2
        local h; h=$(height)
        if [[ "$h" =~ ^[0-9]+$ ]] && [ "$h" -ge "$target" ]; then return 0; fi
        sleep 1
    done
    return 1
}

wait_blocks() { local h; h=$(height); wait_for_height $((h + ${1:-1})) 60; }

tx() { printf '12345678\n' | seid tx "$@" --keyring-backend test --chain-id "$CHAIN_ID" \
    --fees 200000usei --gas 2000000 -b sync -y --output json 2>&1; }

# `seid export` writes startup logs to stdout ahead of the genesis document, so
# the JSON has to be cut out of the stream rather than read from line one.
export_genesis() { # export_genesis <binary> <outfile>
    local bin="$1" out="$2"
    "$bin" export --home "$CHAIN_HOME" --chain-id "$CHAIN_ID" 2>"$out.err" \
        | sed -n '/^{/,$p' > "$out"
    jq -e . "$out" >/dev/null 2>&1
}

# ---------------------------------------------------------------------------
step "Building both binaries"
# ---------------------------------------------------------------------------
mkdir -p "$WORK" "$OUT" "$(dirname "$OLD_BIN")"

if [ "$SKIP_BUILD" = 1 ] && [ -x "$NEW_BIN" ]; then
    note "reusing $NEW_BIN"
else
    make build >/dev/null || fail "could not build the current binary"
fi
[ -x "$NEW_BIN" ] || fail "no binary at $NEW_BIN"

# A binary built here links libwasmvm by an rpath into the source tree it was
# built from, so moving or deleting that tree leaves the binary unable to start.
# Treat an unrunnable binary as absent rather than reusing it, or the failure
# surfaces later as a confusing "this release cannot grant fee allowances".
if [ -x "$OLD_BIN" ] && ! "$OLD_BIN" version >/dev/null 2>&1; then
    note "$OLD_BIN cannot start (its source tree moved); rebuilding"
    rm -f "$OLD_BIN"
fi

if [ -x "$OLD_BIN" ] && [ "$SKIP_BUILD" = 1 ]; then
    note "reusing $OLD_BIN"
else
    if [ ! -d "$OLD_SRC" ]; then
        note "checking out $OLD_VERSION into $OLD_SRC"
        mkdir -p "$OLD_SRC_ROOT"
        git worktree add "$OLD_SRC" "$OLD_VERSION" >/dev/null 2>&1 \
            || fail "could not create a worktree at $OLD_VERSION"
    fi
    note "building $OLD_VERSION (a few minutes the first time)"
    ( cd "$OLD_SRC" && go build -o "$OLD_BIN" ./cmd/seid ) >/dev/null 2>&1 \
        || fail "could not build $OLD_VERSION"
fi
[ -x "$OLD_BIN" ] || fail "no binary at $OLD_BIN"

# The whole test rests on the two binaries differing in the right way: the old
# one must be able to grant a fee allowance and the new one must not.
"$OLD_BIN" version >/dev/null 2>&1 \
    || fail "$OLD_BIN will not start: $("$OLD_BIN" version 2>&1 | head -2 | tr '\n' ' ')"
"$OLD_BIN" tx feegrant --help 2>&1 | grep -q "Grant Fee allowance" \
    || fail "$OLD_VERSION cannot grant fee allowances, so it cannot seed the state this test needs"
"$NEW_BIN" tx feegrant --help 2>&1 | grep -q "Grant Fee allowance" \
    && fail "the current binary still has the feegrant CLI; there is nothing removed to test"
note "old binary grants fee allowances, current binary does not"

# ---------------------------------------------------------------------------
step "Initialising a chain with $OLD_VERSION"
# ---------------------------------------------------------------------------
stop_node
rm -rf "$CHAIN_HOME"
mkdir -p "$CHAIN_HOME"
: > "$LOG"
use_binary "$OLD_BIN"
export PATH="$SHIM_DIR:$PATH"

seid init cross-upgrade --chain-id "$CHAIN_ID" --overwrite >/dev/null 2>&1
seid config keyring-backend test >/dev/null 2>&1
seid config chain-id "$CHAIN_ID" >/dev/null 2>&1

for key in admin granter grantee; do
    seid keys add "$key" --keyring-backend test >/dev/null 2>&1 || fail "could not create key $key"
done
ADMIN=$(seid keys show admin -a --keyring-backend test)
GRANTER=$(seid keys show granter -a --keyring-backend test)
GRANTEE=$(seid keys show grantee -a --keyring-backend test)
VALOPER=$(seid keys show admin --bech val -a --keyring-backend test)

seid add-genesis-account "$ADMIN" 1000000000000000000usei --keyring-backend test >/dev/null
seid add-genesis-account "$GRANTER" 1000000000000000usei --keyring-backend test >/dev/null
# Deliberately small: the grantee must not be able to pay its own fees, so a
# transaction that only works because of the allowance is unambiguous.
seid add-genesis-account "$GRANTEE" 100000usei --keyring-backend test >/dev/null
seid gentx admin 70000000000000usei --chain-id "$CHAIN_ID" --keyring-backend test >/dev/null 2>&1

GENESIS="$CHAIN_HOME/config/genesis.json"
VAL_KEY=$(jq '.pub_key' "$CHAIN_HOME/config/priv_validator_key.json" -c)
jq --argjson k "$VAL_KEY" '.validators = [{"power":"70000000","pub_key":$k}]' "$GENESIS" > "$GENESIS.t" && mv "$GENESIS.t" "$GENESIS"
seid collect-gentxs >/dev/null 2>&1

jq '
  .app_state.gov.deposit_params.max_deposit_period = "60s"
  | .app_state.gov.voting_params.voting_period = "12s"
  | .app_state.gov.voting_params.expedited_voting_period = "6s"
  | .app_state.gov.tally_params.quorum = "0.100000000000000000"
  | .app_state.gov.tally_params.expedited_quorum = "0.100000000000000000"
  | .app_state.oracle.params.vote_period = "2"
  | .app_state.oracle.params.whitelist = [{"name":"uatom"},{"name":"ueth"}]
  | .app_state.distribution.params.community_tax = "0.000000000000000000"
  | .consensus_params.block.max_gas = "35000000"
  | .consensus_params.block.max_gas_wanted = "50000000"
  | .consensus_params.timeout.commit = "500000000"
' "$GENESIS" > "$GENESIS.t" && mv "$GENESIS.t" "$GENESIS"

APP_TOML="$CHAIN_HOME/config/app.toml"
sed -i.bak -e 's/^sc-enable = .*/sc-enable = true/' -e 's/^ss-enable = .*/ss-enable = true/' "$APP_TOML"
CONFIG_TOML="$CHAIN_HOME/config/config.toml"
sed -i.bak -e 's/^mode = "full"/mode = "validator"/' -e 's/^indexer = \["null"\]/indexer = ["kv"]/' "$CONFIG_TOML"
grep -q '^mode = "validator"' "$CONFIG_TOML" || fail "could not switch the node to validator mode"

# Genesis written by the old binary must carry the sections for the modules the
# upgrade removes, or there is nothing to seed into.
for section in feegrant capability oracle; do
    jq -e --arg s "$section" '.app_state[$s]' "$GENESIS" >/dev/null \
        || fail "$OLD_VERSION genesis has no $section section"
done
note "genesis carries feegrant, capability and oracle sections"

start_node || fail "the $OLD_VERSION node did not start; see $LOG"
wait_for_height 3 90 >/dev/null || fail "no blocks from $OLD_VERSION; see $LOG"
note "running $OLD_VERSION at height $(height)"

# ---------------------------------------------------------------------------
step "Seeding state with modules that are alive on $OLD_VERSION"
# ---------------------------------------------------------------------------

# --- feegrant: a real allowance, then actually spend it ---
RESP=$(tx feegrant grant "$GRANTER" "$GRANTEE" --spend-limit 100000000usei --from granter)
[ "$(echo "$RESP" | jq -r '.code // 1')" = "0" ] \
    || fail "could not grant a fee allowance: $(echo "$RESP" | jq -r '.raw_log // .')"
wait_blocks 2 >/dev/null

GRANT=$(seid q feegrant grant "$GRANTER" "$GRANTEE" -o json 2>&1)
echo "$GRANT" | jq -e '.allowance' >/dev/null 2>&1 \
    || fail "the allowance is not in state after granting: $GRANT"
note "feegrant allowance $GRANTER -> $GRANTEE is in state"

# The grantee has almost no balance, so this can only pay its fee through the
# allowance. Succeeding here is what proves the grant is live, and it is the
# behaviour the upgrade takes away.
GRANTEE_BAL_BEFORE=$(seid q bank balances "$GRANTEE" --denom usei -o json 2>/dev/null | jq -r '.amount')
RESP=$(tx bank send "$GRANTEE" "$ADMIN" 1usei --fee-account "$GRANTER" --from grantee)
[ "$(echo "$RESP" | jq -r '.code // 1')" = "0" ] \
    || fail "a fee-granted send was rejected on $OLD_VERSION: $(echo "$RESP" | jq -r '.raw_log // .')"
wait_blocks 2 >/dev/null
GRANTEE_BAL_AFTER=$(seid q bank balances "$GRANTEE" --denom usei -o json 2>/dev/null | jq -r '.amount')
note "fee-granted send accepted on $OLD_VERSION (grantee balance $GRANTEE_BAL_BEFORE -> $GRANTEE_BAL_AFTER)"

# --- oracle: real votes, tallied into exchange rates ---
for i in 1 2 3; do
    tx oracle aggregate-vote "1.${i}uatom,2.${i}ueth" "$VALOPER" --from admin >/dev/null
    wait_blocks 2 >/dev/null
done
RATES=$(seid q oracle exchange-rates -o json 2>&1)
echo "$RATES" | jq -e '.denom_oracle_exchange_rate_pairs | length > 0' >/dev/null 2>&1 \
    || fail "oracle votes did not tally into exchange rates on $OLD_VERSION: $RATES"
note "oracle exchange rates present: $(echo "$RATES" | jq -c '[.denom_oracle_exchange_rate_pairs[].denom]')"

# ---------------------------------------------------------------------------
step "Recording what $OLD_VERSION holds"
# ---------------------------------------------------------------------------
seid q feegrant grant "$GRANTER" "$GRANTEE" -o json > "$OUT/old-grant.json" 2>&1
seid q oracle exchange-rates -o json > "$OUT/old-rates.json" 2>&1
OLD_MODULE_VERSIONS=$(seid q upgrade module_versions -o json 2>/dev/null | jq -r '[.module_versions[].name] | sort | join(" ")')
echo "$OLD_MODULE_VERSIONS" > "$OUT/old-modules.txt"
note "module versions: $OLD_MODULE_VERSIONS"
for m in feegrant capability oracle; do
    echo "$OLD_MODULE_VERSIONS" | grep -qw "$m" || fail "$m has no module version on $OLD_VERSION"
done

# ---------------------------------------------------------------------------
step "Passing a software-upgrade proposal for $PLAN"
# ---------------------------------------------------------------------------
TARGET=$(( $(height) + 25 ))
RESP=$(tx gov submit-proposal software-upgrade "$PLAN" --title "$PLAN" \
    --description "cross version upgrade test" --upgrade-height "$TARGET" \
    --deposit 20000000usei --is-expedited --from admin)
[ "$(echo "$RESP" | jq -r '.code // 1')" = "0" ] \
    || fail "could not submit the proposal: $(echo "$RESP" | jq -r '.raw_log // .')"
sleep 3
PROPOSAL=$(seid q gov proposals --reverse --limit 1 -o json 2>/dev/null | jq -r '.proposals[0].proposal_id // .proposals[0].id')
[ -n "$PROPOSAL" ] && [ "$PROPOSAL" != "null" ] || fail "no proposal in gov state"
tx gov vote "$PROPOSAL" yes --from admin >/dev/null

STATUS=""
for _ in $(seq 1 40); do
    STATUS=$(seid q gov proposal "$PROPOSAL" -o json 2>/dev/null | jq -r '.status // ""')
    [ "$STATUS" = "PROPOSAL_STATUS_PASSED" ] && break
    case "$STATUS" in
        PROPOSAL_STATUS_REJECTED|PROPOSAL_STATUS_FAILED) fail "proposal reached $STATUS" ;;
    esac
    sleep 1
done
[ "$STATUS" = "PROPOSAL_STATUS_PASSED" ] || fail "proposal did not pass (last status ${STATUS:-unknown})"
note "proposal $PROPOSAL passed, targeting height $TARGET"

# ---------------------------------------------------------------------------
step "Letting $OLD_VERSION halt at $TARGET"
# ---------------------------------------------------------------------------
# The old binary has no handler for $PLAN, so it must stop rather than produce
# the block. This is the halt a validator on the old release sees.
wait_for_height "$TARGET" 240
case $? in
    2) note "halted, as a node without the $PLAN handler must" ;;
    0) fail "$OLD_VERSION produced block $TARGET without a $PLAN handler" ;;
    *) fail "$OLD_VERSION neither halted nor reached $TARGET; see $LOG" ;;
esac
grep -q "UPGRADE \"$PLAN\" NEEDED" "$LOG" || fail "the halt was not an upgrade panic; see $LOG"
[ -f "$CHAIN_HOME/data/upgrade-info.json" ] || fail "no upgrade-info.json was written"
note "upgrade-info.json: $(cat "$CHAIN_HOME/data/upgrade-info.json")"

# With the node down, export the state the old binary understands. This is the
# only chance to capture it with a binary that still has these modules.
step "Exporting state with $OLD_VERSION while it is stopped"
export_genesis "$OLD_BIN" "$OUT/pre-upgrade-by-old.json" \
    || fail "could not export with $OLD_VERSION: $(head -1 "$OUT/pre-upgrade-by-old.json.err")"
jq -e '.app_state.feegrant.allowances | length > 0' "$OUT/pre-upgrade-by-old.json" >/dev/null \
    || fail "the pre-upgrade export carries no feegrant allowance, so nothing was seeded"
note "feegrant: $(jq -c '.app_state.feegrant.allowances[0].allowance.spend_limit' "$OUT/pre-upgrade-by-old.json")"
note "capability: $(jq -c '.app_state.capability | {index, owners: (.owners | length)}' "$OUT/pre-upgrade-by-old.json")"
note "oracle rates: $(jq -c '[.app_state.oracle.exchange_rates[] | .denom + "=" + .exchange_rate]' "$OUT/pre-upgrade-by-old.json")"

# ---------------------------------------------------------------------------
step "Swapping in the current binary and restarting"
# ---------------------------------------------------------------------------
use_binary "$NEW_BIN"
start_node || fail "the new binary did not start; see $LOG"
wait_for_height $((TARGET + 3)) 180 || fail "the new binary did not advance past $TARGET; see $LOG"
note "advanced to height $(height) on the current binary"
grep -q "BINARY UPDATED BEFORE TRIGGER" "$LOG" \
    && fail "the new binary refused the chain state; see $LOG"

APPLIED=$(seid q upgrade applied "$PLAN" 2>/dev/null | jq -r '.header.height // 0')
[[ "$APPLIED" =~ ^[0-9]+$ ]] && [ "$APPLIED" -gt 0 ] \
    || fail "the chain does not report $PLAN as applied, so the handler never ran"
note "$PLAN applied at height $APPLIED"

# ---------------------------------------------------------------------------
step "What the upgrade did to the seeded state"
# ---------------------------------------------------------------------------
FAILURES=0
check() { # check <description> <condition-exit-code>
    if [ "$2" = 0 ]; then printf '    \033[32mok\033[0m   %s\n' "$1"
    else printf '    \033[31mBAD\033[0m  %s\n' "$1"; FAILURES=$((FAILURES + 1)); fi
}

NEW_MODULE_VERSIONS=$(seid q upgrade module_versions -o json 2>/dev/null | jq -r '[.module_versions[].name] | sort | join(" ")')
note "module versions now: $NEW_MODULE_VERSIONS"

for m in $EXPECTED_REMOVED; do
    echo "$NEW_MODULE_VERSIONS" | grep -qw "$m"
    [ $? = 1 ]; check "$m module version removed" $?
done
echo "$NEW_MODULE_VERSIONS" | grep -qw oracle
check "oracle keeps its module version (deprecated, not removed)" $?

REMOVED=$(comm -23 <(tr ' ' '\n' <<<"$OLD_MODULE_VERSIONS" | sort -u) <(tr ' ' '\n' <<<"$NEW_MODULE_VERSIONS" | sort -u) | tr '\n' ' ' | xargs)
WANT=$(tr ' ' '\n' <<<"$EXPECTED_REMOVED" | sort -u | tr '\n' ' ' | xargs)
[ "$REMOVED" = "$WANT" ]
check "exactly the expected modules were removed (want: $WANT / got: ${REMOVED:-none})" $?

# The behaviour change a user sees: the same fee-granted send that worked before
# the upgrade must now be refused, and refused for a stated reason.
RESP=$(tx bank send "$GRANTEE" "$ADMIN" 1usei --fee-account "$GRANTER" --from grantee)
echo "$RESP" | grep -q "fee grants are not enabled"
check "the fee-granted send that worked before is now refused: fee grants are not enabled" $?

# Oracle rates seeded on the old binary are no longer queryable.
RATES_NOW=$(seid q oracle exchange-rates -o json 2>&1)
echo "$RATES_NOW" | grep -q "oracle module is deprecated"
check "oracle exchange rates are no longer queryable (handler deprecated)" $?

# ...and the chain is still producing blocks with all of that behind it.
wait_blocks 3 >/dev/null
check "chain still producing blocks after the upgrade" $?

step "Where the seeded state ended up"
stop_node

# Three exports answer the question the release notes raise. The new binary
# cannot emit a section for a module it no longer registers, so its export
# alone cannot distinguish "kept on disk but unreachable" from "deleted". The
# old binary still has the feegrant and capability modules, so pointing it at
# the upgraded state reads whatever the upgrade left behind.
export_genesis "$NEW_BIN" "$OUT/post-upgrade-by-new.json" \
    || fail "could not export with the current binary: $(head -1 "$OUT/post-upgrade-by-new.json.err")"
export_genesis "$OLD_BIN" "$OUT/post-upgrade-by-old.json" \
    || note "the old binary could not read the upgraded state: $(head -1 "$OUT/post-upgrade-by-old.json.err")"

for section in feegrant capability; do
    jq -e --arg s "$section" '.app_state[$s]' "$OUT/post-upgrade-by-new.json" >/dev/null 2>&1
    [ $? = 1 ]; check "$section is absent from the export the current binary produces" $?
done

# Oracle is deprecated rather than removed, so it is still a registered module
# and its state still exports. That contrast is the point: deprecating a module
# and removing one leave state in very different places.
jq -e '.app_state.oracle.exchange_rates | length > 0' "$OUT/post-upgrade-by-new.json" >/dev/null 2>&1
check "oracle rates seeded on $OLD_VERSION still export after the upgrade" $?

if jq -e . "$OUT/post-upgrade-by-old.json" >/dev/null 2>&1; then
    RETAINED=$(jq -r '.app_state.feegrant.allowances | length' "$OUT/post-upgrade-by-old.json" 2>/dev/null)
    if [ "${RETAINED:-0}" -gt 0 ]; then
        check "the allowance survives on disk: $OLD_VERSION reads it back out of the upgraded state" 0
        note "spend limit before: $(jq -c '.app_state.feegrant.allowances[0].allowance.spend_limit' "$OUT/pre-upgrade-by-old.json")"
        note "spend limit after:  $(jq -c '.app_state.feegrant.allowances[0].allowance.spend_limit' "$OUT/post-upgrade-by-old.json")"
        note "no code on the current binary can reach it, and no export carries it forward"
    else
        check "the upgrade dropped the feegrant allowance from the store" 1
        note "the stores are documented as retained for historical access, so an empty"
        note "  read here contradicts that and is application-hash relevant"
    fi
fi

# ---------------------------------------------------------------------------
step "Result"
# ---------------------------------------------------------------------------
note "artefacts in $OUT"
if [ "$FAILURES" -gt 0 ]; then
    fail "$FAILURES check(s) did not hold across the $OLD_VERSION -> $PLAN upgrade"
fi
printf '\n\033[32mPASS\033[0m  %s -> %s applied at height %s against state the removed modules actually wrote.\n' \
    "$OLD_VERSION" "$PLAN" "$APPLIED"
