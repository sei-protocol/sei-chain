#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(git rev-parse --show-toplevel)"
readonly RUN_ROOT="${RUNNER_TEMP:-/tmp}/sei-offline-upgrade-${GITHUB_RUN_ID:-$$}"
readonly SOURCE_WORKTREE="$RUN_ROOT/source"
readonly TARGET_WORKTREE="$RUN_ROOT/target"
readonly ARTIFACT_ROOT="$RUN_ROOT/artifacts"
readonly STATE_ARTIFACT="$ARTIFACT_ROOT/state"

FROM_REF="${FROM_REF:-}"
TO_REF="${TO_REF:-${GITHUB_SHA:-HEAD}}"
BOUNDARY_FROM=
BOUNDARY_TO=
UPGRADE_TAG=

log() {
  printf '\n[%s] %s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "$*"
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

offline_phase_test_path() {
  case "$1" in
    source)
      printf '%s/app/testdata/%s_offline_source_test.go\n' "$REPO_ROOT" "$UPGRADE_TAG"
      ;;
    target)
      printf '%s/app/%s_offline_target_test.go\n' "$REPO_ROOT" "$UPGRADE_TAG"
      ;;
    *) die "unknown offline upgrade file phase $1" ;;
  esac
}

prepare_boundary() {
  BOUNDARY_FROM="$(go run ./upgradetest/cmd/boundary from)"
  BOUNDARY_TO="$(go run ./upgradetest/cmd/boundary to)"
  UPGRADE_TAG="$(go run ./upgradetest/cmd/boundary tag)"
  FROM_REF="${FROM_REF:-release/$BOUNDARY_FROM}"
}

