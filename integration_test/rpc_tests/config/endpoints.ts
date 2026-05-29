const env = (key: string, fallback: string): string => {
    const v = process.env[key];
    return v && v.length > 0 ? v : fallback;
};

const envOptional = (key: string): string | undefined => {
    const v = process.env[key];
    return v && v.length > 0 ? v : undefined;
};

export const Endpoints = {
    sei: {
        evmRpc: env('SEI_EVM_RPC', 'http://localhost:8545'),
        cosmosRpc: env('SEI_COSMOS_RPC', 'http://localhost:26657'),
        rest: env('SEI_REST', 'http://localhost:1317'),
    },
    eth: {
        geth: env('RPC_ETH_GETH', 'http://127.0.0.1:9547'),
        fork: env('RPC_ETH_FORK', 'http://127.0.0.1:9546'),
        upstream: env(
            'ETH_MAINNET_UPSTREAM',
            'https://eth-mainnet.g.alchemy.com/v2/Dmh5eMv-DYo4wvFHE2e3E',
        ),
        forkBlock: envOptional('ETH_MAINNET_FORK_BLOCK'),
    },
    accountless: env('RPC_ACCOUNTLESS', 'https://evm-rpc.sei-apis.com'),
} as const;

export const AdminMnemonic = env(
    'SEI_ADMIN_MNEMONIC',
    'cover brand danger absent gas worth sustain rural powder auction shadow find merge domain promote glimpse burger embody favorite lake rain plate present soda',
);

export const RuntimeStatePath = env(
    'RPC_TESTS_RUNTIME_STATE',
    'runtime/runtime.json',
);
