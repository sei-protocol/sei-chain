#!/usr/bin/env bash
set -euo pipefail

# One measurement thread pinned to one core. Set CORE to a core that is NOT
# shared with an active hyperthread sibling and, ideally, isolated on the
# kernel cmdline (isolcpus/nohz_full/rcu_nocbs).
CORE="${GASBENCH_CORE:-3}"

# --- Operator responsibilities the PROCESS CANNOT set -----------------------
# These MUST be set at OS/BIOS before trusting numbers. This runner checks and
# warns; it cannot enforce them. On a dedicated EC2 host, ensure no co-tenants.
#   * Turbo/boost OFF:   echo 1 | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo
#                        (or disable in BIOS) -- else frequency drifts per-sample.
#   * Governor=performance:
#                        sudo cpupower frequency-set -g performance
#   * Deep C-states OFF: boot intel_idle.max_cstate=1 processor.max_cstate=1
#                        (C-state exit latency lands in the tail).
#   * SMT OFF for the measured core, or pin away from its sibling.
#   * Core isolation:    isolcpus=$CORE nohz_full=$CORE rcu_nocbs=$CORE (cmdline).
#   * IRQ affinity moved off $CORE.

warn() { printf 'WARN: %s\n' "$*" >&2; }

if [ -r /sys/devices/system/cpu/intel_pstate/no_turbo ]; then
  [ "$(cat /sys/devices/system/cpu/intel_pstate/no_turbo)" = "1" ] || warn "turbo appears ENABLED"
fi
gov="/sys/devices/system/cpu/cpu${CORE}/cpufreq/scaling_governor"
if [ -r "$gov" ]; then
  [ "$(cat "$gov")" = "performance" ] || warn "cpu${CORE} governor != performance"
fi

export GOMAXPROCS=1
# GASBENCH_ITERS / GASBENCH_WARMUP are left unset here by default:
# gasbench.DefaultConfig() in Go is the single source of truth for those two.
# GASBENCH_COV_CEILING (an advisory health-check, not the acceptance gate --
# see emit.go/diff.go) / GASBENCH_SIGMA_K default in bench_test.go instead (no
# Config field for them). Set any of these in the environment only to
# override for this run.
export GASBENCH_OUT_CSV="${GASBENCH_OUT_CSV:-gasbench.csv}"
export GASBENCH_OUT_NDJSON="${GASBENCH_OUT_NDJSON:-gasbench.ndjson}"

PIN=(env)
if command -v taskset >/dev/null 2>&1; then
  PIN=(taskset -c "$CORE")
else
  warn "taskset not found (non-Linux?); running unpinned -- results are indicative only"
fi

exec "${PIN[@]}" go test ./tools/gasbench/ \
  -run '^$' -bench '^BenchmarkOpcodes$' \
  -benchtime=1x -count="${GASBENCH_COUNT:-10}" -benchmem
