#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(git rev-parse --show-toplevel)"
readonly RUN_ROOT="${RUNNER_TEMP:-/tmp}/sei-release-upgrade-${GITHUB_RUN_ID:-$$}"
readonly MAIN_WORKTREE="$RUN_ROOT/main"
readonly RELEASE_WORKTREE="$RUN_ROOT/release"
readonly BUILD_ROOT="$RUN_ROOT/bin"
readonly ARTIFACT_ROOT="$RUN_ROOT/artifacts"
readonly NODE_COUNT=4
readonly ORACLE_VOTE_PERIOD=20
readonly UPGRADE_NAME="v6.7"
readonly -a EXPECTED_REMOVED_MODULES=(capability feegrant ibc transfer)

RELEASE_BRANCH="${RELEASE_BRANCH:-release/v6.6}"
MAIN_REF="${MAIN_REF:-${GITHUB_SHA:-HEAD}}"
UPGRADE_LEAD_SECONDS="${UPGRADE_LEAD_SECONDS:-60}"
POST_UPGRADE_BLOCKS="${POST_UPGRADE_BLOCKS:-10}"
CLUSTER_STARTED=false
MAIN_BINARY_HASH=
RELEASE_MODULE_VERSIONS=
FEE_GRANTER_ADDRESS=
FEE_GRANTEE_ADDRESS=

