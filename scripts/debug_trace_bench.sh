#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
debug_trace benchmark/soak runner (minimal path)

Discovers blocks with varying transaction counts, then repeatedly calls
debug_trace and writes per-request CSV metrics.

Requirements:
  - curl
  - jq

Usage:
  ./scripts/debug_trace_bench.sh [options]

Core options:
  --rpc-url URL              JSON-RPC endpoint (default: http://127.0.0.1:8545)
  --bins SPEC                Tx-count bins, comma-separated min-max ranges
                             (default: 1-10,11-50,51-200,201-1000000)
  --samples-per-bin N        Number of distinct blocks to keep per bin (default: 1)
  --scan-back N              Scan latest..latest-N blocks to find bin matches (default: 500)
  --trace-by MODE            number|hash (default: number)
  --tracer NAME              Tracer name (default: callTracer)
  --trace-timeout DUR        Trace timeout string (e.g. 30s). Empty = omit field.

Run length:
  --iterations N             Total trace requests to run (default: 200)
  --duration-sec N           Run for N seconds instead of fixed iterations
  --sleep-ms N               Sleep between requests (default: 0)

Output:
  --output FILE              CSV output path
                             (default: ./debug_trace_bench_<utc timestamp>.csv)
  --scan-only                Only discover/print candidate blocks, do not trace

Other:
  -h, --help                Show this help

Examples:
  # Quick run: 3 bins, 1 block per bin, 90 total trace calls
  ./scripts/debug_trace_bench.sh \
    --bins "1-25,26-100,101-1000000" \
    --samples-per-bin 1 \
    --iterations 90 \
    --output ./trace_quick.csv

  # 30-minute soak with 250ms between calls
  ./scripts/debug_trace_bench.sh \
    --duration-sec 1800 \
    --sleep-ms 250 \
    --output ./trace_soak.csv
EOF
}

require_binary() {
  local bin="$1"
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "missing required binary: $bin" >&2
    exit 1
  fi
}

csv_escape() {
  local s="$1"
  s="${s//\"/\"\"}"
  printf '"%s"' "$s"
}

to_hex() {
  local n="$1"
  printf "0x%x" "$n"
}

sleep_ms() {
  local ms="$1"
  if (( ms <= 0 )); then
    return
  fi
  local sec=$((ms / 1000))
  local rem=$((ms % 1000))
  sleep "$(printf "%d.%03d" "$sec" "$rem")"
}

rpc_payload() {
  local method="$1"
  local params_json="$2"
  jq -cn --arg method "$method" --argjson params "$params_json" \
    '{"jsonrpc":"2.0","id":1,"method":$method,"params":$params}'
}

rpc_call_body() {
  local method="$1"
  local params_json="$2"
  local payload
  payload="$(rpc_payload "$method" "$params_json")"
  curl -sS --max-time "$HTTP_TIMEOUT_SEC" \
    -H "Content-Type: application/json" \
    --data "$payload" \
    "$RPC_URL"
}

rpc_call_timed_to_file() {
  local method="$1"
  local params_json="$2"
  local body_file="$3"
  local payload
  payload="$(rpc_payload "$method" "$params_json")"
  curl -sS --max-time "$HTTP_TIMEOUT_SEC" \
    -o "$body_file" \
    -w "%{http_code} %{time_total} %{size_download}" \
    -H "Content-Type: application/json" \
    --data "$payload" \
    "$RPC_URL"
}

all_bins_filled() {
  local i
  for i in "${!BIN_LABELS[@]}"; do
    if (( BIN_COUNTS[i] < SAMPLES_PER_BIN )); then
      return 1
    fi
  done
  return 0
}

print_candidates() {
  echo "Selected candidate blocks:"
  echo "  bin, height, tx_count, hash"
  local i
  for i in "${!TARGET_HEIGHTS[@]}"; do
    echo "  ${TARGET_LABELS[i]}, ${TARGET_HEIGHTS[i]}, ${TARGET_TX_COUNTS[i]}, ${TARGET_HASHES[i]}"
  done
}

validate_positive_int() {
  local name="$1"
  local val="$2"
  if ! [[ "$val" =~ ^[0-9]+$ ]]; then
    echo "$name must be a non-negative integer, got: $val" >&2
    exit 1
  fi
}

