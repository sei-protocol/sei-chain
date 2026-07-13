#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# rpc-sc-read-probe.sh
#
# Send EVM JSON-RPC read traffic at one or more Sei nodes to (a) verify the
# state-commitment read path is consistent across backends and (b) measure its
# latency/throughput. State reads use the "latest" block tag so they exercise
# the live SC path (memIAVL / FlatKV) rather than the historical state store.
#
# -----------------------------------------------------------------------------
# TARGETS
# -----------------------------------------------------------------------------
# --targets accepts a comma-separated list. Each entry is one of:
#
#   name=SPEC        explicit name, e.g. node-a=http://1.2.3.4:8545
#   SPEC             name auto-derived from the host
#
# SPEC is one of:
#   http://host:port | https://host:port   plain IP or DNS URL
#   host:port | host                        bare IP/DNS (defaults to http, :8545)
#   k8s://[svc/|pod/]name[:port]            a Kubernetes service (default) or pod
#
# k8s:// targets resolve by RUNNER (see below):
#   RUNNER=k8s   -> in-cluster DNS   http://name:port
#   RUNNER=local -> auto kubectl port-forward to 127.0.0.1 (torn down on exit)
#
# -----------------------------------------------------------------------------
# RUNNER  (where the load generator executes)
# -----------------------------------------------------------------------------
#   k8s   (default) run the generator in an ephemeral in-cluster pod. Reaches
#         in-cluster services directly; reaches external IP/DNS if the pod has
#         egress. Requires kubectl + cluster access.
#   local run the generator on this machine (needs python3). Reaches any
#         IP/DNS directly; reaches k8s:// targets via auto port-forward.
#
# -----------------------------------------------------------------------------
# MODES
# -----------------------------------------------------------------------------
# 1. check (default): correctness. For each target, send one batch of "latest"
#    state reads bracketed by eth_blockNumber sentinels; if the node reports the
#    same height at the start and end of the batch, all reads were served at that
#    height -- independent of network latency. Compare responses captured at the
#    SAME height across targets. Batches that span a block boundary are discarded,
#    not reported as mismatches. Requires >= 2 targets.
#
#      MODE=check CHECK_DURATION=600 REQUESTS_PER_HEIGHT=5 \
#        TARGETS=a=http://10.0.0.1:8545,b=http://10.0.0.2:8545 \
#        ./scripts/rpc-sc-read-probe.sh
#
# 2. monitor: loop check forever (or MONITOR_ITERATIONS times), sleeping
#    MONITOR_INTERVAL seconds between rounds.
#
#      MODE=monitor MONITOR_INTERVAL=60 \
#        TARGETS=a=http://10.0.0.1:8545,b=http://10.0.0.2:8545 \
#        ./scripts/rpc-sc-read-probe.sh
#
# 3. load: per-target throughput/latency. Concurrent workers over keep-alive
#    connections, optional RPS cap and warmup, per-method percentiles.
#
#      MODE=load REQUESTS=5000 LOAD_CONCURRENCY=32 LOAD_SC_ONLY=1 \
#        TARGETS=a=http://10.0.0.1:8545 ./scripts/rpc-sc-read-probe.sh
#
#      MODE=load LOAD_DURATION=300 LOAD_RPS=450 LOAD_CONCURRENCY=32 \
#        TARGETS=a=http://10.0.0.1:8545 ./scripts/rpc-sc-read-probe.sh
#
# -----------------------------------------------------------------------------
# EXAMPLES across target types
# -----------------------------------------------------------------------------
#   # k8s service, in-cluster generator (default runner):
#   MODE=load NS=my-namespace TARGETS='n0=k8s://my-rpc-svc:8545' \
#     ./scripts/rpc-sc-read-probe.sh
#
#   # k8s service from your laptop (auto port-forward):
#   RUNNER=local MODE=load NS=my-namespace TARGETS='n0=k8s://my-rpc-svc:8545' \
#     ./scripts/rpc-sc-read-probe.sh
#
#   # external IP/DNS from your laptop:
#   RUNNER=local MODE=load TARGETS='rpc=https://evm-rpc.sei-apis.com' \
#     ./scripts/rpc-sc-read-probe.sh
#
#   # cross-target consistency check over IP + k8s service:
#   RUNNER=local MODE=check NS=my-namespace \
#     TARGETS='a=http://10.0.0.1:8545,b=k8s://my-rpc-svc:8545' \
#     ./scripts/rpc-sc-read-probe.sh
#
# Machine-readable output: set OUTPUT_JSON=/path to write one JSON object per
# target (load) or per run (check) as JSONL, in addition to the human log.
#
# Every long-option below also has an identically named UPPERCASE env var, e.g.
#   REQUESTS=200 ./scripts/rpc-sc-read-probe.sh
# =============================================================================

# -- runner / cluster --------------------------------------------------------
RUNNER=${RUNNER:-k8s}                 # k8s | local
NS=${NS:-}                            # k8s namespace; empty => kubectl current-context namespace
CHECK_IMAGE=${CHECK_IMAGE:-python:3.12-alpine}

# -- targets -----------------------------------------------------------------
TARGETS=${TARGETS:-}                  # required; comma-separated list (see --help)
DEFAULT_PORT=${DEFAULT_PORT:-8545}

# -- mode --------------------------------------------------------------------
MODE=${MODE:-check}

# -- shared RPC knobs --------------------------------------------------------
RPC_TIMEOUT=${RPC_TIMEOUT:-5}
RPC_RETRIES=${RPC_RETRIES:-3}
PROGRESS_EVERY=${PROGRESS_EVERY:-10}

# -- check knobs -------------------------------------------------------------
CHECK_DURATION=${CHECK_DURATION:-600}
REQUESTS_PER_HEIGHT=${REQUESTS_PER_HEIGHT:-5}
CHECK_POLL=${CHECK_POLL:-0.2}
VERBOSE=${VERBOSE:-0}

# -- monitor knobs -----------------------------------------------------------
MONITOR_INTERVAL=${MONITOR_INTERVAL:-60}
MONITOR_ITERATIONS=${MONITOR_ITERATIONS:-0}