log() {
  printf '\n[%s] %s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "$*"
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

validate_inputs() {
  [[ "$UPGRADE_LEAD_SECONDS" =~ ^[0-9]+$ ]] ||
    die "upgrade_lead_seconds must be an integer"
  ((UPGRADE_LEAD_SECONDS >= 60 && UPGRADE_LEAD_SECONDS <= 300)) ||
    die "upgrade_lead_seconds must be between 60 and 300"

  [[ "$POST_UPGRADE_BLOCKS" =~ ^[0-9]+$ ]] ||
    die "post_upgrade_blocks must be an integer"
  ((POST_UPGRADE_BLOCKS >= 2 && POST_UPGRADE_BLOCKS <= 100)) ||
    die "post_upgrade_blocks must be between 2 and 100"

  [[ "$RELEASE_BRANCH" =~ ^release/v[0-9]+\.[0-9]+(\.[0-9]+)?(-branch)?$ ]] ||
    die "release_branch must look like release/v6.6 or release/v6.2.0-branch"
  [[ -n "$MAIN_REF" ]] || die "main_ref must not be empty"
}

latest_upgrade_tag() {
  python3 - "$1/app/tags" <<'PY'
import re
import sys

versions = []
with open(sys.argv[1], encoding="utf-8") as tags:
    for line in tags:
        tag = line.strip()
        match = re.fullmatch(r"v(\d+)\.(\d+)(?:\.(\d+))?", tag)
        if match:
            versions.append((tuple(int(part or 0) for part in match.groups()), tag))

if not versions:
    raise SystemExit(f"no semantic upgrade names found in {sys.argv[1]}")

print(max(versions)[1])
PY
}

has_upgrade_tag() {
  local source_dir="$1"
  local upgrade_name="$2"
  grep -Fxq "$upgrade_name" "$source_dir/app/tags"
}

prepare_worktrees() {
  log "Pinning main $MAIN_REF and release $RELEASE_BRANCH"
  local main_sha
  main_sha="$(git rev-parse "$MAIN_REF^{commit}")" ||
    die "unable to resolve main ref $MAIN_REF"

  git fetch --no-tags origin "$RELEASE_BRANCH"
  local release_sha
  release_sha="$(git rev-parse FETCH_HEAD)"

  git worktree add --detach "$MAIN_WORKTREE" "$main_sha"
  git worktree add --detach "$RELEASE_WORKTREE" "$release_sha"

  {
    printf 'main_ref=%s\n' "$MAIN_REF"
    printf 'main_sha=%s\n' "$main_sha"
    printf 'release_branch=%s\n' "$RELEASE_BRANCH"
    printf 'release_sha=%s\n' "$release_sha"
  } | tee "$ARTIFACT_ROOT/revisions.txt"
}

prepare_upgrade_name() {
  local release_upgrade
  local main_upgrade
  release_upgrade="$(latest_upgrade_tag "$RELEASE_WORKTREE")"
  main_upgrade="$(latest_upgrade_tag "$MAIN_WORKTREE")"

  ! has_upgrade_tag "$RELEASE_WORKTREE" "$UPGRADE_NAME" ||
    die "$RELEASE_BRANCH already knows $UPGRADE_NAME; it cannot exercise that upgrade"
  has_upgrade_tag "$MAIN_WORKTREE" "$UPGRADE_NAME" ||
    die "main $MAIN_REF does not register $UPGRADE_NAME"
  log "Testing $RELEASE_BRANCH ($release_upgrade) -> main with $UPGRADE_NAME"

  {
    printf 'release_upgrade=%s\n' "$release_upgrade"
    printf 'main_latest_upgrade=%s\n' "$main_upgrade"
    printf 'test_upgrade=%s\n' "$UPGRADE_NAME"
  } | tee -a "$ARTIFACT_ROOT/revisions.txt"
}

build_localnode_image() {
  log "Building the localnode toolchain image"
  (
    cd "$REPO_ROOT"
    DOCKER_PLATFORM=linux/amd64 make build-docker-node
  )
}

build_binary() {
  local source_dir="$1"
  local output_path="$2"
  local label="$3"
  local source_commit
  local source_version
  local go_mod_cache
  local go_build_cache
  source_commit="$(git -C "$source_dir" rev-parse HEAD)"
  source_version="$(git -C "$source_dir" describe --tags --always)"
  go_mod_cache="$(go env GOMODCACHE)"
  go_build_cache="$(go env GOCACHE)"
  mkdir -p "$go_mod_cache" "$go_build_cache"

  log "Building $label seid"
  docker run --rm \
    --user="$(id -u):$(id -g)" \
    --platform linux/amd64 \
    -v "$source_dir:/sei-protocol/sei-chain:Z" \
    -v "$go_mod_cache:/root/go/pkg/mod:Z" \
    -v "$go_build_cache:/root/.cache/go-build:Z" \
    -w /sei-protocol/sei-chain \
    -e LEDGER_ENABLED=false \
    -e GOFLAGS=-buildvcs=false \
    -e "BUILD_COMMIT=$source_commit" \
    -e "BUILD_VERSION=$source_version" \
    sei-chain/localnode \
    bash -c 'export PATH=/usr/local/go/bin:$PATH && make clean && make VERSION="$BUILD_VERSION" COMMIT="$BUILD_COMMIT" build-linux'

  install -m 0755 "$source_dir/build/seid" "$output_path"
  sha256sum "$output_path" | tee "$ARTIFACT_ROOT/$label.sha256"
}

height() {
  local node="$1"
  docker exec "$node" /root/go/bin/seid status 2>/dev/null |
    jq -er '.SyncInfo.latest_block_height | tonumber'
}

wait_for_cluster_ready() {
  local deadline=$((SECONDS + 300))
  log "Waiting for all validators to become query-ready"

  while ((SECONDS < deadline)); do
    if [[ -f "$REPO_ROOT/build/generated/launch.complete" ]] &&
      [[ "$(wc -l <"$REPO_ROOT/build/generated/launch.complete")" -eq "$NODE_COUNT" ]]; then
      return
    fi
    sleep 2
  done

  die "cluster did not become ready within 300 seconds"
}

wait_for_node_height() {
  local node="$1"
  local target="$2"
  local timeout_seconds="$3"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while ((SECONDS < deadline)); do
    if current="$(height "$node" 2>/dev/null)" && ((current >= target)); then
      printf '%s reached height %s\n' "$node" "$current"
      return
    fi
    sleep 1
  done

  die "$node did not reach height $target within $timeout_seconds seconds"
}

wait_for_blocks() {
  local node="$1"
  local blocks="$2"
  local start
  start="$(height "$node")" || die "unable to query $node before waiting for blocks"
  wait_for_node_height "$node" "$((start + blocks))" 180
}

node_key_address() {
  local node="$1"
  local key="$2"
  printf '12345678\n' |
    docker exec -i "$node" seid keys show "$key" -a 2>/dev/null
}

node_validator_address() {
  local node="$1"
  printf '12345678\n' |
    docker exec -i "$node" seid keys show node_admin --bech val -a 2>/dev/null
}

require_check_tx_success() {
  local label="$1"
  local output="$2"
  local code
  code="$(jq -r '.code // 0' <<<"$output" 2>/dev/null)" ||
    die "$label did not return JSON: $output"
  [[ "$code" == "0" ]] ||
    die "$label was rejected: $(jq -r '.raw_log // .' <<<"$output")"
}

feegrant_spend_limit() {
  jq -er '
    [
      .. | objects | select(has("spend_limit")) |
      .spend_limit[] | select(.denom == "usei") | .amount | tonumber
    ][0]
  '
}

query_module_versions() {
  local node="$1"
  docker exec "$node" seid q upgrade module_versions --output json |
    jq -er '.module_versions | map(.name) | sort | .[]'
}

seed_release_oracle_rates() {
  log "Seeding oracle exchange rates with release validator votes"
  local configured_vote_period
  configured_vote_period="$(
    docker exec sei-node-0 seid q oracle params --output json |
      jq -er '.params.vote_period | tonumber'
  )" || die "unable to query the release oracle vote period"
  ((configured_vote_period == ORACLE_VOTE_PERIOD)) ||
    die "release oracle vote period is $configured_vote_period, expected $ORACLE_VOTE_PERIOD"

  local attempt
  local code
  local current
  local start
  local tally_height
  local node
  local output_file
  local validator
  local rates=

  for ((attempt = 1; attempt <= 3; attempt++)); do
    current="$(height sei-node-0)"
    start="$((current + ORACLE_VOTE_PERIOD - (current % ORACLE_VOTE_PERIOD) + 1))"
    wait_for_node_height sei-node-0 "$start" 180

    for ((i = 0; i < NODE_COUNT; i++)); do
      node="sei-node-$i"
      output_file="$ARTIFACT_ROOT/oracle-vote-$attempt-$i.json"
      validator="$(node_validator_address "$node")" ||
        die "unable to resolve the oracle validator in $node"
      [[ -n "$validator" ]] || die "oracle validator in $node is empty"
      if ! (
        printf '12345678\n' |
          docker exec -i "$node" seid tx oracle aggregate-vote \
            "1.${attempt}uatom,2.${attempt}ueth" "$validator" \
            --from node_admin --chain-id sei --fees 200000usei --gas 2000000 \
            --broadcast-mode sync --yes --output json
      ) >"$output_file" 2>"$output_file.stderr"; then
        die "oracle vote failed in $node; see $output_file.stderr"
      fi
      code="$(jq -r '.code // 0' "$output_file" 2>/dev/null)" ||
        die "oracle vote in $node did not return JSON; see $output_file"
      [[ "$code" == "0" ]] ||
        die "oracle vote in $node was rejected; see $output_file"
    done

    current="$(height sei-node-0)"
    tally_height="$((current + ORACLE_VOTE_PERIOD - (current % ORACLE_VOTE_PERIOD) + 2))"
    wait_for_node_height sei-node-0 "$tally_height" 180
    if rates="$(docker exec sei-node-0 seid q oracle exchange-rates --output json 2>&1)" &&
      jq -e '.denom_oracle_exchange_rate_pairs | length > 0' <<<"$rates" >/dev/null; then
      printf '%s\n' "$rates" >"$ARTIFACT_ROOT/release-oracle-rates.json"
      return
    fi
  done

  die "release validators did not produce oracle exchange rates; see oracle-vote artifacts"
}