RPC_URL="http://127.0.0.1:8545"
BINS="1-10,11-50,51-200,201-1000000"
SAMPLES_PER_BIN=1
SCAN_BACK=500
TRACE_BY="number"
TRACER="callTracer"
TRACE_TIMEOUT=""
ITERATIONS=200
DURATION_SEC=0
SLEEP_MS=0
HTTP_TIMEOUT_SEC=120
SCAN_ONLY=false
OUTPUT_CSV="./debug_trace_bench_$(date -u +%Y%m%dT%H%M%SZ).csv"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rpc-url)
      RPC_URL="${2:-}"; shift 2 ;;
    --bins)
      BINS="${2:-}"; shift 2 ;;
    --samples-per-bin)
      SAMPLES_PER_BIN="${2:-}"; shift 2 ;;
    --scan-back)
      SCAN_BACK="${2:-}"; shift 2 ;;
    --trace-by)
      TRACE_BY="${2:-}"; shift 2 ;;
    --tracer)
      TRACER="${2:-}"; shift 2 ;;
    --trace-timeout)
      TRACE_TIMEOUT="${2:-}"; shift 2 ;;
    --iterations)
      ITERATIONS="${2:-}"; shift 2 ;;
    --duration-sec)
      DURATION_SEC="${2:-}"; shift 2 ;;
    --sleep-ms)
      SLEEP_MS="${2:-}"; shift 2 ;;
    --output)
      OUTPUT_CSV="${2:-}"; shift 2 ;;
    --scan-only)
      SCAN_ONLY=true; shift 1 ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1 ;;
  esac
done

require_binary curl
require_binary jq

validate_positive_int "samples-per-bin" "$SAMPLES_PER_BIN"
validate_positive_int "scan-back" "$SCAN_BACK"
validate_positive_int "iterations" "$ITERATIONS"
validate_positive_int "duration-sec" "$DURATION_SEC"
validate_positive_int "sleep-ms" "$SLEEP_MS"

if [[ "$TRACE_BY" != "number" && "$TRACE_BY" != "hash" ]]; then
  echo "--trace-by must be 'number' or 'hash', got: $TRACE_BY" >&2
  exit 1
fi

if (( DURATION_SEC == 0 && ITERATIONS == 0 )); then
  echo "either --iterations or --duration-sec must be > 0" >&2
  exit 1
fi

