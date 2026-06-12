#!/usr/bin/env bash
#
# Parallel test runner that still produces a valid mochawesome report.
#
# mocha's built-in `--parallel` mode is incompatible with mochawesome: the single
# main-process reporter can't consolidate worker results and emits a corrupt
# `results: [false]`. So instead of mocha-level parallelism we shard the spec files
# into N buckets and run one mocha PROCESS per bucket concurrently. Each process
# writes its own well-formed mochawesome JSON (run-<bucket>.json); report:merge then
# globs them together. This is the same "one JSON per process" model that makes
# mochawesome merging reliable for Cypress.
#
# Env:
#   RPC_JOBS   number of concurrent mocha processes (default 8)
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RPC_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$RPC_DIR"

JOBS="${RPC_JOBS:-8}"
REPORT_DIR="reports/new_rpc"
mkdir -p "$REPORT_DIR"

# Every spec file in a namespace dir (one level deep, e.g. eth/, debug/, sei/...)
# except the sequential bootstrap under _start/. Directory-agnostic so new RPC
# namespace folders are picked up automatically. A plain one-level glob keeps this
# portable to macOS's default bash 3.2 (which lacks `globstar`).
shopt -s nullglob
specs=()
for f in */*.spec.ts; do
    case "$f" in
        _start/*|node_modules/*) continue ;;
    esac
    specs+=("$f")
done
if [ "${#specs[@]}" -eq 0 ]; then
    echo "run-parallel: no spec files found under $RPC_DIR" >&2
    exit 1
fi

# Clear previous run shards/logs (but keep bootstrap.json from the bootstrap phase).
rm -f "$REPORT_DIR"/run-*.json "$REPORT_DIR"/run.json "$REPORT_DIR"/run-*.log

# Prefer the locally-installed mocha to avoid npx resolution overhead per process.
MOCHA_BIN="$RPC_DIR/node_modules/.bin/mocha"
[ -x "$MOCHA_BIN" ] || MOCHA_BIN="npx mocha"

# Round-robin the specs into JOBS buckets so load (esp. the eth/ specs) spreads out.
declare -a buckets
for i in "${!specs[@]}"; do
    b=$(( i % JOBS ))
    buckets[$b]="${buckets[$b]:-} ${specs[$i]}"
done

echo "==> Running ${#specs[@]} spec files across up to $JOBS parallel mocha processes"

pids=()
bucket_ids=()
for b in "${!buckets[@]}"; do
    files="${buckets[$b]}"
    [ -z "${files// /}" ] && continue
    # shellcheck disable=SC2086 -- spec paths/bin are controlled and contain no spaces.
    $MOCHA_BIN --require tsx --timeout 600000 --exit \
        --reporter mochawesome \
        --reporter-options "reportDir=$REPORT_DIR,reportFilename=run-$b,html=false,json=true,overwrite=true" \
        $files \
        > "$REPORT_DIR/run-$b.log" 2>&1 &
    pids+=($!)
    bucket_ids+=("$b")
done

fails=0
for idx in "${!pids[@]}"; do
    if ! wait "${pids[$idx]}"; then
        fails=$((fails + 1))
        echo "==> bucket ${bucket_ids[$idx]} reported failures (see $REPORT_DIR/run-${bucket_ids[$idx]}.log)"
    fi
done

# Surface a combined tail so failures are visible in the runner output.
if [ "$fails" -ne 0 ]; then
    echo "==> $fails/${#pids[@]} buckets had failing tests"
fi

exit "$fails"
