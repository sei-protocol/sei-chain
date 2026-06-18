const env = (key: string, fallback: string): string => {
    const v = process.env[key];
    return v && v.length > 0 ? v : fallback;
};

export const Endpoints = {
    sei: {
        evmRpc: env('SEI_EVM_RPC', 'http://localhost:8545'),
        evmWs: env('SEI_EVM_WS', 'ws://localhost:8546'),
        cosmosRpc: env('SEI_COSMOS_RPC', 'http://localhost:26657'),
        rest: env('SEI_REST', 'http://localhost:1317'),
    },
    eth: {
        geth: env('RPC_ETH_GETH', 'http://127.0.0.1:9547'),
        fork: env('RPC_ETH_FORK', 'http://127.0.0.1:9546'),
    },
    accountless: env('RPC_ACCOUNTLESS', 'https://evm-rpc.sei-apis.com'),
} as const;

export const AdminMnemonic = env('SEI_ADMIN_MNEMONIC', '');

export const RuntimeStatePath = env(
    'RPC_TESTS_RUNTIME_STATE',
    'runtime/runtime.json',
);
