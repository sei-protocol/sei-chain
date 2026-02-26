#!/usr/bin/env bash
#
# Stops the local Grafana server. The container is stopped but not removed;
# use start-grafana.sh to start it again.
#
# Usage: ./stop-grafana.sh
#
# Requirements: Docker must be installed and running.
# Compatible with macOS and Linux.

set -euo pipefail

CONTAINER_NAME="cryptosim-grafana"

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
	echo "Stopping Grafana container..."
	docker stop "$CONTAINER_NAME"
	echo "Grafana stopped."
else
	echo "Grafana is not running."
fi
