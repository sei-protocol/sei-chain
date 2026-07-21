const env = (key: string, fallback: string): string => {
    const v = process.env[key];
    return v && v.length > 0 ? v : fallback;
};

export const Endpoints = {
    sei: {
        evmRpc: env('SEI_EVM_RPC', 'http://localhost:8545'),
        cosmosRpc: env('SEI_COSMOS_RPC', 'http://localhost:26657'),
        rest: env('SEI_REST', 'http://localhost:1317'),
    },
} as const;

export const AdminMnemonic = env('SEI_ADMIN_MNEMONIC', '');

export const RuntimeStatePath = env(
    'PRECOMPILE_TESTS_RUNTIME_STATE',
    'runtime/runtime.json',
);