validate_inputs() {
  [[ "$FROM_REF" =~ ^release/v[0-9]+\.[0-9]+(\.[0-9]+)?(-branch)?$ ]] ||
    die "from_ref must look like release/v6.6 or release/v6.2.0-branch"
  [[ "$TO_REF" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ ]] &&
    [[ "$TO_REF" != *..* ]] && [[ "$TO_REF" != *@\{* ]] ||
    die "to_ref contains characters Git cannot safely resolve"

  local phase
  local test_path
  for phase in source target; do
    test_path="$(offline_phase_test_path "$phase")"
    [[ -f "$test_path" ]] ||
      die "$UPGRADE_TAG has no offline $phase test"
  done
  grep -q 'func Test.*OfflineUpgradeReopen(' \
    "$(offline_phase_test_path source)" ||
    die "$UPGRADE_TAG has no offline reopen test"
  [[ -f "$REPO_ROOT/app/upgrade_offline_harness_test.go" ]] ||
    die "offline upgrade harness is missing"
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
  local source_sha
  local target_sha
  source_sha="$(resolve_ref "$FROM_REF")" ||
    die "unable to resolve source ref $FROM_REF"
  target_sha="$(resolve_ref "$TO_REF")" ||
    die "unable to resolve target ref $TO_REF"

  git worktree add --detach "$SOURCE_WORKTREE" "$source_sha"
  git worktree add --detach "$TARGET_WORKTREE" "$target_sha"

  grep -Fxq "$BOUNDARY_FROM" "$SOURCE_WORKTREE/app/tags" ||
    die "$FROM_REF does not contain source upgrade $BOUNDARY_FROM"
  ! grep -Fxq "$BOUNDARY_TO" "$SOURCE_WORKTREE/app/tags" ||
    die "$FROM_REF already contains target upgrade $BOUNDARY_TO"
  grep -Fxq "$BOUNDARY_TO" "$TARGET_WORKTREE/app/tags" ||
    die "$TO_REF does not contain target upgrade $BOUNDARY_TO"

  {
    printf 'source_ref=%s\n' "$FROM_REF"
    printf 'source_sha=%s\n' "$source_sha"
    printf 'target_ref=%s\n' "$TO_REF"
    printf 'target_sha=%s\n' "$target_sha"
    printf 'boundary_from=%s\n' "$BOUNDARY_FROM"
    printf 'boundary_to=%s\n' "$BOUNDARY_TO"
    printf 'upgrade_tag=%s\n' "$UPGRADE_TAG"
  } >"$ARTIFACT_ROOT/revisions.txt"
}

install_phase_tests() {
  local worktree="$1"
  local phase="$2"
  install -m 0644 \
    "$REPO_ROOT/app/upgrade_offline_harness_test.go" \
    "$worktree/app/upgrade_offline_harness_test.go"
  install -m 0644 \
    "$(offline_phase_test_path "$phase")" \
    "$worktree/app/${UPGRADE_TAG}_offline_${phase}_test.go"
}

run_phase() {
  local phase="$1"
  local worktree="$2"
  local test_suffix
  local upgrade_list
  local file_phase
  case "$phase" in
    source)
      test_suffix=Source
      upgrade_list=
      file_phase=source
      ;;
    target)
      test_suffix=Target
      upgrade_list="$BOUNDARY_TO"
      file_phase=target
      ;;
    reopen)
      test_suffix=Reopen
      upgrade_list=
      file_phase=source
      ;;
    *) die "unknown offline upgrade phase $phase" ;;
  esac
  install_phase_tests "$worktree" "$file_phase"

  log "Running $UPGRADE_TAG offline $phase phase against $(git -C "$worktree" rev-parse --short HEAD)"
  (
    cd "$worktree"
    local tests
    local listing_stdout="$ARTIFACT_ROOT/$phase-list.stdout"
    local listing_stderr="$ARTIFACT_ROOT/$phase-list.stderr"
    if ! go test \
      -tags="$UPGRADE_TAG,offline_upgrade,upgrade_$file_phase" \
      -list "^Test.*OfflineUpgrade${test_suffix}$" \
      ./app >"$listing_stdout" 2>"$listing_stderr"; then
      cat "$listing_stdout" "$listing_stderr" >&2
      die "$UPGRADE_TAG $phase phase failed to list offline tests"
    fi
    tests="$(awk '/^Test.*OfflineUpgrade(Source|Target|Reopen)$/ { print }' "$listing_stdout")"
    if [[ -z "$tests" ]]; then
      cat "$listing_stderr" >&2
      die "$UPGRADE_TAG $phase phase selected no offline test"
    fi
    rm -f "$listing_stdout" "$listing_stderr"

    UPGRADE_TEST_PHASE="$phase" \
      UPGRADE_TEST_ARTIFACT="$STATE_ARTIFACT" \
      UPGRADE_VERSION_LIST="$upgrade_list" \
      go test \
        -tags="$UPGRADE_TAG,offline_upgrade,upgrade_$file_phase" \
        -run "^Test.*OfflineUpgrade${test_suffix}$" \
        -count=1 \
        -timeout=10m \
        ./app
  ) 2>&1 | tee "$ARTIFACT_ROOT/$phase.log"
}

cleanup() {
  local exit_code=$?
  trap - EXIT
  set +e
  git -C "$REPO_ROOT" worktree remove --force "$SOURCE_WORKTREE" 2>/dev/null
  git -C "$REPO_ROOT" worktree remove --force "$TARGET_WORKTREE" 2>/dev/null
  exit "$exit_code"
}

main() {
  prepare_boundary
  validate_inputs
  mkdir -p "$RUN_ROOT" "$ARTIFACT_ROOT" "$STATE_ARTIFACT"
  exec > >(tee "$ARTIFACT_ROOT/runner.log") 2>&1
  trap cleanup EXIT

  prepare_worktrees
  run_phase source "$SOURCE_WORKTREE"
  run_phase target "$TARGET_WORKTREE"
  run_phase reopen "$SOURCE_WORKTREE"
  log "Offline upgrade $BOUNDARY_FROM -> $BOUNDARY_TO succeeded"
}

main "$@"
