## Compile and build contracts
 * This is a foundry project so just run: `forge install` and `forge build`
 * This will generate binaries and abis in the `out/` directory

## Updating Pointer contracts across codebase
 * Follow instructions above to compile and build the contracts
 * copy the binary under the corresponding `bytecode:object` into `x/evm/contracts` `.bin` file
 * restart seid