seed_release_state() {
  log "Seeding state through modules available on the release binary"
  FEE_GRANTER_ADDRESS="$(node_key_address sei-node-0 admin)" ||
    die "unable to resolve the release fee granter"
  FEE_GRANTEE_ADDRESS="$(node_key_address sei-node-0 node_admin)" ||
    die "unable to resolve the release fee grantee"
  [[ -n "$FEE_GRANTER_ADDRESS" && -n "$FEE_GRANTEE_ADDRESS" ]] ||
    die "release feegrant accounts are empty"

  local grant_output
  if ! grant_output="$(
    printf '12345678\n' |
      docker exec -i sei-node-0 seid tx feegrant grant \
        "$FEE_GRANTER_ADDRESS" "$FEE_GRANTEE_ADDRESS" \
        --spend-limit 100000000usei --from admin --chain-id sei \
        --fees 200000usei --gas 2000000 --broadcast-mode sync --yes --output json \
        2>"$ARTIFACT_ROOT/release-feegrant-grant.stderr"
  )"; then
    die "release binary could not grant a fee allowance; see release-feegrant-grant.stderr"
  fi
  require_check_tx_success "fee allowance grant" "$grant_output"
  wait_for_blocks sei-node-0 3

  local allowance
  allowance="$(docker exec sei-node-0 seid q feegrant grant \
    "$FEE_GRANTER_ADDRESS" "$FEE_GRANTEE_ADDRESS" --output json 2>&1)" ||
    die "release binary could not query the seeded fee allowance: $allowance"
  jq -e '.allowance' <<<"$allowance" >/dev/null ||
    die "release fee allowance was not stored: $allowance"
  printf '%s\n' "$allowance" >"$ARTIFACT_ROOT/release-feegrant.json"
  local allowance_before
  allowance_before="$(feegrant_spend_limit <<<"$allowance")" ||
    die "release fee allowance has no usei spend limit: $allowance"

  local spend_output
  if ! spend_output="$(
    printf '12345678\n' |
      docker exec -i sei-node-0 seid tx bank send node_admin \
        "$FEE_GRANTER_ADDRESS" 1usei --fee-account "$FEE_GRANTER_ADDRESS" \
        --from node_admin --chain-id sei --fees 200000usei --gas 2000000 \
        --broadcast-mode sync --yes --output json \
        2>"$ARTIFACT_ROOT/release-feegrant-spend.stderr"
  )"; then
    die "release binary could not spend the fee allowance; see release-feegrant-spend.stderr"
  fi
  require_check_tx_success "fee-granted bank send" "$spend_output"
  wait_for_blocks sei-node-0 3

  local allowance_after_output
  allowance_after_output="$(docker exec sei-node-0 seid q feegrant grant \
    "$FEE_GRANTER_ADDRESS" "$FEE_GRANTEE_ADDRESS" --output json 2>&1)" ||
    die "release binary could not re-query the spent fee allowance: $allowance_after_output"
  local allowance_after
  allowance_after="$(feegrant_spend_limit <<<"$allowance_after_output")" ||
    die "spent release fee allowance has no usei spend limit: $allowance_after_output"
  ((allowance_after < allowance_before)) ||
    die "fee-granted send did not consume the allowance ($allowance_before -> $allowance_after)"
  printf '%s\n' "$allowance_after_output" \
    >"$ARTIFACT_ROOT/release-feegrant-after-spend.json"

  seed_release_oracle_rates

  RELEASE_MODULE_VERSIONS="$(query_module_versions sei-node-0)" ||
    die "unable to query release module versions"
  printf '%s\n' "$RELEASE_MODULE_VERSIONS" >"$ARTIFACT_ROOT/release-module-versions.txt"

  local module
  for module in "${EXPECTED_REMOVED_MODULES[@]}" oracle; do
    grep -Fxq "$module" <<<"$RELEASE_MODULE_VERSIONS" ||
      die "release module version map does not contain $module"
  done
}

