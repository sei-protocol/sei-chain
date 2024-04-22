cd contracts
npm ci
npx hardhat test --network seilocal test/EVMCompatabilityTest.js
npx hardhat test --network seilocal test/EVMPrecompileTest.js
