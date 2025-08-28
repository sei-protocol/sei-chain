# dApp Tests

This directory contains integration tests that simulate simple use cases on the chain by deploying and running common dApp contracts.
The focus here is mainly on testing common interop scenarios (interactions with associated/unassociated accounts, pointer contracts etc.)
In each test scenario, we deploy the dapp contracts, fund wallets, then go through common end to end scenarios.

## Setup
To run the dapp tests, simply run the script at `/integration_test/dapp_tests/dapp_tests.sh <chain>`

3 chain types are supported, `seilocal`, `devnet` (arctic-1) and `testnet` (atlantic-2). The configs for each chain are stored in `./hardhat.config.js`.

If running on `seilocal`, the script assumes that a local instance of the chain is running by running `/scripts/initialize_local_chain.sh`.
A well funded `admin` account must be available on the local keyring.

If running on the live chains, the tests rely on a `deployer` account, which has to have sufficient funds on the chain the test is running on.
The deployer mnemonic must be stored as an environment variable: DAPP_TESTS_MNEMONIC.
On the test pipelines, the account used is:
- Deployer Sei address: `sei1rtpakm7w9egh0n7xngzm6vrln0szv6yeva6hhn`
- Deployer EVM address: `0x4D952b770C3a0B096e739399B40263D0b516d406`

## Tests

### Uniswap (EVM DEX)
This test deploys a small set of UniswapV3 contracts to the EVM and tests swapping and creation of uniswap pools.
- Test that associated accounts are able to swap erc20 tokens
- Test that associated accounts are able to swap native tokens via pointer
- Test that associated accounts are able to swap cw20 tokens via pointer
- Test that unassociated accounts are able to receive erc20 tokens
- Test that unassociated accounts are able to receive native tokens via pointer
- Unassociated EVM accounts are not able to receive cw20 tokens via pointer
- Test that unassociated accounts can still deploy and supply erc20-erc20pointer liquidity pools.

### Steak (CW Liquid Staking)
This test deploys a set of WASM liquid staking contracts, then tests bonding and unbonding.
- Test that associated accounts are able to bond, then unbond tokens.
- Test that unassociated accounts are able to bond, then unbond tokens.

### NFT Marketplace (EVM NFT Marketplace)
This test deploys a simple NFT Marketplace contract, then tests listing and buying NFTs.
- Test that associated accounts are able to list and buy erc721 tokens
- Test that unassociated accounts are able to list and buy erc721 tokens
- Test that associated accounts are able to buy cw721 tokens via pointers
- Unassociated EVM accounts are currently unable to own or receive cw721 tokens via pointers

### To Be Added
The following is a list of testcases/scenarios that we should add to verify completeness
- CosmWasm DEX tests - test that ERC20 tokens are tradeable via pointer contracts.
- CosmWasm NFT Marketplace tests - test that ERC721 tokens are tradeable via pointer contracts.