assert_all_nodes_progress() {
  local blocks="$1"
  local label="$2"
  local node
  local start

  log "Asserting $label block production"
  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    start="$(height "$node")" || die "unable to query $node before $label progress check"
    wait_for_node_height "$node" "$((start + blocks))" 180
  done
}

verify_running_binary_hash() {
  local expected_hash="$1"
  local label="$2"
  local node
  local actual_hash

  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    actual_hash="$(docker exec "$node" sha256sum /root/go/bin/seid | awk '{print $1}')"
    [[ "$actual_hash" == "$expected_hash" ]] ||
      die "$node is not running the expected $label binary"
  done
}

start_release_cluster() {
  log "Starting four validators with the release binary"
  mkdir -p "$REPO_ROOT/build"
  install -m 0755 "$BUILD_ROOT/release-seid" "$REPO_ROOT/build/seid"

  (
    cd "$REPO_ROOT"
    DOCKER_DETACH=true \
      DOCKER_PLATFORM=linux/amd64 \
      INVARIANT_CHECK_INTERVAL=10 \
      ORACLE_VOTE_PERIOD="$ORACLE_VOTE_PERIOD" \
      UPGRADE_VERSION_LIST='' \
      make docker-cluster-start-skipbuild
  )
  CLUSTER_STARTED=true

  wait_for_cluster_ready
  local release_hash
  release_hash="$(sha256sum "$BUILD_ROOT/release-seid" | awk '{print $1}')"
  verify_running_binary_hash "$release_hash" release
  assert_all_nodes_progress 5 "pre-upgrade"
}