IFS=',' read -r -a RAW_BINS <<< "$BINS"
if (( ${#RAW_BINS[@]} == 0 )); then
  echo "no bins parsed from --bins: $BINS" >&2
  exit 1
fi

BIN_LABELS=()
BIN_MINS=()
BIN_MAXS=()
BIN_COUNTS=()
for raw in "${RAW_BINS[@]}"; do
  spec="${raw//[[:space:]]/}"
  if [[ "$spec" =~ ^([0-9]+)-([0-9]+)$ ]]; then
    mn="${BASH_REMATCH[1]}"
    mx="${BASH_REMATCH[2]}"
    if (( mn > mx )); then
      echo "invalid bin '$spec': min > max" >&2
      exit 1
    fi
    BIN_LABELS+=("$spec")
    BIN_MINS+=("$mn")
    BIN_MAXS+=("$mx")
    BIN_COUNTS+=(0)
  else
    echo "invalid bin '$spec'. Expected format min-max (e.g. 11-50)." >&2
    exit 1
  fi
done

echo "Discovering candidate blocks on $RPC_URL ..." >&2
latest_body="$(rpc_call_body "eth_blockNumber" "[]")"
if jq -e '.error != null' >/dev/null 2>&1 <<<"$latest_body"; then
  echo "eth_blockNumber returned error:" >&2
  echo "$latest_body" >&2
  exit 1
fi

latest_hex="$(jq -r '.result // empty' <<<"$latest_body")"
if [[ -z "$latest_hex" || "$latest_hex" == "null" ]]; then
  echo "eth_blockNumber returned empty result" >&2
  exit 1
fi

latest_dec=$((16#${latest_hex#0x}))
start_dec=$((latest_dec - SCAN_BACK))
if (( start_dec < 0 )); then
  start_dec=0
fi

TARGET_LABELS=()
TARGET_HEIGHTS=()
TARGET_HASHES=()
TARGET_TX_COUNTS=()

for (( h=latest_dec; h>=start_dec; h-- )); do
  if all_bins_filled; then
    break
  fi

  block_hex="$(to_hex "$h")"
  params_json="$(jq -cn --arg block "$block_hex" '[$block, false]')"
  block_body="$(rpc_call_body "eth_getBlockByNumber" "$params_json")"

  if jq -e '.error != null' >/dev/null 2>&1 <<<"$block_body"; then
    continue
  fi

  hash="$(jq -r '.result.hash // empty' <<<"$block_body")"
  tx_count="$(jq -r 'if .result == null then -1 else (.result.transactions | length) end' <<<"$block_body")"

  if [[ -z "$hash" || "$tx_count" == "-1" ]]; then
    continue
  fi

  for i in "${!BIN_LABELS[@]}"; do
    if (( BIN_COUNTS[i] >= SAMPLES_PER_BIN )); then
      continue
    fi
    if (( tx_count >= BIN_MINS[i] && tx_count <= BIN_MAXS[i] )); then
      TARGET_LABELS+=("${BIN_LABELS[i]}")
      TARGET_HEIGHTS+=("$h")
      TARGET_HASHES+=("$hash")
      TARGET_TX_COUNTS+=("$tx_count")
      BIN_COUNTS[i]=$((BIN_COUNTS[i] + 1))
      echo "  selected bin=${BIN_LABELS[i]} height=$h txs=$tx_count hash=$hash" >&2
      break
    fi
  done
done

target_count="${#TARGET_HEIGHTS[@]}"
if (( target_count == 0 )); then
  echo "no candidate blocks found in scanned range" >&2
  exit 1
fi

for i in "${!BIN_LABELS[@]}"; do
  if (( BIN_COUNTS[i] < SAMPLES_PER_BIN )); then
    echo "warning: bin ${BIN_LABELS[i]} only found ${BIN_COUNTS[i]} blocks (wanted $SAMPLES_PER_BIN)" >&2
  fi
done

print_candidates

if [[ "$SCAN_ONLY" == "true" ]]; then
  exit 0
fi

mkdir -p "$(dirname "$OUTPUT_CSV")"
cat > "$OUTPUT_CSV" <<'EOF'
timestamp_utc,request_index,target_bin,block_height,block_hash,tx_count,trace_method,tracer,http_code,latency_sec,response_bytes,success,error_code,error_message
EOF

TRACE_METHOD="debug_traceBlockByNumber"
if [[ "$TRACE_BY" == "hash" ]]; then
  TRACE_METHOD="debug_traceBlockByHash"
fi

if [[ -n "$TRACE_TIMEOUT" ]]; then
  TRACE_CONFIG_JSON="$(jq -cn --arg tracer "$TRACER" --arg timeout "$TRACE_TIMEOUT" '{tracer:$tracer, timeout:$timeout}')"
else
  TRACE_CONFIG_JSON="$(jq -cn --arg tracer "$TRACER" '{tracer:$tracer}')"
fi

echo "Running trace benchmark..." >&2
echo "  method=$TRACE_METHOD tracer=$TRACER trace_by=$TRACE_BY" >&2
echo "  output=$OUTPUT_CSV" >&2

request_idx=0
success_count=0
failure_count=0

end_at=0
if (( DURATION_SEC > 0 )); then
  end_at=$((SECONDS + DURATION_SEC))
fi

while :; do
  if (( DURATION_SEC > 0 )); then
    if (( SECONDS >= end_at )); then
      break
    fi
  else
    if (( request_idx >= ITERATIONS )); then
      break
    fi
  fi

  target_i=$((request_idx % target_count))
  req_num=$((request_idx + 1))

  bin_label="${TARGET_LABELS[target_i]}"
  height_dec="${TARGET_HEIGHTS[target_i]}"
  block_hash="${TARGET_HASHES[target_i]}"
  tx_count="${TARGET_TX_COUNTS[target_i]}"

  block_param="$(to_hex "$height_dec")"
  if [[ "$TRACE_BY" == "hash" ]]; then
    block_param="$block_hash"
  fi

  params_json="$(jq -cn --arg block "$block_param" --argjson cfg "$TRACE_CONFIG_JSON" '[$block, $cfg]')"
  body_file="$(mktemp)"

  set +e
  metrics="$(rpc_call_timed_to_file "$TRACE_METHOD" "$params_json" "$body_file")"
  curl_rc=$?
  set -e

  http_code="000"
  latency="0"
  response_bytes="0"
  success="false"
  error_code=""
  error_message=""

  if (( curl_rc != 0 )); then
    error_message="curl_failed_rc_${curl_rc}"
    failure_count=$((failure_count + 1))
  else
    read -r http_code latency response_bytes <<<"$metrics"

    if ! jq -e . >/dev/null 2>&1 "$body_file"; then
      error_message="invalid_json_response"
      failure_count=$((failure_count + 1))
    elif jq -e '.error != null' >/dev/null 2>&1 "$body_file"; then
      success="false"
      error_code="$(jq -r '.error.code // ""' "$body_file")"
      error_message="$(jq -r '.error.message // ""' "$body_file")"
      failure_count=$((failure_count + 1))
    else
      success="true"
      success_count=$((success_count + 1))
    fi
  fi

  ts="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  {
    csv_escape "$ts"; printf ","
    csv_escape "$req_num"; printf ","
    csv_escape "$bin_label"; printf ","
    csv_escape "$height_dec"; printf ","
    csv_escape "$block_hash"; printf ","
    csv_escape "$tx_count"; printf ","
    csv_escape "$TRACE_METHOD"; printf ","
    csv_escape "$TRACER"; printf ","
    csv_escape "$http_code"; printf ","
    csv_escape "$latency"; printf ","
    csv_escape "$response_bytes"; printf ","
    csv_escape "$success"; printf ","
    csv_escape "$error_code"; printf ","
    csv_escape "$error_message"; printf "\n"
  } >> "$OUTPUT_CSV"

  rm -f "$body_file"

  request_idx=$req_num
  if (( request_idx % 25 == 0 )); then
    echo "  progress: requests=$request_idx success=$success_count failure=$failure_count" >&2
  fi

  sleep_ms "$SLEEP_MS"
done

echo "Done." >&2
echo "  requests=$request_idx success=$success_count failure=$failure_count" >&2
echo "  csv=$OUTPUT_CSV" >&2
