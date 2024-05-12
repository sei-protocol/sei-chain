#!/bin/bash

set -e

cd contracts
npm ci
npx hardhat test --network seilocal test/CW20toERC20PointerTest.js
npx hardhat test --network seilocal test/ERC20toCW20PointerTest.js
npx hardhat test --network seilocal test/CW721toERC721PointerTest.js
npx hardhat test --network seilocal test/ERC721toCW721PointerTest.js