submit_upgrade() {
  log "Submitting coordinated upgrade $UPGRADE_NAME"
  local target_output
  target_output="$(docker exec sei-node-0 \
    proposal_target_height.sh "$UPGRADE_LEAD_SECONDS")"
  TARGET_HEIGHT="$(awk '/^[0-9]+$/ { value=$0 } END { print value }' <<<"$target_output")"
  [[ "$TARGET_HEIGHT" =~ ^[0-9]+$ ]] ||
    die "could not determine target height from: $target_output"

  local proposal_output
  proposal_output="$(docker exec sei-node-0 \
    proposal_submit.sh "$TARGET_HEIGHT" major "$UPGRADE_NAME")"
  printf '%s\n' "$proposal_output"
  local proposal_id
  proposal_id="$(awk '/^[0-9]+$/ { value=$0 } END { print value }' <<<"$proposal_output")"
  [[ "$proposal_id" =~ ^[0-9]+$ ]] ||
    die "could not determine proposal ID"

  local node
  local vote_output
  local vote_result
  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    vote_output="$(docker exec "$node" proposal_vote.sh "$proposal_id")"
    printf '%s\n' "$vote_output"
    vote_result="$(awk '/^[0-9]+$/ { value=$0 } END { print value }' <<<"$vote_output")"
    [[ "$vote_result" == "0" ]] || die "$node failed to vote for proposal $proposal_id"
  done

  docker exec sei-node-0 \
    proposal_wait_for_pass.sh "$proposal_id" node_admin

  {
    printf 'proposal_id=%s\n' "$proposal_id"
    printf 'target_height=%s\n' "$TARGET_HEIGHT"
  } | tee -a "$ARTIFACT_ROOT/revisions.txt"
}

stage_main_binary() {
  log "Staging the main binary in every validator"
  MAIN_BINARY_HASH="$(sha256sum "$BUILD_ROOT/main-seid" | awk '{print $1}')"
  local node
  local actual_hash

  docker exec --user root sei-node-0 \
    sh -c 'cp /root/go/bin/seid /tmp/seid.release && chmod 0755 /tmp/seid.release'

  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    docker cp "$BUILD_ROOT/main-seid" "$node:/tmp/seid.next"
    actual_hash="$(docker exec "$node" sha256sum /tmp/seid.next | awk '{print $1}')"
    [[ "$actual_hash" == "$MAIN_BINARY_HASH" ]] ||
      die "$node staged binary checksum mismatch"
  done
}

node_has_upgrade_halt() {
  local node_id="$1"
  local expected="UPGRADE \"$UPGRADE_NAME\" NEEDED at height: $TARGET_HEIGHT"
  local escaped_expected="UPGRADE \\\"$UPGRADE_NAME\\\" NEEDED at height: $TARGET_HEIGHT"
  local log_file="$REPO_ROOT/build/generated/logs/seid-$node_id.log"

  [[ -f "$log_file" ]] &&
    { grep -Fq "$expected" "$log_file" ||
      grep -Fq "$escaped_expected" "$log_file"; }
}

seid_process_state() {
  local node="$1"
  local state
  if ! state="$(docker exec "$node" sh -c '
    for comm in /proc/[0-9]*/comm; do
      [ -r "$comm" ] || continue
      read -r name <"$comm" || continue
      if [ "$name" = seid ]; then
        process_dir="${comm%/comm}"
        read -r stat_pid stat_comm process_state stat_rest <"$process_dir/stat" ||
          continue
        [ "$process_state" = Z ] && continue
        printf running
        exit
      fi
    done
    printf stopped
  ')"; then
    die "unable to inspect the seid process in $node"
  fi
  case "$state" in
    running | stopped)
      printf '%s\n' "$state"
      ;;
    *)
      die "$node returned an invalid seid process state: $state"
      ;;
  esac
}

stop_node_process() {
  local node="$1"
  docker exec "$node" sh -c '
    for comm in /proc/[0-9]*/comm; do
      [ -r "$comm" ] || continue
      read -r name <"$comm" || continue
      if [ "$name" = seid ]; then
        process_dir="${comm%/comm}"
        read -r stat_pid stat_comm process_state stat_rest <"$process_dir/stat" ||
          continue
        [ "$process_state" = Z ] && continue
        pid="${comm#/proc/}"
        pid="${pid%/comm}"
        kill -TERM "$pid"
      fi
    done
  ' || die "unable to stop seid in $node"

  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    if [[ "$(seid_process_state "$node")" == stopped ]]; then
      return
    fi
    sleep 1
  done
  die "seid in $node did not stop within 60 seconds"
}

