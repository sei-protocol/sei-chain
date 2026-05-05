#!/usr/bin/env bash
# Provisions the historical-offload consumer against a pre-existing MSK cluster
# and CockroachDB cluster. The cloud-side resources (MSK cluster, topic, IAM
# role, CockroachDB cluster + database + user) must already exist.
#
# Required env:
#   KAFKA_BROKERS          comma-separated broker endpoints (e.g. b-1.x.kafka.amazonaws.com:9098,b-2.x...)
#   KAFKA_TOPIC            topic cryptosim is publishing to
#   KAFKA_GROUP_ID         consumer group id
#   AWS_REGION             region for AWS MSK IAM signing (also exported for the binary at runtime)
#   COCKROACH_DSN          full postgresql:// DSN (include sslmode, statement_timeout, etc.)
#
# Optional env:
#   KAFKA_TLS_ENABLED      default true
#   KAFKA_SASL_MECHANISM   default aws-msk-iam ("" or "none" disables)
#   KAFKA_START_OFFSET     default first (first|last)
#   COCKROACH_MAX_CONNS    default 16
#   WORKERS                default 1 (per-partition parallelism)
#   CONFIG_OUT             default ./historical-offload-consumer.json
#   BIN_OUT                default ./bin/historical-offload-consumer
#   SKIP_SCHEMA=1          skip applying schema.sql
#   SKIP_BUILD=1           skip go build
#   RUN=1                  exec the binary at the end

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../../../../.." &>/dev/null && pwd)"

: "${KAFKA_BROKERS:?set KAFKA_BROKERS}"
: "${KAFKA_TOPIC:?set KAFKA_TOPIC}"
: "${KAFKA_GROUP_ID:?set KAFKA_GROUP_ID}"
: "${AWS_REGION:?set AWS_REGION}"
: "${COCKROACH_DSN:?set COCKROACH_DSN}"

KAFKA_TLS_ENABLED="${KAFKA_TLS_ENABLED:-true}"
KAFKA_SASL_MECHANISM="${KAFKA_SASL_MECHANISM:-aws-msk-iam}"
KAFKA_START_OFFSET="${KAFKA_START_OFFSET:-first}"
COCKROACH_MAX_CONNS="${COCKROACH_MAX_CONNS:-16}"
WORKERS="${WORKERS:-1}"
CONFIG_OUT="${CONFIG_OUT:-./historical-offload-consumer.json}"
BIN_OUT="${BIN_OUT:-./bin/historical-offload-consumer}"

log() { printf '[%s] %s\n' "$(date -u +%FT%TZ)" "$*"; }

apply_schema() {
    local schema="${SCRIPT_DIR}/schema/schema.sql"
    [[ -f "$schema" ]] || { echo "schema file missing: $schema" >&2; exit 1; }

    if command -v cockroach &>/dev/null; then
        log "applying schema with cockroach sql"
        cockroach sql --url="${COCKROACH_DSN}" <"$schema"
    elif command -v psql &>/dev/null; then
        log "applying schema with psql"
        psql "${COCKROACH_DSN}" -v ON_ERROR_STOP=1 -f "$schema"
    else
        echo "need 'cockroach' or 'psql' on PATH to apply schema; set SKIP_SCHEMA=1 to bypass" >&2
        exit 1
    fi
}

write_config() {
    log "writing config to ${CONFIG_OUT}"
    mkdir -p "$(dirname "${CONFIG_OUT}")"

    python3 - "$CONFIG_OUT" <<PY
import json, os, sys
brokers = [b.strip() for b in os.environ["KAFKA_BROKERS"].split(",") if b.strip()]
cfg = {
    "Kafka": {
        "Brokers": brokers,
        "Topic": os.environ["KAFKA_TOPIC"],
        "GroupID": os.environ["KAFKA_GROUP_ID"],
        "Region": os.environ["AWS_REGION"],
        "TLSEnabled": os.environ["KAFKA_TLS_ENABLED"].lower() == "true",
        "SASLMechanism": os.environ["KAFKA_SASL_MECHANISM"],
        "StartOffset": os.environ["KAFKA_START_OFFSET"],
    },
    "Cockroach": {
        "DSN": os.environ["COCKROACH_DSN"],
        "MaxOpenConns": int(os.environ["COCKROACH_MAX_CONNS"]),
    },
    "Workers": int(os.environ["WORKERS"]),
}
with open(sys.argv[1], "w") as f:
    json.dump(cfg, f, indent=2)
    f.write("\n")
PY
    chmod 600 "${CONFIG_OUT}"
}

build_binary() {
    log "building ${BIN_OUT}"
    mkdir -p "$(dirname "${BIN_OUT}")"
    (cd "${REPO_ROOT}" && \
        go build -o "${BIN_OUT}" ./sei-db/state_db/ss/offload/consumer/cmd/historical-offload-consumer)
}

[[ "${SKIP_SCHEMA:-0}" == "1" ]] || apply_schema
write_config
[[ "${SKIP_BUILD:-0}" == "1" ]] || build_binary

log "ready. config=${CONFIG_OUT} bin=${BIN_OUT}"

if [[ "${RUN:-0}" == "1" ]]; then
    log "starting consumer"
    exec "${BIN_OUT}" "${CONFIG_OUT}"
fi
