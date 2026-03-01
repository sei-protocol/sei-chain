#!/usr/bin/env bash
#
# Starts the local Grafana server, preconfigured with Prometheus as a data source.
# Prometheus must be running (see start-prometheus.sh).
#
# Usage: ./start-grafana.sh
#
# Requirements: Docker must be installed and running.
# Compatible with macOS and Linux.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATASOURCE_CONFIG="${SCRIPT_DIR}/grafana.yaml"
CONTAINER_NAME="cryptosim-grafana"
GRAFANA_PORT=3000
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

# Check that datasource config exists
if [[ ! -f "${DATASOURCE_CONFIG}" ]]; then
	echo "Error: Grafana datasource config not found at ${DATASOURCE_CONFIG}" >&2
	exit 1
fi

# If container exists and is running, we're done
if docker ps -q -f "name=^${CONTAINER_NAME}$" | grep -q .; then
	echo "Grafana is already running."
	echo "  UI: http://localhost:${GRAFANA_PORT}"
	echo "  Default login: admin / admin"
	exit 0
fi

# If container exists but is stopped, start it
if docker ps -aq -f "name=^${CONTAINER_NAME}$" | grep -q .; then
	echo "Starting existing Grafana container..."
	docker start "$CONTAINER_NAME"
	echo ""
	echo "Grafana is running."
	echo "  UI: http://localhost:${GRAFANA_PORT}"
	echo "  Default login: admin / admin"
	exit 0
fi

# Container doesn't exist – create and run (first-time setup)
# Grafana needs to reach Prometheus on the host; use host.docker.internal.
case "$(uname -s)" in
	Linux*) ADD_HOST_FLAG="--add-host=host.docker.internal:host-gateway" ;;
	*) ADD_HOST_FLAG="" ;;
esac

echo "Creating and starting Grafana container..."
docker run -d \
	--name "$CONTAINER_NAME" \
	${ADD_HOST_FLAG:+"$ADD_HOST_FLAG"} \
	-p "${GRAFANA_PORT}:3000" \
	-v "${DATASOURCE_CONFIG}:/etc/grafana/provisioning/datasources/grafana.yaml:ro" \
	-e "GF_SECURITY_ADMIN_USER=admin" \
	-e "GF_SECURITY_ADMIN_PASSWORD=admin" \
	-e "GF_USERS_ALLOW_SIGN_UP=false" \
	grafana/grafana:latest

echo ""
echo "Grafana is running."
echo "  UI:           http://localhost:${GRAFANA_PORT}"
echo "  Login:        admin / admin"
echo "  Data source:  Prometheus (provisioned from ${DATASOURCE_CONFIG})"
echo ""
echo "To stop: ./stop-grafana.sh"
echo ""
echo "Note: Start Prometheus first (./start-prometheus.sh) to have data to visualize."