install_main_binary() {
  local node_id="$1"
  local node="sei-node-$node_id"
  local actual_hash

  docker exec "$node" test -f /root/.sei/data/upgrade-info.json ||
    die "$node halted without writing upgrade-info.json"

  log "Installing main on $node"
  mkdir -p "$ARTIFACT_ROOT/pre-upgrade-logs"
  cp "$REPO_ROOT/build/generated/logs/seid-$node_id.log" \
    "$ARTIFACT_ROOT/pre-upgrade-logs/"
  docker exec --user root "$node" \
    sh -c 'mv /tmp/seid.next /root/go/bin/seid && chmod 0755 /root/go/bin/seid'
  actual_hash="$(docker exec "$node" sha256sum /root/go/bin/seid | awk '{print $1}')"
  [[ "$actual_hash" == "$MAIN_BINARY_HASH" ]] ||
    die "$node installed binary checksum mismatch"
  printf 'upgraded_node=%s\n' "$node" | tee -a "$ARTIFACT_ROOT/revisions.txt"
}

start_main_binary() {
  local node_id="$1"
  local node="sei-node-$node_id"

  log "Starting main on $node"
  docker exec -d "$node" sh -c \
    "exec env -u UPGRADE_VERSION_LIST /root/go/bin/seid start --chain-id sei --inv-check-period 10 >> build/generated/logs/seid-$node_id-post-upgrade.log 2>&1"
}

upgrade_nodes_as_they_halt() {
  log "Supervising each validator through the upgrade at height $TARGET_HEIGHT"
  local timeout_seconds=$((UPGRADE_LEAD_SECONDS + 360))
  local deadline=$((SECONDS + timeout_seconds))
  local upgraded_count=0
  local ready_count=0
  local current
  local node
  local state
  local -a phase=(release release release release)
  local -a ready=(false false false false)
  local -a restart_attempts=(0 0 0 0)
  local -a next_probe=(0 0 0 0)

  # A validator may precommit the target block without entering finalizeCommit
  # before its peers panic. Restart each node only after its own upgrade halt;
  # upgraded peers then keep the target commit available for the remaining
  # release validators to finalize and halt safely.
  while ((SECONDS < deadline && ready_count < NODE_COUNT)); do
    for ((i = 0; i < NODE_COUNT; i++)); do
      node="sei-node-$i"
      if ((SECONDS < next_probe[$i])); then
        continue
      fi

      state="$(seid_process_state "$node")"
      if [[ "${phase[$i]}" == main ]]; then
        if [[ "$state" == stopped ]]; then
          if [[ "${ready[$i]}" == true ]]; then
            ready[$i]=false
            ready_count=$((ready_count - 1))
          fi
          ((restart_attempts[$i] < 5)) ||
            die "$node main binary exited after 5 restart attempts"
          restart_attempts[$i]=$((restart_attempts[$i] + 1))
          start_main_binary "$i"
          next_probe[$i]=$((SECONDS + 1))
          continue
        fi
        if [[ "${ready[$i]}" == false ]] &&
          current="$(height "$node" 2>/dev/null)" &&
          ((current >= TARGET_HEIGHT)); then
          ready[$i]=true
          ready_count=$((ready_count + 1))
          printf 'ready_node=%s height=%s\n' "$node" "$current" |
            tee -a "$ARTIFACT_ROOT/revisions.txt"
        fi
        continue
      fi

      if [[ "$state" == stopped ]]; then
        # The panic log and process exit can become visible in either order.
        # Re-read the log after observing exit before classifying it.
        if ! node_has_upgrade_halt "$i"; then
          sleep 0.1
          node_has_upgrade_halt "$i" ||
            die "$node exited without the expected upgrade-needed halt"
        fi
        install_main_binary "$i"
        phase[$i]=main
        upgraded_count=$((upgraded_count + 1))
        restart_attempts[$i]=1
        start_main_binary "$i"
        next_probe[$i]=$((SECONDS + 1))
        continue
      fi
      if node_has_upgrade_halt "$i"; then
        # The process has logged the panic but is still unwinding. Keep polling
        # every validator so peers that have already exited restart promptly.
        continue
      fi
    done
    ((ready_count == NODE_COUNT)) || sleep 0.5
  done

  ((upgraded_count == NODE_COUNT)) ||
    die "only $upgraded_count of $NODE_COUNT validators halted and upgraded within $timeout_seconds seconds"
  ((ready_count == NODE_COUNT)) ||
    die "only $ready_count of $NODE_COUNT upgraded validators became ready within $timeout_seconds seconds"

  for ((i = 0; i < NODE_COUNT; i++)); do
    state="$(seid_process_state "sei-node-$i")"
    [[ "$state" == running ]] ||
      die "sei-node-$i is not running after the coordinated upgrade"
  done
  verify_running_binary_hash "$MAIN_BINARY_HASH" main
}

