## Running hardhat tests locally
 * start up a local instance of sei: `./scripts/initialize_local_chain.sh`
 * run a hardhat tests:
    * `cd contracts`
    * `npx hardhat test --network seilocal test/ERC20toCW20PointerTest.js`

## Compile and build contracts with Foundry
 * run: `forge install` and `forge build`
 * This will generate binaries and abis in the `contracts/out/` directory

## Updating Pointer contracts across codebase
 * Follow instructions above to compile and build the contracts
 * copy the binary under the corresponding `bytecode:object` into `x/evm/contracts` `.bin` file
 * restart seid