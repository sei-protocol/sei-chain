#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Run a deterministic shard of Go tests.

Usage:
  bash scripts/testing/run-go-test-shard.sh [options] [-- <extra go test args>]

Options:
  --module-dir <dir>        Directory to run go list/go test in (default: .)
  --packages <pattern>      Package pattern for discovery (default: ./...)
  --shard-index <n>         Zero-based shard index (required)
  --shard-count <n>         Number of shards (required)
  --tags <value>            Go build tags to use
  --timeout <duration>      Go test timeout (default: 30m)
  --parallel <n>            go test -parallel value
  --run <regex>             go test -run filter
  --exclude-regex <regex>   Exclude packages matching regex (repeatable)
  --race                    Enable race detector
  --coverage                Enable coverage output
  --covermode <mode>        Coverage mode (default: atomic)
  --coverpkg <pattern>      Coverage package pattern (default: ./...)
  --coverprofile <path>     Coverage output file (default: coverage-shard-<index>.out)
  --list-packages           Only print selected packages, do not run tests
  --help                    Show this help

Examples:
  # Run shard 1/4 with race detector
  bash scripts/testing/run-go-test-shard.sh \
    --module-dir . \
    --shard-index 1 \
    --shard-count 4 \
    --tags "ledger test_ledger_mock" \
    --race

  # Run shard 0/8 with coverage and custom excludes
  bash scripts/testing/run-go-test-shard.sh \
    --shard-index 0 \
    --shard-count 8 \
    --coverage \
    --coverprofile coverage-0.out \
    --exclude-regex '/sei-iavl$'
EOF
}

MODULE_DIR="."
PACKAGE_PATTERN="./..."
SHARD_INDEX=""
SHARD_COUNT=""
TAGS=""
TIMEOUT="30m"
PARALLEL=""
RUN_REGEX=""
RACE=false
COVERAGE=false
COVERMODE="atomic"
COVERPKG="./..."
COVERPROFILE=""
LIST_ONLY=false

EXCLUDE_REGEXES=()
EXTRA_GO_TEST_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --module-dir)
      MODULE_DIR="$2"
      shift 2
      ;;
    --packages)
      PACKAGE_PATTERN="$2"
      shift 2
      ;;
    --shard-index)
      SHARD_INDEX="$2"
      shift 2
      ;;
    --shard-count)
      SHARD_COUNT="$2"
      shift 2
      ;;
    --tags)
      TAGS="$2"
      shift 2
      ;;
    --timeout)
      TIMEOUT="$2"
      shift 2
      ;;
    --parallel)
      PARALLEL="$2"
      shift 2
      ;;
    --run)
      RUN_REGEX="$2"
      shift 2
      ;;
    --exclude-regex)
      EXCLUDE_REGEXES+=("$2")
      shift 2
      ;;
    --race)
      RACE=true
      shift
      ;;
    --coverage)
      COVERAGE=true
      shift
      ;;
    --covermode)
      COVERMODE="$2"
      shift 2
      ;;
    --coverpkg)
      COVERPKG="$2"
      shift 2
      ;;
    --coverprofile)
      COVERPROFILE="$2"
      shift 2
      ;;
    --list-packages)
      LIST_ONLY=true
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --)
      shift
      EXTRA_GO_TEST_ARGS+=("$@")
      break
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$SHARD_INDEX" || -z "$SHARD_COUNT" ]]; then
  echo "--shard-index and --shard-count are required" >&2
  usage
  exit 1
fi

if ! [[ "$SHARD_INDEX" =~ ^[0-9]+$ && "$SHARD_COUNT" =~ ^[0-9]+$ ]]; then
  echo "--shard-index and --shard-count must be non-negative integers" >&2
  exit 1
fi

if (( SHARD_COUNT == 0 )); then
  echo "--shard-count must be greater than zero" >&2
  exit 1
fi

if (( SHARD_INDEX >= SHARD_COUNT )); then
  echo "--shard-index must be less than --shard-count" >&2
  exit 1
fi

if [[ -n "$PARALLEL" ]] && ! [[ "$PARALLEL" =~ ^[0-9]+$ ]]; then
  echo "--parallel must be a non-negative integer" >&2
  exit 1
fi

if [[ -z "$COVERPROFILE" ]]; then
  COVERPROFILE="coverage-shard-${SHARD_INDEX}.out"
fi

cd "$MODULE_DIR"

mapfile -t ALL_PACKAGES < <(
  go list -f '{{ if (or .TestGoFiles .XTestGoFiles) }}{{ .ImportPath }}{{ end }}' "$PACKAGE_PATTERN" \
    | awk 'NF > 0' \
    | sort -u
)

FILTERED_PACKAGES=()
for pkg in "${ALL_PACKAGES[@]}"; do
  include=true
  for rx in "${EXCLUDE_REGEXES[@]}"; do
    if [[ "$pkg" =~ $rx ]]; then
      include=false
      break
    fi
  done
  if [[ "$include" == true ]]; then
    FILTERED_PACKAGES+=("$pkg")
  fi
done

SELECTED_PACKAGES=()
for pkg in "${FILTERED_PACKAGES[@]}"; do
  hash=$(printf '%s' "$pkg" | cksum | awk '{print $1}')
  shard=$(( hash % SHARD_COUNT ))
  if (( shard == SHARD_INDEX )); then
    SELECTED_PACKAGES+=("$pkg")
  fi
done

if [[ "$LIST_ONLY" == true ]]; then
  printf '%s\n' "${SELECTED_PACKAGES[@]}"
  exit 0
fi

echo "Discovered ${#ALL_PACKAGES[@]} testable packages"
echo "After excludes: ${#FILTERED_PACKAGES[@]} packages"
echo "Shard ${SHARD_INDEX}/${SHARD_COUNT}: ${#SELECTED_PACKAGES[@]} packages"

if (( ${#SELECTED_PACKAGES[@]} == 0 )); then
  echo "No packages in this shard, skipping"
  exit 0
fi

GO_TEST_CMD=(go test -mod=readonly -timeout="$TIMEOUT")

if [[ -n "$TAGS" ]]; then
  GO_TEST_CMD+=(-tags="$TAGS")
fi

if [[ "$RACE" == true ]]; then
  GO_TEST_CMD+=(-race)
fi

if [[ -n "$RUN_REGEX" ]]; then
  GO_TEST_CMD+=(-run "$RUN_REGEX")
fi

if [[ -n "$PARALLEL" ]]; then
  GO_TEST_CMD+=(-parallel "$PARALLEL")
fi

if [[ "$COVERAGE" == true ]]; then
  GO_TEST_CMD+=(-covermode="$COVERMODE" -coverprofile="$COVERPROFILE" -coverpkg="$COVERPKG")
fi

if (( ${#EXTRA_GO_TEST_ARGS[@]} > 0 )); then
  GO_TEST_CMD+=("${EXTRA_GO_TEST_ARGS[@]}")
fi

GO_TEST_CMD+=("${SELECTED_PACKAGES[@]}")

echo "Executing:"
printf '  %q' "${GO_TEST_CMD[@]}"
echo

"${GO_TEST_CMD[@]}"
