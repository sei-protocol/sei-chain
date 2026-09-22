#!/usr/bin/env bash
#
# Starts node_exporter so Prometheus can scrape host CPU, memory, disk, and
# filesystem metrics. Must run on Linux: the container shares the host's PID
# and network namespaces and mounts the host root, so the numbers are the
# machine's, not Docker's.
#
# Usage: ./start-node-exporter.sh
#
# Requirements: Docker must be installed and running. Linux only.
# Prometheus scrapes this at host.docker.internal:9100 (job "node").

set -euo pipefail

CONTAINER_NAME="sei-node-exporter"
NODE_EXPORTER_PORT=9100
PROMETHEUS_UI_PORT=9091

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

# Docker Desktop on macOS/Windows would report the VM, not the host running the bench.
if [[ "$(uname -s)" != "Linux" ]]; then
	echo "Error: node_exporter in Docker only reports the host on Linux." >&2
	echo "On macOS/Windows it would scrape Docker's VM, not the machine running the bench." >&2
	exit 1
fi

# If container exists and is running, we're done
if docker ps -q -f "name=^${CONTAINER_NAME}$" | grep -q .; then
	echo "node_exporter is already running."
	echo "  Metrics: http://localhost:${NODE_EXPORTER_PORT}/metrics"
	exit 0
fi

# If container exists but is stopped, start it
if docker ps -aq -f "name=^${CONTAINER_NAME}$" | grep -q .; then
	echo "Starting existing node_exporter container..."
	docker start "$CONTAINER_NAME"
	echo ""
	echo "node_exporter is running."
	echo "  Metrics: http://localhost:${NODE_EXPORTER_PORT}/metrics"
	exit 0
fi

# --net=host / --pid=host: collect the host's network and process view.
# /:/host + --path.rootfs=/host: CPU, disk, and filesystems from the host, not the container.
echo "Creating and starting node_exporter container..."
docker run -d \
	--name "$CONTAINER_NAME" \
	--net=host \
	--pid=host \
	-v "/:/host:ro,rslave" \
	prom/node-exporter:latest \
	--path.rootfs=/host

echo ""
echo "node_exporter is running."
echo "  Metrics: http://localhost:${NODE_EXPORTER_PORT}/metrics"
echo "  Prometheus job: node (host.docker.internal:${NODE_EXPORTER_PORT})"
echo ""
echo "To stop: ./stop-node-exporter.sh"
echo ""
echo "Grafana: import dashboard 1860 (Node Exporter Full), or query node_* in Explore."

# Config is bind-mounted; reload so a running Prometheus picks up the node job.
if curl -sf -X POST "http://localhost:${PROMETHEUS_UI_PORT}/-/reload" >/dev/null 2>&1; then
	echo "Prometheus reloaded."
fi
