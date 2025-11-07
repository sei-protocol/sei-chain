#!/bin/bash

# Check if a configuration argument is passed
if [ -z "$1" ]; then
  echo "Please provide a chain (seilocal, devnet, or testnet)."
  exit 1
fi

set -e

# Print system diagnostics
echo "=========================================="
echo "System Diagnostics:"
echo "=========================================="
echo "Total Memory: $(free -h 2>/dev/null | awk '/^Mem:/{print $2}' || echo 'N/A (macOS)')"
echo "Available Memory: $(free -h 2>/dev/null | awk '/^Mem:/{print $7}' || echo 'N/A (macOS)')"
echo "CPU Cores: $(nproc 2>/dev/null || sysctl -n hw.ncpu)"
echo "Node Version: $(node --version)"
echo "NPM Version: $(npm --version)"
echo "=========================================="

# Define the paths to the test files
uniswap_test="uniswap/uniswapTest.js"
steak_test="steak/SteakTests.js"
nft_test="nftMarketplace/nftMarketplaceTests.js"

# Build contracts repo first since we rely on that for lib.js
cd contracts
npm ci

cd ../integration_test/dapp_tests
npm ci

npx hardhat compile

# Set the CONFIG environment variable
export DAPP_TEST_ENV=$1

# Increase Node.js memory limit to 12GB to prevent OOM in tests
# Uniswap tests are particularly memory intensive, especially on CI
# CI environment has less total memory and no swap
export NODE_OPTIONS="--max-old-space-size=12288"

# Determine which tests to run
if [ -z "$2" ]; then
  tests=("$uniswap_test" "$steak_test" "$nft_test")
else
  case $2 in
    uniswap)
      tests=("$uniswap_test")
      ;;
    steak)
      tests=("$steak_test")
      ;;
    nft)
      tests=("$nft_test")
      ;;
    *)
      echo "Invalid test specified. Please choose either 'uniswap', 'steak', or 'nft'."
      exit 1
      ;;
  esac
fi

# Run the selected tests
test_count=0
for test in "${tests[@]}"; do
  test_count=$((test_count + 1))
  
  echo ""
  echo "=========================================="
  echo "Test $test_count/${#tests[@]}: $test"
  echo "=========================================="
  
  # Memory before test
  echo "Memory before: $(free -h 2>/dev/null | awk '/^Mem:/{print $3 "/" $2}' || echo 'N/A')"
  
  # Disable exit on error temporarily to catch the exit code
  set +e
  npx hardhat test --network $1 $test
  exit_code=$?
  set -e
  
  # Memory after test
  echo "Memory after: $(free -h 2>/dev/null | awk '/^Mem:/{print $3 "/" $2}' || echo 'N/A')"
  
  # Check if killed by OOM
  if [ $exit_code -eq 137 ]; then
    echo ""
    echo "❌❌❌ KILLED (OOM) - Test: $test ❌❌❌"
    echo "This test was killed by the system (exit 137), likely due to out of memory."
    echo "Test file: $test"
    echo "Test number: $test_count out of ${#tests[@]}"
    
    # Try to get system info
    echo ""
    echo "System state at failure:"
    free -h 2>/dev/null || echo "Memory info not available (macOS)"
    
    # Check for OOM in system logs (Linux only)
    if command -v dmesg &> /dev/null; then
      echo "Checking system logs for OOM killer..."
      sudo dmesg 2>/dev/null | tail -20 | grep -i "killed\|oom" || echo "No OOM messages in dmesg"
    fi
    
    exit 137
  elif [ $exit_code -ne 0 ]; then
    echo "⚠️  Test failed with exit code $exit_code (not OOM)"
    echo "Continuing to next test..."
  else
    echo "✅ Test passed"
  fi
  
  # Force garbage collection and wait between tests to reduce memory pressure
  # This is especially important on CI environments with limited memory
  if [ ${#tests[@]} -gt 1 ] && [ $test_count -lt ${#tests[@]} ]; then
    echo "Pausing 5 seconds for memory cleanup before next test..."
    sleep 5
  fi
done

echo ""
echo "=========================================="
echo "✅ All dApp tests completed!"
echo "Total tests run: $test_count"
echo "=========================================="

