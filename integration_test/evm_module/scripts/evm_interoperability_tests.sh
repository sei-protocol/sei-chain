#!/bin/bash

set -e

cd contracts
npm ci
npx hardhat test --network seilocal test/CW20toERC20PointerTest.js
npx hardhat test --network seilocal test/ERC20toCW20PointerTest.js
npx hardhat test --network seilocal test/ERC20toNativePointerTest.js
npx hardhat test --network seilocal test/CW721toERC721PointerTest.js
npx hardhat test --network seilocal test/ERC721toCW721PointerTest.js
npx hardhat test --network seilocal test/CW1155toERC1155PointerTest.js
npx hardhat test --network seilocal test/ERC1155toCW1155PointerTest.js
npx hardhat test --network seilocal test/SeiSoloTest.js
