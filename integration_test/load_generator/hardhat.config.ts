import { HardhatUserConfig, subtask } from 'hardhat/config';
import { TASK_COMPILE_SOLIDITY_GET_SOLC_BUILD } from 'hardhat/builtin-tasks/task-names';
import '@nomicfoundation/hardhat-toolbox';

const SUSHI_SOLC_VERSION = '0.6.12';
const SUSHI_SOLC_LONG_VERSION = '0.6.12+commit.27d51765';

subtask(TASK_COMPILE_SOLIDITY_GET_SOLC_BUILD).setAction(
    async ({ solcVersion }: { solcVersion: string }, _hre, runSuper) => {
        if (solcVersion !== SUSHI_SOLC_VERSION) return runSuper();
        return {
            compilerPath: require.resolve('solc/soljson.js'),
            isSolcJs: true,
            version: SUSHI_SOLC_VERSION,
            longVersion: SUSHI_SOLC_LONG_VERSION,
        };
    },
);

const config: HardhatUserConfig = {
    solidity: {
        compilers: [
            {
                version: '0.8.28',
                settings: {
                    optimizer: { enabled: true, runs: 200 },
                },
            },
            {
                version: SUSHI_SOLC_VERSION,
                settings: {
                    optimizer: { enabled: true, runs: 200 },
                    evmVersion: 'istanbul',
                    metadata: { bytecodeHash: 'ipfs' },
                },
            },
        ],
    },
    paths: {
        sources: 'contracts',
        artifacts: 'artifacts',
        cache: 'cache',
    },
};

export default config;
