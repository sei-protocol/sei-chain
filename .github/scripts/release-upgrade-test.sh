#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(git rev-parse --show-toplevel)"
readonly RUN_ROOT="${RUNNER_TEMP:-/tmp}/sei-release-upgrade-${GITHUB_RUN_ID:-$$}"
readonly MAIN_WORKTREE="$RUN_ROOT/main"
readonly RELEASE_WORKTREE="$RUN_ROOT/release"
readonly BUILD_ROOT="$RUN_ROOT/bin"
readonly ARTIFACT_ROOT="$RUN_ROOT/artifacts"
readonly NODE_COUNT=4
readonly CROSS_VERSION_ARTIFACT="$ARTIFACT_ROOT/cross-version.json"

RELEASE_BRANCH="${RELEASE_BRANCH:-}"
MAIN_REF="${MAIN_REF:-${GITHUB_SHA:-HEAD}}"
UPGRADE_LEAD_SECONDS="${UPGRADE_LEAD_SECONDS:-60}"
POST_UPGRADE_BLOCKS="${POST_UPGRADE_BLOCKS:-10}"
CLUSTER_STARTED=false
MAIN_BINARY_HASH=
BOUNDARY_FROM=
UPGRADE_NAME=
UPGRADE_TAG=
CROSS_VERSION_TESTS=

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

  if [[ -n "$RELEASE_BRANCH" ]]; then
    [[ "$RELEASE_BRANCH" =~ ^release/v[0-9]+\.[0-9]+(\.[0-9]+)?(-branch)?$ ]] ||
      die "release_branch must look like release/v6.6 or release/v6.2.0-branch"
  fi
  [[ "$MAIN_REF" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ ]] &&
    [[ "$MAIN_REF" != *..* ]] && [[ "$MAIN_REF" != *@\{* ]] ||
    die "main_ref contains characters Git cannot safely resolve"
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

prepare_boundary() {
  BOUNDARY_FROM="$(go run ./upgradetest/cmd/boundary from)"
  UPGRADE_NAME="$(go run ./upgradetest/cmd/boundary to)"
  UPGRADE_TAG="$(go run ./upgradetest/cmd/boundary tag)"
  RELEASE_BRANCH="${RELEASE_BRANCH:-release/$BOUNDARY_FROM}"
}

resolve_ref() {
  local ref="$1"
  if [[ "$ref" == "HEAD" || "$ref" =~ ^[0-9a-fA-F]{40}$ ]]; then
    git rev-parse --verify "$ref^{commit}"
    return
  fi

  git fetch --no-tags origin "$ref" >&2
  git rev-parse --verify FETCH_HEAD
}

prepare_worktrees() {
  log "Pinning $MAIN_REF and $RELEASE_BRANCH"
  local main_sha
  main_sha="$(resolve_ref "$MAIN_REF")" ||
    die "unable to resolve target ref $MAIN_REF"
  local release_sha
  release_sha="$(resolve_ref "$RELEASE_BRANCH")" ||
    die "unable to resolve source ref $RELEASE_BRANCH"

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

  has_upgrade_tag "$RELEASE_WORKTREE" "$BOUNDARY_FROM" ||
    die "$RELEASE_BRANCH does not contain source upgrade $BOUNDARY_FROM"
  ! has_upgrade_tag "$RELEASE_WORKTREE" "$UPGRADE_NAME" ||
    die "$RELEASE_BRANCH already contains $UPGRADE_NAME; it cannot test that boundary"
  has_upgrade_tag "$MAIN_WORKTREE" "$UPGRADE_NAME" ||
    die "$MAIN_REF does not contain target upgrade $UPGRADE_NAME"

  log "Testing $RELEASE_BRANCH ($release_upgrade) -> $MAIN_REF ($main_upgrade) with $UPGRADE_NAME"

  {
    printf 'release_upgrade=%s\n' "$release_upgrade"
    printf 'main_latest_upgrade=%s\n' "$main_upgrade"
    printf 'boundary_from=%s\n' "$BOUNDARY_FROM"
    printf 'test_upgrade=%s\n' "$UPGRADE_NAME"
    printf 'upgrade_tag=%s\n' "$UPGRADE_TAG"
  } | tee -a "$ARTIFACT_ROOT/revisions.txt"
}

discover_cross_version_tests() {
  local listed
  listed="$(
    go test -tags "$UPGRADE_TAG" -list '^Test.*CrossVersion$' ./app 2>/dev/null |
      awk '/^Test.*CrossVersion$/ { print }'
  )"
  [[ -n "$listed" ]] ||
    die "build tag $UPGRADE_TAG defines no Test*CrossVersion assertion"
  CROSS_VERSION_TESTS="$(paste -sd'|' - <<<"$listed")"
}

