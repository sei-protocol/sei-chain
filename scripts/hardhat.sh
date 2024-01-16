node --version
npx --version
cd contracts
npm ci
npx hardhat test --network seilocal scripts/EVMCompatabilityTester.js
