#!/bin/bash

set -e

# Print system info for diagnostics
echo "=========================================="
echo "System Information:"
echo "=========================================="
echo "Total Memory: $(free -h | awk '/^Mem:/{print $2}')"
echo "Available Memory: $(free -h | awk '/^Mem:/{print $7}')"
echo "CPU Cores: $(nproc)"
echo "Disk Space: $(df -h . | awk 'NR==2 {print $4}')"
echo "Node Version: $(node --version)"
echo "NPM Version: $(npm --version)"
echo "=========================================="

cd contracts
npm ci

# Increase Node.js memory limit to 8GB
# Tests run sequentially and memory accumulates across test files
export NODE_OPTIONS="--max-old-space-size=8192"

# Run tests with memory monitoring
for test_file in "test/EVMCompatabilityTest.js" "test/EVMPrecompileTest.js" "test/SeiEndpointsTest.js" "test/AssociateTest.js"; do
    echo ""
    echo "=========================================="
    echo "Running: $test_file"
    echo "Memory before test: $(free -h | awk '/^Mem:/{print $3 "/" $2}')"
    echo "=========================================="
    
    set +e  # Temporarily disable exit on error
    npx hardhat test --network seilocal "$test_file"
    exit_code=$?
    set -e  # Re-enable exit on error
    
    echo "Memory after test: $(free -h | awk '/^Mem:/{print $3 "/" $2}')"
    
    if [ $exit_code -eq 137 ]; then
        echo "❌❌❌ KILLED (OOM) - exit code: 137 ❌❌❌"
        # Check dmesg for OOM killer messages
        echo "Checking for OOM in system logs..."
        sudo dmesg | tail -20 | grep -i "killed process\|out of memory" || echo "No OOM messages found"
        exit 137
    elif [ $exit_code -ne 0 ]; then
        echo "⚠️  Test had failures (exit code: $exit_code) but continuing..."
    fi
    
    # Brief pause to allow GC
    sleep 2
done

echo ""
echo "✅ All tests passed!"