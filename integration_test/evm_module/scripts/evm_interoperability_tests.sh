#!/bin/bash

set -e

cd contracts
npm ci

# Increase Node.js memory limit to 8GB to prevent OOM
export NODE_OPTIONS="--max-old-space-size=8192"

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
