#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(git rev-parse --show-toplevel)"
readonly RUN_ROOT="${RUNNER_TEMP:-/tmp}/sei-release-upgrade-${GITHUB_RUN_ID:-$$}"
readonly MAIN_WORKTREE="$RUN_ROOT/main"
readonly RELEASE_WORKTREE="$RUN_ROOT/release"
readonly BUILD_ROOT="$RUN_ROOT/bin"
readonly ARTIFACT_ROOT="$REPO_ROOT/artifacts/release-upgrade"
readonly NODE_COUNT=4

RELEASE_BRANCH="${RELEASE_BRANCH:-}"
UPGRADE_LEAD_SECONDS="${UPGRADE_LEAD_SECONDS:-60}"
POST_UPGRADE_BLOCKS="${POST_UPGRADE_BLOCKS:-10}"
CLUSTER_STARTED=false
MAIN_BINARY_HASH=

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
}

resolve_latest_release_branch() {
  local heads_file="$RUN_ROOT/release-heads"
  git ls-remote --heads origin 'release/v*' >"$heads_file"

  python3 - "$heads_file" <<'PY'
import re
import sys

patterns = (
    (re.compile(r"^refs/heads/(release/v(\d+)\.(\d+))$"), 2),
    (re.compile(r"^refs/heads/(release/v(\d+)\.(\d+)\.(\d+))$"), 1),
    (re.compile(r"^refs/heads/(release/v(\d+)\.(\d+)\.(\d+)-branch)$"), 0),
)

candidates = []
with open(sys.argv[1], encoding="utf-8") as heads:
    for line in heads:
        _, ref = line.split()
        for pattern, preference in patterns:
            match = pattern.fullmatch(ref)
            if not match:
                continue
            branch = match.group(1)
            numbers = tuple(int(part) for part in match.groups()[1:])
            version = numbers if len(numbers) == 3 else (*numbers, 0)
            candidates.append((version, preference, branch))
            break

if not candidates:
    raise SystemExit("no official release/v* branch found")

print(max(candidates)[2])
PY
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

version_greater_than() {
  python3 - "$1" "$2" <<'PY'
import re
import sys

def parse(value):
    match = re.fullmatch(r"v(\d+)\.(\d+)(?:\.(\d+))?", value)
    if not match:
        raise SystemExit(f"invalid upgrade version: {value}")
    return tuple(int(part or 0) for part in match.groups())

raise SystemExit(0 if parse(sys.argv[1]) > parse(sys.argv[2]) else 1)
PY
}

next_minor_version() {
  python3 - "$1" "$2" <<'PY'
import re
import sys

def parse(value):
    match = re.fullmatch(r"v(\d+)\.(\d+)(?:\.(\d+))?", value)
    if not match:
        raise SystemExit(f"invalid upgrade version: {value}")
    return tuple(int(part or 0) for part in match.groups())

highest = max(parse(sys.argv[1]), parse(sys.argv[2]))
print(f"v{highest[0]}.{highest[1] + 1}")
PY
}

prepare_worktrees() {
  if [[ -z "$RELEASE_BRANCH" ]]; then
    RELEASE_BRANCH="$(resolve_latest_release_branch)"
  fi

  log "Pinning origin/main and $RELEASE_BRANCH"
  git fetch --no-tags origin main
  local main_sha
  main_sha="$(git rev-parse FETCH_HEAD)"

  git fetch --no-tags origin "$RELEASE_BRANCH"
  local release_sha
  release_sha="$(git rev-parse FETCH_HEAD)"

  git worktree add --detach "$MAIN_WORKTREE" "$main_sha"
  git worktree add --detach "$RELEASE_WORKTREE" "$release_sha"

  {
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

  if version_greater_than "$main_upgrade" "$release_upgrade"; then
    UPGRADE_NAME="$main_upgrade"
    log "Using main upgrade name $UPGRADE_NAME"
  else
    UPGRADE_NAME="$(next_minor_version "$main_upgrade" "$release_upgrade")"
    log "Generating synthetic main upgrade name $UPGRADE_NAME"
    python3 - "$MAIN_WORKTREE/app/tags" "$UPGRADE_NAME" <<'PY'
import pathlib
import sys

tags_path = pathlib.Path(sys.argv[1])
content = tags_path.read_bytes()
separator = b"" if not content or content.endswith(b"\n") else b"\n"
tags_path.write_bytes(content + separator + sys.argv[2].encode() + b"\n")
PY
    (
      cd "$MAIN_WORKTREE"
      go run ./scripts/bump_version
    ) 2>&1 | tee "$ARTIFACT_ROOT/precompile-generation.log"
  fi

  {
    printf 'release_upgrade=%s\n' "$release_upgrade"
    printf 'main_upgrade_before_generation=%s\n' "$main_upgrade"
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
  local go_mod_cache
  local go_build_cache
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
    sei-chain/localnode \
    bash -c 'export PATH=/usr/local/go/bin:$PATH && make clean && make build-linux'

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

  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    docker cp "$BUILD_ROOT/main-seid" "$node:/tmp/seid.next"
    actual_hash="$(docker exec "$node" sha256sum /tmp/seid.next | awk '{print $1}')"
    [[ "$actual_hash" == "$MAIN_BINARY_HASH" ]] ||
      die "$node staged binary checksum mismatch"
  done
}

wait_for_upgrade_boundary() {
  log "Waiting for the release binary to halt at height $TARGET_HEIGHT"
  local expected="UPGRADE \"$UPGRADE_NAME\" NEEDED at height: $TARGET_HEIGHT"
  local escaped_expected="UPGRADE \\\"$UPGRADE_NAME\\\" NEEDED at height: $TARGET_HEIGHT"
  local deadline=$((SECONDS + 360))
  local found=false
  local log_file

  # A validator panics in BeginBlock before committing the target height. Its
  # peers can remain alive at target-1 because they no longer have quorum, so
  # one exact upgrade halt proves the boundary and the survivors are stopped
  # explicitly before any binary is replaced.
  while ((SECONDS < deadline)); do
    for ((i = 0; i < NODE_COUNT; i++)); do
      log_file="$REPO_ROOT/build/generated/logs/seid-$i.log"
      if [[ -f "$log_file" ]] &&
        { grep -Fq "$expected" "$log_file" ||
          grep -Fq "$escaped_expected" "$log_file"; }; then
        found=true
        break 2
      fi
    done
    sleep 1
  done

  [[ "$found" == true ]] ||
    die "no validator emitted the expected upgrade-needed halt"

  mkdir -p "$ARTIFACT_ROOT/pre-upgrade-logs"
  cp -a "$REPO_ROOT/build/generated/logs/." "$ARTIFACT_ROOT/pre-upgrade-logs/"

  local node
  local current
  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    if current="$(height "$node" 2>/dev/null)" && ((current >= TARGET_HEIGHT)); then
      die "$node committed height $current with the release binary"
    fi
  done
}

stop_old_nodes() {
  log "Stopping release-binary processes"
  local node
  local deadline

  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    docker exec "$node" pkill -f 'seid start --chain-id sei' >/dev/null 2>&1 || true
  done

  deadline=$((SECONDS + 30))
  while ((SECONDS < deadline)); do
    local running=0
    for ((i = 0; i < NODE_COUNT; i++)); do
      node="sei-node-$i"
      if docker exec "$node" pgrep -f 'seid start --chain-id sei' >/dev/null 2>&1; then
        running=$((running + 1))
      fi
    done
    ((running == 0)) && return
    sleep 1
  done

  die "release-binary processes did not stop cleanly"
}

install_and_start_main() {
  log "Installing and starting the main binary on every validator"
  local node

  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    docker exec --user root "$node" \
      sh -c 'mv /tmp/seid.next /root/go/bin/seid && chmod 0755 /root/go/bin/seid'
  done
  verify_running_binary_hash "$MAIN_BINARY_HASH" main

  for ((i = 0; i < NODE_COUNT; i++)); do
    node="sei-node-$i"
    docker exec -d "$node" sh -c \
      "exec env -u UPGRADE_VERSION_LIST /root/go/bin/seid start --chain-id sei --inv-check-period 10 > build/generated/logs/seid-$i-post-upgrade.log 2>&1"
  done
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

  local minimum=
  local maximum=0
  local current
  for ((i = 0; i < NODE_COUNT; i++)); do
    current="$(height "sei-node-$i")"
    ((current > maximum)) && maximum="$current"
    if [[ -z "$minimum" ]] || ((current < minimum)); then
      minimum="$current"
    fi
  done
  ((maximum - minimum <= 3)) ||
    die "validators are not synchronized after upgrade (min=$minimum max=$maximum)"

  printf 'post_upgrade_min_height=%s\n' "$minimum" |
    tee -a "$ARTIFACT_ROOT/revisions.txt"
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
  validate_inputs
  mkdir -p "$RUN_ROOT" "$BUILD_ROOT" "$ARTIFACT_ROOT"
  trap cleanup EXIT

  prepare_worktrees
  prepare_upgrade_name
  build_localnode_image
  build_binary "$RELEASE_WORKTREE" "$BUILD_ROOT/release-seid" release
  build_binary "$MAIN_WORKTREE" "$BUILD_ROOT/main-seid" main
  start_release_cluster
  stage_main_binary
  submit_upgrade
  wait_for_upgrade_boundary
  stop_old_nodes
  install_and_start_main
  verify_post_upgrade
}

main "$@"
