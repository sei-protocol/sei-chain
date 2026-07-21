/**
 * Compile-only Hardhat config for the precompile_tests module. Its sole job is to
 * turn the Solidity fixtures under ./contracts into Hardhat artifacts under
 * ./artifacts, which utils/evmUtils.ts loads at runtime (`npm run compile`).
 */
import { HardhatUserConfig } from 'hardhat/config';
import '@nomicfoundation/hardhat-toolbox';

const config: HardhatUserConfig = {
    solidity: {
        version: '0.8.28',
        settings: {
            optimizer: { enabled: true, runs: 200 },
        },
    },
    paths: {
        sources: 'contracts',
        artifacts: 'artifacts',
        cache: 'cache',
    },
};

export default config;