assert_v67_upgrade() {
  log "Asserting the v6.7 module retirement"
  local applied_output
  applied_output="$(docker exec sei-node-0 \
    seid q upgrade applied "$UPGRADE_NAME" --output json 2>&1)" ||
    die "unable to query applied plan $UPGRADE_NAME: $applied_output"

  local applied_height
  applied_height="$(jq -er '.header.height | tonumber' <<<"$applied_output")" ||
    die "applied plan $UPGRADE_NAME has no height: $applied_output"
  ((applied_height == TARGET_HEIGHT)) ||
    die "$UPGRADE_NAME applied at height $applied_height, expected $TARGET_HEIGHT"
  printf 'applied_height=%s\n' "$applied_height" |
    tee -a "$ARTIFACT_ROOT/revisions.txt"

  local main_module_versions
  main_module_versions="$(query_module_versions sei-node-0)" ||
    die "unable to query main module versions"
  printf '%s\n' "$main_module_versions" \
    >"$ARTIFACT_ROOT/main-module-versions.txt"

  local removed_modules
  local expected_removed
  removed_modules="$(
    comm -23 \
      <(printf '%s\n' "$RELEASE_MODULE_VERSIONS") \
      <(printf '%s\n' "$main_module_versions")
  )"
  expected_removed="$(printf '%s\n' "${EXPECTED_REMOVED_MODULES[@]}" | sort)"
  [[ "$removed_modules" == "$expected_removed" ]] ||
    die "unexpected removed module versions; expected [$expected_removed], got [$removed_modules]"

  grep -Fxq oracle <<<"$main_module_versions" ||
    die "main module version map no longer contains oracle"

  local feegrant_output
  feegrant_output="$(
    printf '12345678\n' |
      docker exec -i sei-node-0 seid tx bank send node_admin \
        "$FEE_GRANTER_ADDRESS" 1usei --fee-account "$FEE_GRANTER_ADDRESS" \
        --from node_admin --chain-id sei --fees 200000usei --gas 2000000 \
        --broadcast-mode sync --yes --output json 2>&1 || true
  )"
  printf '%s\n' "$feegrant_output" >"$ARTIFACT_ROOT/main-feegrant-response.txt"
  grep -Fq "fee grants are not enabled" <<<"$feegrant_output" ||
    die "main did not reject the previously valid fee-granted send: $feegrant_output"

  local oracle_output
  oracle_output="$(
    docker exec sei-node-0 seid q oracle exchange-rates --output json 2>&1 || true
  )"
  printf '%s\n' "$oracle_output" >"$ARTIFACT_ROOT/main-oracle-response.txt"
  grep -Fq "oracle module is deprecated" <<<"$oracle_output" ||
    die "main did not deprecate the release oracle query: $oracle_output"
}

export_node_state() {
  local binary_path="$1"
  local label="$2"
  local raw_output="$ARTIFACT_ROOT/$label.raw"
  local json_output="$ARTIFACT_ROOT/$label.json"
  local error_output="$ARTIFACT_ROOT/$label.stderr"
  local exported=false
  local attempt

  for ((attempt = 1; attempt <= 10; attempt++)); do
    if docker exec sei-node-0 "$binary_path" export --home /root/.sei --chain-id sei \
      >"$raw_output" 2>"$error_output"; then
      exported=true
      break
    fi
    sleep 2
  done
  [[ "$exported" == true ]] || return 1

  if ! python3 - "$raw_output" "$json_output" <<'PY'
import json
import pathlib
import sys

raw_path = pathlib.Path(sys.argv[1])
output_path = pathlib.Path(sys.argv[2])
text = raw_path.read_text(encoding="utf-8", errors="replace")
decoder = json.JSONDecoder()
offset = 0

for line in text.splitlines(keepends=True):
    stripped = line.lstrip()
    if stripped.startswith("{"):
        start = offset + len(line) - len(stripped)
        try:
            value, _ = decoder.raw_decode(text[start:])
        except json.JSONDecodeError:
            pass
        else:
            if isinstance(value, dict) and "app_state" in value:
                output_path.write_text(
                    json.dumps(value, separators=(",", ":")) + "\n",
                    encoding="utf-8",
                )
                break
    offset += len(line)
else:
    raise SystemExit("no genesis document found")
PY
  then
    return 1
  fi
}

