#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(git rev-parse --show-toplevel)"
readonly RUN_ROOT="$(mktemp -d "${RUNNER_TEMP:-/tmp}/sei-offline-upgrade-compile.XXXXXX")"
readonly WORKTREE_ROOT="$RUN_ROOT/worktrees"
readonly TEST_SETS="$RUN_ROOT/test-sets.tsv"

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  local exit_code=$?
  trap - EXIT
  set +e
  if [[ -d "$WORKTREE_ROOT" ]]; then
    local worktree
    for worktree in "$WORKTREE_ROOT"/*; do
      [[ -d "$worktree" ]] || continue
      git -C "$REPO_ROOT" worktree remove --force "$worktree" 2>/dev/null
    done
  fi
  rm -rf "$RUN_ROOT"
  exit "$exit_code"
}

discover_test_sets() {
  python3 - "$REPO_ROOT/app/tags" "$REPO_ROOT/app" >"$TEST_SETS" <<'PY'
import pathlib
import re
import sys

tags_path = pathlib.Path(sys.argv[1])
app_dir = pathlib.Path(sys.argv[2])
versions = []
for line in tags_path.read_text(encoding="utf-8").splitlines():
    match = re.fullmatch(r"v(\d+)\.(\d+)", line.strip())
    if match:
        versions.append((int(match.group(1)), int(match.group(2)), line.strip()))
versions.sort()

version_by_tag = {}
for index, (major, minor, target) in enumerate(versions):
    tag = f"upgrade_v{major}{minor}"
    if tag in version_by_tag:
        raise SystemExit(f"{tag} is ambiguous between {version_by_tag[tag][2]} and {target}")
    version_by_tag[tag] = (index, major, target)

source_suffix = "_offline_source_test.go"
target_suffix = "_offline_target_test.go"
source_files = {
    path.name.removesuffix(source_suffix): path.relative_to(app_dir).as_posix()
    for path in (app_dir / "testdata").glob(f"upgrade_v*{source_suffix}")
}
target_files = {
    path.name.removesuffix(target_suffix): path.name
    for path in app_dir.glob(f"upgrade_v*{target_suffix}")
}
test_sets = []
for tag in source_files.keys() | target_files.keys():
    if tag not in source_files or tag not in target_files:
        raise SystemExit(f"{tag} must define both offline phase files")
    if tag not in version_by_tag:
        raise SystemExit(f"{tag} does not match a minor version in {tags_path}")
    index, major, target = version_by_tag[tag]
    if index == 0 or versions[index - 1][0] != major:
        raise SystemExit(f"{tag} has no preceding minor release")
    source = versions[index - 1][2]
    test_sets.append((major, versions[index][1], source, target, tag))

for _, _, source, target, tag in sorted(test_sets):
    print(source, target, tag, source_files[tag], target_files[tag], sep="\t")
PY
}

resolve_release() {
  local version="$1"
  local branch="release/$version"
  local ref
  for ref in "refs/remotes/origin/$branch" "refs/heads/$branch"; do
    if git -C "$REPO_ROOT" rev-parse --verify "$ref^{commit}" >/dev/null 2>&1; then
      git -C "$REPO_ROOT" rev-parse --verify "$ref^{commit}"
      return
    fi
  done

  git -C "$REPO_ROOT" fetch --no-tags origin "$branch" >&2
  git -C "$REPO_ROOT" rev-parse --verify FETCH_HEAD
}

prepare_release_worktree() {
  local version="$1"
  local worktree="$2"
  if [[ ! -d "$worktree" ]]; then
    local commit
    commit="$(resolve_release "$version")" ||
      die "unable to resolve release/$version"
    git -C "$REPO_ROOT" worktree add --detach "$worktree" "$commit" >&2
  fi
}

compile_phase() {
  local checkout="$1"
  local source_file="$2"
  local tags="$3"
  local file="${source_file##*/}"
  if [[ "$checkout" != "$REPO_ROOT" ]]; then
    install -m 0644 \
      "$REPO_ROOT/app/upgrade_offline_harness_test.go" \
      "$checkout/app/upgrade_offline_harness_test.go"
    install -m 0644 "$REPO_ROOT/app/$source_file" "$checkout/app/$file"
  fi

  local included
  if ! included="$(
    cd "$checkout"
    go list -tags "$tags" \
      -f '{{range .TestGoFiles}}{{println .}}{{end}}{{range .XTestGoFiles}}{{println .}}{{end}}' \
      ./app
  )"; then
    die "failed to list app tests while compiling $file"
  fi
  grep -Fxq "$file" <<<"$included" ||
    die "$file is not selected by build tags $tags"

  printf '=== Compiling app/%s against %s (-tags %s) ===\n' \
    "$file" "$(git -C "$checkout" rev-parse --short HEAD)" "$tags"
  (
    cd "$checkout"
    go test -tags "$tags" -run '^$' ./app
  )
}

main() {
  mkdir -p "$WORKTREE_ROOT"
  trap cleanup EXIT
  discover_test_sets

  local current_target
  current_target="$(go run ./upgradetest/cmd/boundary to)"
  local source
  local target
  local tag
  local source_file
  local target_file
  local source_checkout
  local target_checkout
  while IFS=$'\t' read -r source target tag source_file target_file; do
    [[ -n "$source" ]] || continue
    source_checkout="$WORKTREE_ROOT/${source//./_}"
    prepare_release_worktree "$source" "$source_checkout"
    compile_phase \
      "$source_checkout" \
      "$source_file" \
      "$tag,offline_upgrade,upgrade_source"

    if [[ "$target" == "$current_target" ]]; then
      target_checkout="$REPO_ROOT"
    else
      target_checkout="$WORKTREE_ROOT/${target//./_}"
      prepare_release_worktree "$target" "$target_checkout"
    fi
    compile_phase \
      "$target_checkout" \
      "$target_file" \
      "$tag,offline_upgrade,upgrade_target"
  done <"$TEST_SETS"
}

main "$@"