run_cross_version_phase() {
  local phase="$1"
  log "Running $UPGRADE_TAG cross-version assertions ($phase)"
  UPGRADE_TEST_PHASE="$phase" \
    UPGRADE_TEST_ARTIFACT="$CROSS_VERSION_ARTIFACT" \
    UPGRADE_TEST_NODE="sei-node-0" \
    UPGRADE_TEST_UPGRADE_NAME="$UPGRADE_NAME" \
    UPGRADE_TEST_TARGET_HEIGHT="${TARGET_HEIGHT:-}" \
    UPGRADE_TEST_RELEASE_BINARY="/tmp/seid.release" \
    go test -tags "$UPGRADE_TAG" \
      -run "^($CROSS_VERSION_TESTS)$" \
      -count=1 \
      -timeout=15m \
      ./app 2>&1 |
    tee "$ARTIFACT_ROOT/cross-version-$phase.log"
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
      UPGRADE_VERSION_LIST= \
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

  # Every validator keeps its own copy of the binary it is running, because a
  # test may put the old binary back on any node, not only the one it queries.
  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    docker exec --user root "$node" \
      sh -c 'cp /root/go/bin/seid /tmp/seid.release && chmod 0755 /tmp/seid.release'
  done

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
    if pgrep -x seid >/dev/null 2>&1; then
      printf running
    else
      status=$?
      if [ "$status" -eq 1 ]; then
        printf stopped
      else
        exit "$status"
      fi
    fi
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

install_main_binary() {
  local node_id="$1"
  local node="sei-node-$node_id"
  local actual_hash

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
  local deadline=$((SECONDS + 360))
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
    die "only $upgraded_count of $NODE_COUNT validators halted and upgraded within 360 seconds"
  ((ready_count == NODE_COUNT)) ||
    die "only $ready_count of $NODE_COUNT upgraded validators became ready within 360 seconds"

  for ((i = 0; i < NODE_COUNT; i++)); do
    state="$(seid_process_state "sei-node-$i")"
    [[ "$state" == running ]] ||
      die "sei-node-$i is not running after the coordinated upgrade"
  done
  verify_running_binary_hash "$MAIN_BINARY_HASH" main
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

  run_cross_version_phase after
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
  exit "$exit_code"
}

main() {
  prepare_boundary
  validate_inputs
  mkdir -p "$RUN_ROOT" "$BUILD_ROOT" "$ARTIFACT_ROOT"
  exec > >(tee "$ARTIFACT_ROOT/runner.log") 2>&1
  trap cleanup EXIT

  prepare_worktrees
  prepare_upgrade_name
  discover_cross_version_tests
  build_localnode_image
  build_binary "$RELEASE_WORKTREE" "$BUILD_ROOT/release-seid" release
  build_binary "$MAIN_WORKTREE" "$BUILD_ROOT/main-seid" main
  start_release_cluster
  run_cross_version_phase before
  stage_main_binary
  submit_upgrade
  upgrade_nodes_as_they_halt
  verify_post_upgrade
}

main "$@"
