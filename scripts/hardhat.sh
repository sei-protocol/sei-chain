cd contracts
npm ci
npx hardhat test --network seilocal test/EVMCompatabilityTester.js
npx hardhat test --network seilocal test/EVMPrecompileTester.js
