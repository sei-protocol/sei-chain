/**
 * Standalone Hardhat config used solely to run a local mainnet fork as the
 * Ethereum reference for the new_rpc_tests module. Kept separate from the repo's
 * top-level hardhat.config.ts so spinning up the fork doesn't disturb existing
 * deploy/test flows.
 *
 * Run with:
 *   npm run rpc:fork
 * which expands to:
 *   hardhat --config hardhat/hardhat.config.ts node --port 9546
 *
 * Required env (or use defaults):
 *   ETH_MAINNET_UPSTREAM  - HTTP RPC URL we are forking from (Alchemy / Infura / etc.)
 *   ETH_MAINNET_FORK_BLOCK - optional pinned block, recommended for determinism
 */
import { HardhatUserConfig } from 'hardhat/config';
import '@nomicfoundation/hardhat-toolbox';

const UPSTREAM = process.env.ETH_MAINNET_UPSTREAM;
if (!UPSTREAM) {
    throw new Error(
        'ETH_MAINNET_UPSTREAM is not set. The mainnet fork (npm run rpc:fork) needs an ' +
            'Ethereum mainnet RPC URL to fork from, e.g.\n' +
            '  ETH_MAINNET_UPSTREAM="https://eth-mainnet.g.alchemy.com/v2/<YOUR_KEY>" npm run rpc:fork',
    );
}

const FORK_BLOCK = process.env.ETH_MAINNET_FORK_BLOCK
    ? Number(process.env.ETH_MAINNET_FORK_BLOCK)
    : undefined;

const config: HardhatUserConfig = {
    solidity: '0.8.28',
    networks: {
        hardhat: {
            chainId: 1, // pretend to be mainnet so eth_chainId tests align with the upstream
            forking: {
                url: UPSTREAM,
                blockNumber: FORK_BLOCK,
            },
            // Keep mining instant — RPC tests don't care about block production cadence here.
            mining: { auto: true, interval: 0 },
        },
    },
    paths: {
        // Hardhat resolves these relative to this config file's directory, so keep
        // them simple. They land in tests/new_rpc_tests/hardhat/{.artifacts,.cache,sources}
        // and stay isolated from the repo's top-level hardhat invocation.
        artifacts: '.artifacts',
        cache: '.cache',
        sources: 'sources',
    },
};

export default config;