# -- load knobs --------------------------------------------------------------
REQUESTS=${REQUESTS:-50}
LOAD_CONCURRENCY=${LOAD_CONCURRENCY:-8}
LOAD_DURATION=${LOAD_DURATION:-0}     # >0 => run for seconds instead of REQUESTS count
LOAD_RPS=${LOAD_RPS:-0}               # >0 => cap requests/sec per target
LOAD_SC_ONLY=${LOAD_SC_ONLY:-0}
WARMUP=${WARMUP:-50}                  # unmeasured warmup requests per target (0 disables)
MAX_ERROR_RATE=${MAX_ERROR_RATE:-0}   # load fails (exit 1) if error_rate exceeds this (0..1)

# -- output ------------------------------------------------------------------
OUTPUT_JSON=${OUTPUT_JSON:-}

# -- probe payload defaults (override for a known-hot contract/account) ------
CALL_TO=${CALL_TO:-0x0000000000000000000000000000000000000000}
STATE_ADDRESS=${STATE_ADDRESS:-0x0000000000000000000000000000000000000000}
STORAGE_SLOT=${STORAGE_SLOT:-0x0000000000000000000000000000000000000000000000000000000000000000}

usage() {
  cat <<'EOF'
Usage: rpc-sc-read-probe.sh [options]

Send EVM JSON-RPC read requests at one or more nodes to check SC read-path
consistency (check/monitor) or measure latency/throughput (load). State reads
use the "latest" tag to exercise the live state-commitment path.

Runner:
  --runner MODE          k8s | local (default: k8s)
  --namespace NS         Kubernetes namespace for the k8s runner / port-forward
                         (default: kubectl current-context namespace)

Targets (required):
  --targets LIST         Comma-separated. Each entry: name=SPEC | SPEC.
                         SPEC: http(s)://host:port | host[:port] |
                               k8s://[svc/|pod/]name[:port]
  --default-port N       Port used for bare/k8s specs without one (default: 8545)

Mode:
  --mode MODE            check | monitor | load (default: check)
  --compare              Alias for --mode check

Shared:
  --rpc-timeout SEC      Per-RPC timeout (default: 5)
  --rpc-retries N        Per-RPC retries (default: 3)
  --progress-every N     Progress interval; seconds (check) or count (load) (default: 10)
  --output-json PATH     Write machine-readable JSONL results to PATH
                         (one JSON object per target for load / per run for check)

Check/monitor:
  --check-duration SEC   Sampling duration (default: 600)
  --requests-per-height N Requests sampled per node/height (default: 5)
  --check-poll SEC       Backoff between sample attempts (default: 0.2)
  --verbose              Print per-height sampling detail
  --monitor-interval SEC Seconds between monitor rounds (default: 60)
  --monitor-iterations N Monitor rounds; 0 = forever (default: 0)

Load:
  --requests N           Requests per target (default: 50)
  --load-concurrency N   Concurrent workers per target (default: 8)
  --load-duration SEC    Run for seconds instead of --requests; 0 disables (default: 0)
  --load-rps N           Max requests/sec per target; 0 = unlimited
  --load-sc-only         Only send latest state-read RPCs
  --warmup N             Unmeasured warmup requests per target (default: 50)
  --max-error-rate F     Fail if per-target error rate exceeds F (0..1) (default: 0)

Probe payload:
  --call-to ADDR         eth_call target address
  --state-address ADDR   Address for balance/code/storage probes
  --storage-slot HEX     Storage slot for eth_getStorageAt

Every option has an UPPERCASE env-var equivalent, e.g. REQUESTS=200 ...
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --runner) RUNNER=$2; shift 2 ;;
    --namespace) NS=$2; shift 2 ;;
    --mode) MODE=$2; shift 2 ;;
    --compare) MODE=check; shift ;;
    --targets) TARGETS=$2; shift 2 ;;
    --default-port) DEFAULT_PORT=$2; shift 2 ;;
    --rpc-timeout) RPC_TIMEOUT=$2; shift 2 ;;
    --rpc-retries) RPC_RETRIES=$2; shift 2 ;;
    --progress-every) PROGRESS_EVERY=$2; shift 2 ;;
    --output-json) OUTPUT_JSON=$2; shift 2 ;;
    --check-duration) CHECK_DURATION=$2; shift 2 ;;
    --requests-per-height) REQUESTS_PER_HEIGHT=$2; shift 2 ;;
    --check-poll) CHECK_POLL=$2; shift 2 ;;
    --verbose) VERBOSE=1; shift ;;
    --monitor-interval) MONITOR_INTERVAL=$2; shift 2 ;;
    --monitor-iterations) MONITOR_ITERATIONS=$2; shift 2 ;;
    --requests) REQUESTS=$2; shift 2 ;;
    --load-concurrency) LOAD_CONCURRENCY=$2; shift 2 ;;
    --load-duration) LOAD_DURATION=$2; shift 2 ;;
    --load-rps) LOAD_RPS=$2; shift 2 ;;
    --load-sc-only) LOAD_SC_ONLY=1; shift ;;
    --warmup) WARMUP=$2; shift 2 ;;
    --max-error-rate) MAX_ERROR_RATE=$2; shift 2 ;;
    --call-to) CALL_TO=$2; shift 2 ;;
    --state-address) STATE_ADDRESS=$2; shift 2 ;;
    --storage-slot) STORAGE_SLOT=$2; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$MODE" in
  compare) MODE=check ;;
  check|monitor|load) ;;
  *) echo "unknown --mode $MODE (want check|monitor|load)" >&2; exit 2 ;;
esac

case "$RUNNER" in
  k8s|local) ;;
  *) echo "unknown --runner $RUNNER (want k8s|local)" >&2; exit 2 ;;
esac

if [ -z "${TARGETS//[, ]/}" ]; then
  echo "no targets specified; pass --targets or set TARGETS (see --help)" >&2
  exit 2
fi

if [ "$RUNNER" = "local" ] && ! command -v python3 >/dev/null 2>&1; then
  echo "RUNNER=local requires python3 on PATH" >&2
  exit 2
fi

