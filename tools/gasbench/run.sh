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

checked_freq=0
if [ -r /sys/devices/system/cpu/intel_pstate/no_turbo ]; then
  checked_freq=1
  [ "$(cat /sys/devices/system/cpu/intel_pstate/no_turbo)" = "1" ] || warn "turbo appears ENABLED"
fi
gov="/sys/devices/system/cpu/cpu${CORE}/cpufreq/scaling_governor"
if [ -r "$gov" ]; then
  checked_freq=1
  [ "$(cat "$gov")" = "performance" ] || warn "cpu${CORE} governor != performance"
fi
# These sysfs paths are x86/Linux-specific and absent on ARM (Graviton) and
# non-Linux. Absence means "could not verify", NOT "clean" -- say so, or the
# operator reads silence as a passing check.
[ "$checked_freq" = "1" ] || warn "cannot verify turbo/governor here (no intel_pstate or cpufreq) -- trust numbers only on a known fixed-frequency host"

export GOMAXPROCS=1
# See README.md "Running it" for the full env-var list and defaults.
export GASBENCH_OUT_CSV="${GASBENCH_OUT_CSV:-gasbench.csv}"
export GASBENCH_OUT_NDJSON="${GASBENCH_OUT_NDJSON:-gasbench.ndjson}"

PIN=(env)
if command -v taskset >/dev/null 2>&1; then
  # taskset -c N aborts outright if CPU index N does not exist (e.g. the
  # default CORE=3 on a 2-core CI runner), so validate against the host and
  # fall back to unpinned like the no-taskset branch instead of failing the run.
  ncpu=$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || echo 1)
  if [ "$CORE" -ge 0 ] 2>/dev/null && [ "$CORE" -lt "$ncpu" ]; then
    PIN=(taskset -c "$CORE")
  else
    warn "GASBENCH_CORE=$CORE out of range (host has $ncpu CPUs); running unpinned -- results are indicative only"
  fi
else
  warn "taskset not found (non-Linux?); running unpinned -- results are indicative only"
fi

exec "${PIN[@]}" go test ./tools/gasbench/ \
  -run '^$' -bench '^BenchmarkOpcodes$' \
  -benchtime=1x -count="${GASBENCH_COUNT:-10}" -benchmem
