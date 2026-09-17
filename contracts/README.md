## Running hardhat tests locally
 * start up a local instance of sei: `./scripts/initialize_local_chain.sh`
 * run a hardhat tests:
    * `cd contracts`
    * `npx hardhat test --network seilocal test/EVMCompatabilityTest.js`

## Compile and build contracts with Foundry
 * run: `forge install` and `forge build`
 * This will generate binaries and abis in the `contracts/out/` directory
