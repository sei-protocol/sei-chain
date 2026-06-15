/**
 * Compile-only Hardhat config for the rpc_tests module. Its sole job is to turn
 * the Solidity sources under ./contracts into Hardhat artifacts under ./artifacts,
 * which utils/evmUtils.ts loads at runtime (`npm run compile`).
 *
 * The separate ./hardhat/hardhat.config.ts is used only to spin up the optional
 * mainnet fork reference node (`npm run rpc:fork`); keep the two configs apart.
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