# ---------------------------------------------------------------------------
# Cleanup: kill any port-forwards and remove temp files on exit.
# ---------------------------------------------------------------------------
PF_PIDS=()
CAPTURE_FILE=$(mktemp)
cleanup() {
  local pid
  for pid in "${PF_PIDS[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" >/dev/null 2>&1 || true
  done
  rm -f "$CAPTURE_FILE" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# kubectl wrapper: pass -n only when NS is set, else use the context namespace.
kctl() {
  if [ -n "$NS" ]; then kubectl -n "$NS" "$@"; else kubectl "$@"; fi
}

# start_port_forward KIND NAME REMOTE_PORT -> sets global PF_LOCAL_PORT.
# Lets kubectl pick a free local port (":remote") and parses it from the log.
# MUST run in the main shell (not a $(...) subshell) so the background PID is
# tracked in PF_PIDS for cleanup and the process is not reaped early.
PF_LOCAL_PORT=""
start_port_forward() {
  local kind=$1 name=$2 rport=$3
  local logf
  logf=$(mktemp)
  kctl port-forward "$kind/$name" ":$rport" >"$logf" 2>&1 &
  local pid=$!
  PF_PIDS+=("$pid")
  PF_LOCAL_PORT=""
  local i
  for ((i = 0; i < 100; i++)); do
    PF_LOCAL_PORT=$(sed -n 's/.*Forwarding from 127\.0\.0\.1:\([0-9]\{1,\}\) ->.*/\1/p' "$logf" | head -n1)
    [ -n "$PF_LOCAL_PORT" ] && break
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      echo "port-forward $kind/$name failed:" >&2; cat "$logf" >&2; rm -f "$logf"; return 1
    fi
    sleep 0.1
  done
  rm -f "$logf"
  if [ -z "$PF_LOCAL_PORT" ]; then
    echo "timed out waiting for port-forward $kind/$name" >&2; return 1
  fi
}

# resolve_spec SPEC -> sets global RESOLVED_URL to a URL reachable by RUNNER.
# May start a port-forward (local runner + k8s:// target); must not run inside
# command substitution.
RESOLVED_URL=""
resolve_spec() {
  local spec=$1
  case "$spec" in
    http://*|https://*) RESOLVED_URL=$spec ;;
    k8s://*)
      local rest=${spec#k8s://} kind=svc host port
      case "$rest" in
        svc/*) kind=svc; rest=${rest#svc/} ;;
        pod/*) kind=pod; rest=${rest#pod/} ;;
      esac
      if [[ "$rest" == *:* ]]; then host=${rest%%:*}; port=${rest##*:}; else host=$rest; port=$DEFAULT_PORT; fi
      if [ "$RUNNER" = "local" ]; then
        start_port_forward "$kind" "$host" "$port" || exit 1
        echo "port-forward $kind/$host:$port -> 127.0.0.1:$PF_LOCAL_PORT" >&2
        RESOLVED_URL="http://127.0.0.1:$PF_LOCAL_PORT"
      else
        RESOLVED_URL="http://$host:$port"
      fi
      ;;
    *)
      if [[ "$spec" == *:* ]]; then RESOLVED_URL="http://$spec"; else RESOLVED_URL="http://$spec:$DEFAULT_PORT"; fi
      ;;
  esac
}

# derive_name SPEC -> a sanitized short name from the host[:port] portion. The
# port is kept (rendered as a '-') so two targets on the same host but different
# ports (e.g. 10.0.0.1:8545 and 10.0.0.1:9545) derive distinct names instead of
# colliding -- a collision would silently drop one target in check mode.
derive_name() {
  local s=$1
  s=${s#*://}; s=${s#svc/}; s=${s#pod/}
  s=${s%%/*}
  printf '%s' "$s" | tr -c 'A-Za-z0-9_.-' '-' | sed 's/-*$//'
}

# Parse TARGETS into parallel arrays target_names / target_urls.
target_names=()
target_urls=()
parse_targets() {
  local entry name spec existing
  local -a entries
  # IFS=',' is scoped to this read only (bash prefix assignment), so the global
  # IFS is left untouched for whitespace splitting elsewhere.
  IFS=',' read -r -a entries <<< "$TARGETS"
  for entry in "${entries[@]}"; do
    [ -z "$entry" ] && continue
    case "$entry" in
      *=*)      name=${entry%%=*}; spec=${entry#*=} ;;
      *)        spec=$entry; name=$(derive_name "$spec") ;;
    esac
    # Names key the per-target samples in check mode; a duplicate would collapse
    # two targets to one entry and make the comparison silently pass. Reject it
    # rather than report a misconfiguration as CONSISTENT. (Guard the loop for
    # the empty-array case so it is safe under `set -u` on bash 3.2.)
    if [ "${#target_names[@]}" -gt 0 ]; then
      for existing in "${target_names[@]}"; do
        if [ "$existing" = "$name" ]; then
          echo "duplicate target name '$name' in TARGETS: each target must resolve to a unique name (use name=SPEC to disambiguate)" >&2
          exit 2
        fi
      done
    fi
    resolve_spec "$spec"
    target_names+=("$name")
    target_urls+=("$RESOLVED_URL")
  done
  if [ "${#target_names[@]}" -eq 0 ]; then
    echo "no targets parsed from TARGETS='$TARGETS'" >&2; exit 2
  fi
}

# ===========================================================================
# Shared Python preamble, prepended to both LOAD_PY and CHECK_PY so load and
# check exercise the exact same request generator (no silent drift between the
# two method mixes). All requests use the "latest" tag to hit the live SC path.
# choose_request references CALL_TO/STATE_ADDRESS/STORAGE_SLOT, which each
# program defines from the environment before the generator is ever called.
# ===========================================================================
read -r -d '' COMMON_PY <<'PYEOF' || true
def choose_request(i, sc_only=False):
    bucket = (i * 37) % 100
    if sc_only:
        if bucket < 44:
            return "eth_call", [{"to": CALL_TO, "data": "0x"}, "latest"]
        if bucket < 64:
            return "eth_getBalance", [STATE_ADDRESS, "latest"]
        if bucket < 82:
            return "eth_getStorageAt", [STATE_ADDRESS, STORAGE_SLOT, "latest"]
        return "eth_getCode", [STATE_ADDRESS, "latest"]
    if bucket < 44:
        return "eth_call", [{"to": CALL_TO, "data": "0x"}, "latest"]
    if bucket < 64:
        return "eth_getBlockByNumber", ["latest", False]
    if bucket < 70:
        return "eth_getLogs", [{"fromBlock": "latest", "toBlock": "latest", "address": STATE_ADDRESS}]
    if bucket < 75:
        return "eth_getStorageAt", [STATE_ADDRESS, STORAGE_SLOT, "latest"]
    if bucket < 79:
        return "eth_blockNumber", []
    if bucket < 82:
        return "eth_feeHistory", ["0x5", "latest", []]
    if bucket < 92:
        return "eth_getBalance", [STATE_ADDRESS, "latest"]
    return "eth_getCode", [STATE_ADDRESS, "latest"]
PYEOF

# ===========================================================================
# Load generator (Python). Keep-alive connections, optional warmup + RPS cap,
# interpolated percentiles, human + JSON output.
# ===========================================================================
read -r -d '' LOAD_PY <<'PYEOF' || true
import json
import os
import threading
import time
import http.client
from urllib.parse import urlparse

TARGET_NAME = os.environ["TARGET_NAME"]
RPC_URL = os.environ["RPC_URL"]
REQUESTS = int(os.environ["REQUESTS"])
LOAD_CONCURRENCY = int(os.environ["LOAD_CONCURRENCY"])
LOAD_DURATION = float(os.environ["LOAD_DURATION"])
LOAD_RPS = float(os.environ["LOAD_RPS"])
LOAD_SC_ONLY = os.environ["LOAD_SC_ONLY"] == "1"
WARMUP = int(os.environ["WARMUP"])
PROGRESS_EVERY = int(os.environ["PROGRESS_EVERY"])
RPC_TIMEOUT = float(os.environ["RPC_TIMEOUT"])
RPC_RETRIES = int(os.environ["RPC_RETRIES"])
MAX_ERROR_RATE = float(os.environ["MAX_ERROR_RATE"])
CALL_TO = os.environ["CALL_TO"]
STATE_ADDRESS = os.environ["STATE_ADDRESS"]
STORAGE_SLOT = os.environ["STORAGE_SLOT"]

_p = urlparse(RPC_URL)
IS_HTTPS = _p.scheme == "https"
HOST = _p.hostname
PORT = _p.port or (443 if IS_HTTPS else 80)
PATH = (_p.path or "/") + (("?" + _p.query) if _p.query else "")

# Must match exactly the methods choose_request() can emit: METHOD_COUNTS is
# printed for every entry, so a method that is never sent would misleadingly
# report =0, and a method that is sent but missing here would KeyError.
methods = [
    "eth_call", "eth_getBalance", "eth_getStorageAt", "eth_getCode",
    "eth_getBlockByNumber", "eth_getLogs", "eth_blockNumber", "eth_feeHistory",
]


class Client:
    """Per-thread persistent HTTP/1.1 connection (keep-alive) with reconnect."""

    def __init__(self):
        self.conn = None

    def _connect(self):
        cls = http.client.HTTPSConnection if IS_HTTPS else http.client.HTTPConnection
        self.conn = cls(HOST, PORT, timeout=RPC_TIMEOUT)

    def post(self, body):
        last = None
        for attempt in range(1, RPC_RETRIES + 1):
            try:
                if self.conn is None:
                    self._connect()
                self.conn.request(
                    "POST", PATH, body=body,
                    headers={"Content-Type": "application/json", "Connection": "keep-alive"},
                )
                resp = self.conn.getresponse()
                data = resp.read()
                if resp.status != 200:
                    raise RuntimeError("http %d" % resp.status)
                return data
            except Exception as exc:  # noqa: BLE001 - stdlib script
                last = exc
                try:
                    if self.conn is not None:
                        self.conn.close()
                except Exception:
                    pass
                self.conn = None
                if attempt < RPC_RETRIES:
                    time.sleep(0.05)
        raise last


_tls = threading.local()


def client():
    c = getattr(_tls, "client", None)
    if c is None:
        c = Client()
        _tls.client = c
    return c


def encode(i):
    method, params = choose_request(i, LOAD_SC_ONLY)
    body = json.dumps(
        {"jsonrpc": "2.0", "id": i, "method": method, "params": params},
        separators=(",", ":"),
    ).encode()
    return method, body


def do_request(i):
    method, body = encode(i)
    try:
        text = client().post(body)
        obj = json.loads(text)
        success = "error" not in obj
    except Exception:
        success = False
    return method, success


def warmup():
    if WARMUP <= 0:
        return
    counter = [0]
    lock = threading.Lock()

    def w():
        while True:
            with lock:
                if counter[0] >= WARMUP:
                    return
                counter[0] += 1
                i = counter[0]
            do_request(i)

    n = min(LOAD_CONCURRENCY, max(1, WARMUP))
    ts = [threading.Thread(target=w, daemon=True) for _ in range(n)]
    for t in ts:
        t.start()
    for t in ts:
        t.join()
    print("LOAD_WARMUP target=%s warmup_requests=%d done" % (TARGET_NAME, WARMUP), flush=True)


method_counts = {m: 0 for m in methods}
method_ok = {m: 0 for m in methods}
method_err = {m: 0 for m in methods}
method_latencies_ms = {m: [] for m in methods}
latencies_ms = []
ok = 0
err = 0
next_id = 1
lock = threading.Lock()
stop_at = None
rate_lock = threading.Lock()
next_send_at = 0.0


def next_request_id():
    global next_id
    with lock:
        if stop_at is None and next_id > REQUESTS:
            return None
        if stop_at is not None and time.monotonic() >= stop_at:
            return None
        i = next_id
        next_id += 1
        return i


def wait_for_rate_slot():
    global next_send_at
    if LOAD_RPS <= 0:
        return
    interval = 1.0 / LOAD_RPS
    with rate_lock:
        now = time.monotonic()
        scheduled = max(now, next_send_at)
        next_send_at = scheduled + interval
    delay = scheduled - time.monotonic()
    if delay > 0:
        time.sleep(delay)


def worker():
    global ok, err
    while True:
        i = next_request_id()
        if i is None:
            return
        # Build the request and parse the response OUTSIDE the timed region so
        # the measurement reflects network/server latency, not client-side JSON
        # serialization/parsing (which also holds the GIL).
        method, body = encode(i)
        c = client()
        wait_for_rate_slot()
        # The rate slot can sleep past the duration deadline: with
        # LOAD_CONCURRENCY >> LOAD_RPS every worker reserves a slot up front and
        # would otherwise fire it after stop_at, overrunning LOAD_DURATION by
        # ~(concurrency-1)/LOAD_RPS seconds. Re-check the deadline here so a
        # duration-bounded run stops near it.
        if stop_at is not None and time.monotonic() >= stop_at:
            return
        start = time.perf_counter()
        try:
            text = c.post(body)
            failed = False
        except Exception:  # noqa: BLE001 - stdlib script
            text, failed = None, True
        elapsed = (time.perf_counter() - start) * 1000.0
        if failed:
            success = False
        else:
            try:
                success = "error" not in json.loads(text)
            except Exception:  # noqa: BLE001 - stdlib script
                success = False
        with lock:
            latencies_ms.append(elapsed)
            method_latencies_ms[method].append(elapsed)
            method_counts[method] += 1
            if success:
                ok += 1
                method_ok[method] += 1
            else:
                err += 1
                method_err[method] += 1
            completed = ok + err
        # Print progress outside the lock so the stdout write never stalls other
        # workers waiting on the shared stats lock.
        if PROGRESS_EVERY > 0 and completed % PROGRESS_EVERY == 0:
            print("LOAD_PROGRESS target=%s completed=%d ok=%d error=%d"
                  % (TARGET_NAME, completed, ok, err), flush=True)


def percentile(sorted_values, pct):
    if not sorted_values:
        return 0.0
    if len(sorted_values) == 1:
        return sorted_values[0]
    k = (len(sorted_values) - 1) * pct / 100.0
    f = int(k)
    c = min(f + 1, len(sorted_values) - 1)
    if f == c:
        return sorted_values[f]
    return sorted_values[f] + (sorted_values[c] - sorted_values[f]) * (k - f)


print(
    "LOAD_START target=%s url=%s requests=%d duration=%.1fs concurrency=%d "
    "rps_limit=%.1f timeout=%.1fs sc_only=%d warmup=%d"
    % (TARGET_NAME, RPC_URL, REQUESTS, LOAD_DURATION, LOAD_CONCURRENCY,
       LOAD_RPS, RPC_TIMEOUT, int(LOAD_SC_ONLY), WARMUP),
    flush=True,
)

warmup()

if LOAD_DURATION > 0:
    stop_at = time.monotonic() + LOAD_DURATION
if LOAD_RPS > 0:
    next_send_at = time.monotonic()

started = time.perf_counter()
threads = [threading.Thread(target=worker, daemon=True) for _ in range(LOAD_CONCURRENCY)]
for t in threads:
    t.start()
for t in threads:
    t.join()
elapsed_s = time.perf_counter() - started

latencies_ms.sort()
total = ok + err
rps = total / elapsed_s if elapsed_s > 0 else 0.0
error_rate = (err / total) if total else 0.0

print(
    "LOAD_SUMMARY target=%s requests=%d ok=%d error=%d elapsed_s=%.3f rps=%.2f "
    "latency_ms_p50=%.2f p95=%.2f p99=%.2f max=%.2f"
    % (TARGET_NAME, total, ok, err, elapsed_s, rps,
       percentile(latencies_ms, 50), percentile(latencies_ms, 95),
       percentile(latencies_ms, 99), max(latencies_ms) if latencies_ms else 0.0),
    flush=True,
)
print("METHOD_COUNTS " + " ".join("%s=%d" % (m, method_counts[m]) for m in methods), flush=True)

method_json = {}
for method in methods:
    values = sorted(method_latencies_ms[method])
    if not values:
        continue
    method_json[method] = {
        "count": len(values), "ok": method_ok[method], "error": method_err[method],
        "p50": round(percentile(values, 50), 3), "p95": round(percentile(values, 95), 3),
        "p99": round(percentile(values, 99), 3), "max": round(max(values), 3),
    }
    print(
        "METHOD_LATENCY target=%s method=%s count=%d ok=%d error=%d p50=%.2f p95=%.2f p99=%.2f max=%.2f"
        % (TARGET_NAME, method, len(values), method_ok[method], method_err[method],
           percentile(values, 50), percentile(values, 95), percentile(values, 99), max(values)),
        flush=True,
    )

summary = {
    "kind": "load", "target": TARGET_NAME, "url": RPC_URL,
    "requests": total, "ok": ok, "error": err, "error_rate": round(error_rate, 6),
    "elapsed_s": round(elapsed_s, 3), "rps": round(rps, 2),
    "concurrency": LOAD_CONCURRENCY, "rps_limit": LOAD_RPS, "sc_only": int(LOAD_SC_ONLY),
    "latency_ms": {
        "p50": round(percentile(latencies_ms, 50), 3),
        "p95": round(percentile(latencies_ms, 95), 3),
        "p99": round(percentile(latencies_ms, 99), 3),
        "max": round(max(latencies_ms) if latencies_ms else 0.0, 3),
    },
    "methods": method_json,
}
print("LOAD_RESULT_JSON " + json.dumps(summary, separators=(",", ":")), flush=True)

if total and error_rate > MAX_ERROR_RATE:
    print("LOAD_FAIL target=%s error_rate=%.6f exceeds max_error_rate=%.6f"
          % (TARGET_NAME, error_rate, MAX_ERROR_RATE), flush=True)
    raise SystemExit(1)
PYEOF

# ===========================================================================
# Cross-target consistency checker (Python). Height-synchronized sampling with
# keep-alive connections; human + JSON output.
# ===========================================================================
read -r -d '' CHECK_PY <<'PYEOF' || true
import json
import os
import threading
import time
import http.client
from urllib.parse import urlparse

CHECK_DURATION = float(os.environ["CHECK_DURATION"])
REQUESTS_PER_HEIGHT = int(os.environ["REQUESTS_PER_HEIGHT"])
CHECK_POLL = float(os.environ["CHECK_POLL"])
RPC_TIMEOUT = float(os.environ["RPC_TIMEOUT"])
RPC_RETRIES = int(os.environ["RPC_RETRIES"])
PROGRESS_EVERY = int(os.environ["PROGRESS_EVERY"])
VERBOSE = os.environ["VERBOSE"] == "1"
CALL_TO = os.environ["CALL_TO"]
STATE_ADDRESS = os.environ["STATE_ADDRESS"]
STORAGE_SLOT = os.environ["STORAGE_SLOT"]

_tls = threading.local()


def _conns():
    d = getattr(_tls, "d", None)
    if d is None:
        d = {}
        _tls.d = d
    return d


def post(url, body):
    p = urlparse(url)
    https = p.scheme == "https"
    host = p.hostname
    port = p.port or (443 if https else 80)
    path = (p.path or "/") + (("?" + p.query) if p.query else "")
    key = (host, port, https)
    d = _conns()
    last = None
    for attempt in range(1, RPC_RETRIES + 1):
        try:
            conn = d.get(key)
            if conn is None:
                cls = http.client.HTTPSConnection if https else http.client.HTTPConnection
                conn = cls(host, port, timeout=RPC_TIMEOUT)
                d[key] = conn
            conn.request(
                "POST", path, body=body,
                headers={"Content-Type": "application/json", "Connection": "keep-alive"},
            )
            resp = conn.getresponse()
            data = resp.read()
            if resp.status != 200:
                raise RuntimeError("http %d" % resp.status)
            return data.decode()
        except Exception as exc:  # noqa: BLE001 - stdlib script
            last = exc
            c = d.pop(key, None)
            try:
                if c is not None:
                    c.close()
            except Exception:
                pass
            if attempt < RPC_RETRIES:
                time.sleep(0.2)
    raise last


def rpc_json(url, method, params, request_id):
    text = post(url, json.dumps(
        {"jsonrpc": "2.0", "id": request_id, "method": method, "params": params},
        separators=(",", ":")).encode())
    return text, json.loads(text)


def block_number(url):
    _, obj = rpc_json(url, "eth_blockNumber", [], 0)
    return int(obj["result"], 16)


def parse_targets():
    targets = []
    for line in os.environ["TARGETS_PAYLOAD"].splitlines():
        parts = line.split()
        if len(parts) >= 2:
            targets.append((parts[0], parts[1]))
    if len(targets) < 2:
        # A misconfiguration, not a mismatch: exit 2 so it never masquerades as
        # MISMATCH (exit 1).
        print("check mode requires at least two targets", flush=True)
        raise SystemExit(2)
    # Samples are keyed by name, so duplicate names would collapse two targets to
    # one entry and compare the baseline against itself -- a silent false pass.
    # The bash layer already rejects this; guard here too since the comparison's
    # correctness depends on it.
    names = [name for name, _ in targets]
    if len(set(names)) != len(names):
        print("check mode requires unique target names; got duplicates in %r" % names, flush=True)
        raise SystemExit(2)
    return targets


TARGETS = parse_targets()


def vlog(message):
    if VERBOSE:
        print(message, flush=True)


def short(text, limit=220):
    return text if len(text) <= limit else text[:limit] + "..."


def capture_coherent(name, url):
    """Capture one latency-independent sample of REQUESTS_PER_HEIGHT reads.

    Send a single batch of "latest" state reads bracketed by eth_blockNumber
    sentinels. The node processes the batch server-side (near-instant), so if
    the height reported at the start and end of the batch is identical, every
    "latest" read in between was served at that same height -- regardless of
    client/network latency.

    Returns (height, rows) on success, ("crossed", None) if the batch spanned a
    block boundary, or (None, None) on error.
    """
    batch = [{"jsonrpc": "2.0", "id": "bn_start", "method": "eth_blockNumber", "params": []}]
    method_by_id = {}
    for i in range(1, REQUESTS_PER_HEIGHT + 1):
        method, params = choose_request(i)
        method_by_id[i] = method
        batch.append({"jsonrpc": "2.0", "id": i, "method": method, "params": params})
    batch.append({"jsonrpc": "2.0", "id": "bn_end", "method": "eth_blockNumber", "params": []})

    try:
        batch_text = post(url, json.dumps(batch, separators=(",", ":")).encode())
        batch_obj = json.loads(batch_text)
        if not isinstance(batch_obj, list):
            batch_obj = [batch_obj]
    except Exception as exc:  # noqa: BLE001
        vlog("SAMPLE_RPC_ERROR node=%s error=%s" % (name, exc))
        return None, None

    by_id = {}
    for item in batch_obj:
        if isinstance(item, dict) and "id" in item:
            by_id[item["id"]] = item

    def sentinel_height(key):
        item = by_id.get(key)
        if not isinstance(item, dict) or "result" not in item:
            return None
        try:
            return int(item["result"], 16)
        except (ValueError, TypeError):
            return None

    h_start = sentinel_height("bn_start")
    h_end = sentinel_height("bn_end")
    if h_start is None or h_end is None:
        vlog("SAMPLE_RPC_ERROR node=%s error=missing/invalid blockNumber sentinel" % name)
        return None, None
    if h_start != h_end:
        return "crossed", None

    rows = []
    for i in range(1, REQUESTS_PER_HEIGHT + 1):
        item = by_id.get(i, {"jsonrpc": "2.0", "id": i, "error": "missing batch response"})
        text = json.dumps(item, sort_keys=True, separators=(",", ":"))
        rows.append({"id": i, "method": method_by_id[i], "text": text})
    return h_start, rows


target_names = [name for name, _ in TARGETS]


def initial_height():
    """Highest current height across targets, tolerating pre-flight failures.

    block_number() raises on an unreachable target, a non-200, a JSON-RPC error
    object, or a non-hex result. Letting that propagate would abort with a
    Python traceback and exit code 1 -- indistinguishable from a MISMATCH. Skip
    the targets that fail here; if none respond, bail as INCONCLUSIVE (exit 2)
    so the 0/1/2 verdict contract holds.
    """
    heights = []
    for name, url in TARGETS:
        try:
            heights.append(block_number(url))
        except Exception as exc:  # noqa: BLE001 - stdlib script
            print("CHECK_PREFLIGHT_ERROR node=%s error=%s" % (name, exc), flush=True)
    if not heights:
        print(
            "CHECK_VERDICT verdict=INCONCLUSIVE nothing_compared=1 -- no target answered the "
            "pre-flight eth_blockNumber, so consistency was NOT verified (this is not a pass).",
            flush=True,
        )
        raise SystemExit(2)
    return max(heights)


start_height = initial_height()
deadline = time.time() + CHECK_DURATION
samples = {}
sample_lock = threading.Lock()
stop_event = threading.Event()
per_node_samples = {name: 0 for name in target_names}
per_node_crossed = {name: 0 for name in target_names}
per_node_errors = {name: 0 for name in target_names}

print(
    "CHECK_START duration=%.0fs requests_per_height=%d targets=%d start_height=%d"
    % (CHECK_DURATION, REQUESTS_PER_HEIGHT, len(TARGETS), start_height),
    flush=True,
)


def watch_worker(name, url):
    seen = set()
    while time.time() < deadline and not stop_event.is_set():
        height, rows = capture_coherent(name, url)
        if height is None:
            per_node_errors[name] += 1
            time.sleep(CHECK_POLL)
            continue
        if height == "crossed":
            per_node_crossed[name] += 1
            vlog("SAMPLE_CROSSED node=%s (batch spanned a block boundary)" % name)
            time.sleep(CHECK_POLL)
            continue
        if height < start_height or height in seen:
            time.sleep(CHECK_POLL)
            continue
        seen.add(height)
        with sample_lock:
            samples.setdefault(height, {})[name] = rows
            per_node_samples[name] += 1
        vlog("SAMPLE height=%d node=%s requests=%d" % (height, name, len(rows)))


threads = [threading.Thread(target=watch_worker, args=(name, url), daemon=True) for name, url in TARGETS]
for thread in threads:
    thread.start()

next_progress = time.time() + max(1, PROGRESS_EVERY)
while time.time() < deadline:
    time.sleep(0.5)
    if time.time() >= next_progress:
        with sample_lock:
            comparable_now = sum(1 for node_rows in samples.values() if all(n in node_rows for n in target_names))
            heights_seen = len(samples)
        elapsed = int(CHECK_DURATION - max(0, deadline - time.time()))
        print(
            "CHECK_PROGRESS elapsed_s=%d heights_seen=%d comparable_heights=%d "
            % (elapsed, heights_seen, comparable_now)
            + " ".join("%s_samples=%d" % (n, per_node_samples[n]) for n in target_names),
            flush=True,
        )
        next_progress = time.time() + max(1, PROGRESS_EVERY)

stop_event.set()
for thread in threads:
    thread.join(timeout=2)

matches = 0
mismatches = 0
comparable_heights = 0
incomplete_heights = 0
baseline_name = target_names[0]

with sample_lock:
    snapshot = dict(samples)

for height in sorted(snapshot):
    node_rows = snapshot[height]
    if not all(name in node_rows for name in target_names):
        incomplete_heights += 1
        continue
    comparable_heights += 1
    baseline_rows = node_rows[baseline_name]
    for idx, baseline in enumerate(baseline_rows):
        request_ok = True
        for name in target_names[1:]:
            row = node_rows[name][idx]
            if row["text"] != baseline["text"]:
                request_ok = False
                if mismatches < 5:
                    print(
                        "MISMATCH height=%d id=%s method=%s baseline=%s target=%s"
                        % (height, baseline["id"], baseline["method"], baseline_name, name),
                        flush=True,
                    )
                    print("  %s=%s" % (baseline_name, short(baseline["text"])), flush=True)
                    print("  %s=%s" % (name, short(row["text"])), flush=True)
        if request_ok:
            matches += 1
        else:
            mismatches += 1

print(
    "CHECK_SUMMARY duration=%.0fs start_height=%d heights_seen=%d comparable_heights=%d "
    "skipped_incomplete=%d requests_per_height=%d matched=%d mismatched=%d "
    % (CHECK_DURATION, start_height, len(snapshot), comparable_heights,
       incomplete_heights, REQUESTS_PER_HEIGHT, matches, mismatches)
    + " ".join("%s_samples=%d" % (n, per_node_samples[n]) for n in target_names) + " "
    + " ".join("%s_crossed=%d" % (n, per_node_crossed[n]) for n in target_names) + " "
    + " ".join("%s_errors=%d" % (n, per_node_errors[n]) for n in target_names),
    flush=True,
)

# Verdict + distinct exit codes so callers can tell the three outcomes apart:
#   CONSISTENT (0)   we compared samples and everything agreed
#   MISMATCH   (1)   targets returned different results at the SAME height
#   INCONCLUSIVE (2) nothing was comparable, so consistency was NOT verified
if mismatches:
    verdict, exit_code = "MISMATCH", 1
elif comparable_heights == 0:
    verdict, exit_code = "INCONCLUSIVE", 2
else:
    verdict, exit_code = "CONSISTENT", 0

summary = {
    "kind": "check", "verdict": verdict,
    "start_height": start_height, "heights_seen": len(snapshot),
    "comparable_heights": comparable_heights, "skipped_incomplete": incomplete_heights,
    "requests_per_height": REQUESTS_PER_HEIGHT, "matched": matches, "mismatched": mismatches,
    "targets": target_names,
    "per_node_samples": per_node_samples, "per_node_crossed": per_node_crossed,
    "per_node_errors": per_node_errors,
}
print("CHECK_RESULT_JSON " + json.dumps(summary, separators=(",", ":")), flush=True)

if verdict == "INCONCLUSIVE":
    print(
        "CHECK_VERDICT verdict=INCONCLUSIVE nothing_compared=1 -- no height was sampled "
        "on ALL targets, so consistency was NOT verified (this is not a pass). Likely "
        "causes: targets too far apart in height, slow methods causing crossed batches, "
        "or --check-duration too short. Inspect per-node samples/crossed/errors above.",
        flush=True,
    )
elif verdict == "MISMATCH":
    print(
        "CHECK_VERDICT verdict=MISMATCH matched=%d mismatched=%d -- targets returned "
        "different results at the SAME height (see MISMATCH lines above)."
        % (matches, mismatches),
        flush=True,
    )
else:
    print(
        "CHECK_VERDICT verdict=CONSISTENT matched=%d comparable_heights=%d -- all reads "
        "agreed at every compared height." % (matches, comparable_heights),
        flush=True,
    )

raise SystemExit(exit_code)
PYEOF

# Prepend the shared generator so both programs define choose_request().
LOAD_PY="$COMMON_PY
$LOAD_PY"
CHECK_PY="$COMMON_PY
$CHECK_PY"

# ---------------------------------------------------------------------------
# dispatch_python CODE KEY=VAL...  -> runs CODE locally or in an ephemeral pod,
# streaming stdout to the terminal and appending it to CAPTURE_FILE.
# ---------------------------------------------------------------------------
dispatch_python() {
  local code=$1
  shift
  if [ "$RUNNER" = "local" ]; then
    printf '%s' "$code" | env "$@" python3 - | tee -a "$CAPTURE_FILE"
  else
    local runner="rpc-probe-$(date +%s)-$$-${RANDOM:-0}"
    local -a envflags=()
    local kv
    for kv in "$@"; do
      envflags+=(--env="$kv")
    done
    printf '%s' "$code" | kctl run "$runner" \
      --rm -i --restart=Never --image="$CHECK_IMAGE" "${envflags[@]}" \
      --command -- python3 - \
      2> >(sed -e '/websocket.go.*Unknown stream id/d' -e '/^pod ".*" deleted$/d' -e "/^If you don't see a command prompt, try pressing enter\\.$/d" >&2) \
      | sed -e '/^pod ".*" deleted$/d' -e "/^If you don't see a command prompt, try pressing enter\\.$/d" \
      | tee -a "$CAPTURE_FILE"
  fi
}

run_target() {
  local name=$1 url=$2
  echo "== load target=$name url=$url requests=$REQUESTS concurrency=$LOAD_CONCURRENCY duration=$LOAD_DURATION rps_limit=$LOAD_RPS sc_only=$LOAD_SC_ONLY warmup=$WARMUP =="
  dispatch_python "$LOAD_PY" \
    "TARGET_NAME=$name" "RPC_URL=$url" "REQUESTS=$REQUESTS" \
    "LOAD_CONCURRENCY=$LOAD_CONCURRENCY" "LOAD_DURATION=$LOAD_DURATION" \
    "LOAD_RPS=$LOAD_RPS" "LOAD_SC_ONLY=$LOAD_SC_ONLY" "WARMUP=$WARMUP" \
    "PROGRESS_EVERY=$PROGRESS_EVERY" "RPC_TIMEOUT=$RPC_TIMEOUT" "RPC_RETRIES=$RPC_RETRIES" \
    "MAX_ERROR_RATE=$MAX_ERROR_RATE" "CALL_TO=$CALL_TO" \
    "STATE_ADDRESS=$STATE_ADDRESS" "STORAGE_SLOT=$STORAGE_SLOT"
}

run_load_targets() {
  local rc=0 i
  for i in "${!target_names[@]}"; do
    run_target "${target_names[$i]}" "${target_urls[$i]}" || rc=1
  done
  return $rc
}

build_check_targets_payload() {
  check_targets_payload=""
  local i
  for i in "${!target_names[@]}"; do
    check_targets_payload="${check_targets_payload}${target_names[$i]} ${target_urls[$i]}
"
  done
}

run_check_once() {
  build_check_targets_payload
  echo "== check targets=${TARGETS} duration=${CHECK_DURATION}s requests_per_height=$REQUESTS_PER_HEIGHT =="
  dispatch_python "$CHECK_PY" \
    "CHECK_DURATION=$CHECK_DURATION" "REQUESTS_PER_HEIGHT=$REQUESTS_PER_HEIGHT" \
    "CHECK_POLL=$CHECK_POLL" "RPC_TIMEOUT=$RPC_TIMEOUT" "RPC_RETRIES=$RPC_RETRIES" \
    "PROGRESS_EVERY=$PROGRESS_EVERY" "VERBOSE=$VERBOSE" "CALL_TO=$CALL_TO" \
    "STATE_ADDRESS=$STATE_ADDRESS" "STORAGE_SLOT=$STORAGE_SLOT" \
    "TARGETS_PAYLOAD=$check_targets_payload"
}

run_monitor() {
  local iteration=1 failures=0
  while :; do
    printf 'MONITOR_ITERATION iteration=%s timestamp=%s mode=check\n' "$iteration" "$(date -u +%FT%TZ)"
    if run_check_once; then
      printf 'MONITOR_RESULT iteration=%s result=ok\n' "$iteration"
    else
      failures=$((failures + 1))
      printf 'MONITOR_RESULT iteration=%s result=failed failures=%s\n' "$iteration" "$failures"
    fi
    if [ "$MONITOR_ITERATIONS" -gt 0 ] && [ "$iteration" -ge "$MONITOR_ITERATIONS" ]; then
      break
    fi
    iteration=$((iteration + 1))
    sleep "$MONITOR_INTERVAL"
  done
  [ "$failures" -eq 0 ]
}

parse_targets

rc=0
case "$MODE" in
  check) run_check_once || rc=$? ;;
  monitor) run_monitor || rc=$? ;;
  load)
    echo "== load mode: per-target request run; increase REQUESTS or set LOAD_DURATION for volume =="
    run_load_targets || rc=$?
    ;;
esac

if [ -n "$OUTPUT_JSON" ]; then
  # -E for portable alternation (BSD/macOS sed lacks \| in basic regex).
  sed -E -n 's/^(LOAD|CHECK)_RESULT_JSON //p' "$CAPTURE_FILE" > "$OUTPUT_JSON"
  echo "wrote $(wc -l < "$OUTPUT_JSON" | tr -d ' ') machine-readable result(s) (JSONL) to $OUTPUT_JSON" >&2
fi

exit "$rc"
