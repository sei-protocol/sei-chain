#!/usr/bin/env bash
#
# Starts the local Prometheus server. If the container already exists, it is
# started; otherwise it is created and run (first-time setup).
#
# Usage: ./start-prometheus.sh
#
# Requirements: Docker must be installed and running.
# Compatible with macOS and Linux.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROMETHEUS_CONFIG="${SCRIPT_DIR}/prometheus.yaml"
CONTAINER_NAME="cryptosim-prometheus"
PROMETHEUS_UI_PORT=9091
CRYPTOSIM_METRICS_PORT=9090

# Check for Docker
if ! command -v docker &>/dev/null; then
	echo "Error: docker is not installed or not in PATH" >&2
	exit 1
fi

# Check that Docker daemon is reachable
if ! docker info &>/dev/null; then
	echo "Error: Docker daemon is not running or not accessible. Start Docker and try again." >&2
	exit 1
fi

# Check that config exists
if [[ ! -f "${PROMETHEUS_CONFIG}" ]]; then
	echo "Error: Prometheus config not found at ${PROMETHEUS_CONFIG}" >&2
	exit 1
fi

# If container exists and is running, we're done
if docker ps -q -f "name=^${CONTAINER_NAME}$" | grep -q .; then
	echo "Prometheus is already running."
	echo "  UI: http://localhost:${PROMETHEUS_UI_PORT}"
	exit 0
fi

# If container exists but is stopped, start it
if docker ps -aq -f "name=^${CONTAINER_NAME}$" | grep -q .; then
	echo "Starting existing Prometheus container..."
	docker start "$CONTAINER_NAME"
	echo ""
	echo "Prometheus is running."
	echo "  UI: http://localhost:${PROMETHEUS_UI_PORT}"
	exit 0
fi

# Container doesn't exist – create and run (first-time setup)
# host.docker.internal works on Docker Desktop (macOS/Windows). On Linux we add it via --add-host.
case "$(uname -s)" in
	Linux*) ADD_HOST_FLAG="--add-host=host.docker.internal:host-gateway" ;;
	*) ADD_HOST_FLAG="" ;;
esac

echo "Creating and starting Prometheus container..."
docker run -d \
	--name "$CONTAINER_NAME" \
	${ADD_HOST_FLAG:+"$ADD_HOST_FLAG"} \
	-p "${PROMETHEUS_UI_PORT}:9090" \
	-v "${PROMETHEUS_CONFIG}:/etc/prometheus/prometheus.yml:ro" \
	prom/prometheus:latest \
	--config.file=/etc/prometheus/prometheus.yml \
	--storage.tsdb.path=/prometheus \
	--web.enable-lifecycle

echo ""
echo "Prometheus is running."
echo "  UI:        http://localhost:${PROMETHEUS_UI_PORT}"
echo "  Config:    ${PROMETHEUS_CONFIG}"
echo ""
echo "To stop: ./stop-prometheus.sh"
