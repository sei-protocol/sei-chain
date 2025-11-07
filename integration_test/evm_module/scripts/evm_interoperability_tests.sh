#!/bin/bash

set -e

# Print system diagnostics
echo "=========================================="
echo "EVM Interoperability Tests - System Info"
echo "=========================================="
echo "Total Memory: $(free -h 2>/dev/null | awk '/^Mem:/{print $2}' || echo 'N/A (macOS)')"
echo "Available Memory: $(free -h 2>/dev/null | awk '/^Mem:/{print $7}' || echo 'N/A (macOS)')"
echo "CPU Cores: $(nproc 2>/dev/null || sysctl -n hw.ncpu)"
echo "Node Version: $(node --version)"
echo "=========================================="

cd contracts
npm ci

# Increase Node.js memory limit to 8GB to prevent OOM
export NODE_OPTIONS="--max-old-space-size=8192"

echo ""
echo "Running: CW20 to ERC20 Pointer Test"
npx hardhat test --network seilocal test/CW20toERC20PointerTest.js
npx hardhat test --network seilocal test/ERC20toCW20PointerTest.js
npx hardhat test --network seilocal test/ERC20toNativePointerTest.js
npx hardhat test --network seilocal test/CW721toERC721PointerTest.js
npx hardhat test --network seilocal test/ERC721toCW721PointerTest.js
npx hardhat test --network seilocal test/CW1155toERC1155PointerTest.js
npx hardhat test --network seilocal test/ERC1155toCW1155PointerTest.js
npx hardhat test --network seilocal test/SeiSoloTest.js
npx hardhat test --network seilocal test/SetCodeTxTest.js
npx hardhat test --network seilocal test/TransientStorageTest.js
