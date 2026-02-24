#!/usr/bin/env bash

# Kill all cryptosim benchmark processes.
# Targets: run.sh, go run main/main.go, and the compiled main binary.

BENCH_PATH="sei-db/state_db/bench/cryptosim"

pids=$(pgrep -f "$BENCH_PATH" 2>/dev/null || true)
pids="$pids $(pgrep -f 'cryptosim/run\.sh' 2>/dev/null || true)"
pids="$pids $(pgrep -f 'cryptosim.*main/main\.go' 2>/dev/null || true)"

# Dedupe and kill
killed=0
for pid in $(echo "$pids" | tr ' ' '\n' | sort -un); do
	[ -n "$pid" ] && [ "$pid" -gt 0 ] 2>/dev/null || continue
	if kill -9 "$pid" 2>/dev/null; then
		echo "Killed PID $pid"
		killed=$((killed + 1))
	fi
done

[ "$killed" -eq 0 ] && echo "No cryptosim benchmark processes found."