verify_retained_state() {
  log "Verifying retired state through both binaries"
  stop_node_process sei-node-0

  export_node_state /root/go/bin/seid main-export ||
    die "main export failed; see main-export.stderr"
  jq -e '
    (.app_state | has("feegrant") | not) and
    (.app_state | has("capability") | not) and
    (.app_state | has("ibc") | not) and
    (.app_state | has("transfer") | not) and
    (.app_state.oracle.exchange_rates | length > 0)
  ' "$ARTIFACT_ROOT/main-export.json" >/dev/null ||
    die "main export did not omit retired modules while retaining oracle rates"

  if export_node_state /tmp/seid.release release-export-after-upgrade; then
    jq -e '
      (.app_state.feegrant.allowances | length > 0) and
      (.app_state | has("capability")) and
      (.app_state.oracle.exchange_rates | length > 0)
    ' "$ARTIFACT_ROOT/release-export-after-upgrade.json" >/dev/null ||
      die "release export lost retained feegrant, capability, or oracle state"
  else
    log "Release binary could not export upgraded state; keeping its diagnostics"
  fi
}

verify_post_upgrade() {
  local required_height=$((TARGET_HEIGHT + POST_UPGRADE_BLOCKS))
  log "Requiring every main node to reach height $required_height"
  local node

  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    wait_for_node_height "$node" "$required_height" 300
  done

  assert_all_nodes_progress 3 "post-upgrade"

  local current
  for ((i = 0; i < NODE_COUNT; i++)); do
    current="$(height "sei-node-$i")"
    printf 'post_upgrade_node_%s_height=%s\n' "$i" "$current" |
      tee -a "$ARTIFACT_ROOT/revisions.txt"
  done

  assert_v67_upgrade
  verify_retained_state
  log "Upgrade $UPGRADE_NAME succeeded"
}

collect_diagnostics() {
  mkdir -p "$ARTIFACT_ROOT/final-logs"
  if [[ -d "$REPO_ROOT/build/generated/logs" ]]; then
    cp -a "$REPO_ROOT/build/generated/logs/." "$ARTIFACT_ROOT/final-logs/" ||
      true
  fi

  if command -v docker >/dev/null 2>&1; then
    docker ps -a >"$ARTIFACT_ROOT/docker-ps.txt" 2>&1 || true
    local node
    for ((i = 0; i < NODE_COUNT; i++)); do
      node="sei-node-$i"
      docker logs "$node" >"$ARTIFACT_ROOT/$node-container.log" 2>&1 || true
    done
  fi
}

cleanup() {
  local exit_code=$?
  trap - EXIT
  set +e
  collect_diagnostics
  if [[ "$CLUSTER_STARTED" == true ]]; then
    (
      cd "$REPO_ROOT"
      DOCKER_PLATFORM=linux/amd64 make docker-cluster-stop
    )
  fi
  git -C "$REPO_ROOT" worktree remove --force "$MAIN_WORKTREE" 2>/dev/null
  git -C "$REPO_ROOT" worktree remove --force "$RELEASE_WORKTREE" 2>/dev/null
  git -C "$REPO_ROOT" worktree prune
  exit "$exit_code"
}

main() {
  validate_inputs
  mkdir -p "$RUN_ROOT" "$BUILD_ROOT" "$ARTIFACT_ROOT"
  trap cleanup EXIT

  prepare_worktrees
  prepare_upgrade_name
  build_localnode_image
  build_binary "$RELEASE_WORKTREE" "$BUILD_ROOT/release-seid" release
  build_binary "$MAIN_WORKTREE" "$BUILD_ROOT/main-seid" main
  start_release_cluster
  seed_release_state
  stage_main_binary
  submit_upgrade
  upgrade_nodes_as_they_halt
  verify_post_upgrade
}

main "$@"
