#!/bin/bash
#
# GIGA Mixed-Mode EVM Integration Tests
#
# This script replaces the default cluster with a mixed-mode cluster where:
#   - Node 0: GIGA_EXECUTOR=true (sequential mode)
#   - Nodes 1-3: Standard V2 executor
#
# If giga produces different results from V2, the giga node will halt with
# a consensus failure (AppHash or LastResultsHash mismatch).
#
# The GitHub workflow starts a default cluster before running matrix scripts.
# This wrapper tears it down and starts a mixed-mode cluster instead.
#

set -e

echo "=== GIGA Mixed-Mode Integration Test ==="
echo "=== Node 0: GIGA_EXECUTOR=true, Nodes 1-3: standard V2 ==="

# Stop the default cluster that the workflow started
echo "Stopping default cluster..."
make docker-cluster-stop || true

# Start mixed-mode cluster (node 0 = giga, nodes 1-3 = V2)
# build-docker-node is a no-op since the image was already built
echo "Starting mixed-mode cluster..."
DOCKER_DETACH=true make docker-cluster-start-giga-mixed

# Wait for all 4 nodes to be ready
echo "Waiting for mixed cluster to be ready..."
timeout=300
elapsed=0
while [ $elapsed -lt $timeout ]; do
    if [ -f "build/generated/launch.complete" ] && [ $(cat build/generated/launch.complete | wc -l) -ge 4 ]; then
        echo "All 4 nodes are ready (took ${elapsed}s)"
        break
    fi
    sleep 5
    elapsed=$((elapsed + 5))
    echo "  Waiting... (${elapsed}s elapsed)"
done
if [ $elapsed -ge $timeout ]; then
    echo "ERROR: Mixed cluster failed to start within ${timeout}s"
    make docker-cluster-stop
    exit 1
fi

echo "Waiting 10s for nodes to stabilize..."
sleep 10

# Run the same giga EVM tests — they hit node 0 (giga) via seilocal RPC
echo "=== Running GIGA EVM Tests against mixed cluster ==="
./integration_test/evm_module/scripts/evm_giga_tests.sh
EXIT_CODE=$?

if [ $EXIT_CODE -ne 0 ]; then
    echo "TEST FAILURE — check if node 0 (giga) halted due to consensus mismatch"
    echo "Logs: build/generated/logs/seid-0.log"
fi

exit $EXIT_CODE
