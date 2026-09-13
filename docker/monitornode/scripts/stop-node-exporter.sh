#!/usr/bin/env bash
#
# Stops the local node_exporter. The container is stopped but not removed;
# use start-node-exporter.sh to start it again.
#
# Usage: ./stop-node-exporter.sh
#
# Requirements: Docker must be installed and running.

set -euo pipefail

CONTAINER_NAME="sei-node-exporter"

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

if docker ps -q -f "name=^${CONTAINER_NAME}$" | grep -q .; then
	echo "Stopping node_exporter container..."
	docker stop "$CONTAINER_NAME"
	echo "node_exporter stopped."
else
	echo "node_exporter is not running."
fi
