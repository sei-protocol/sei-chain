/**
 * Which chain the suite talks to, and the guard that it is the intended one.
 *
 * This suite runs against long-lived shared networks, and its two phases run
 * hours or days apart. Recording against one network and verifying against
 * another would produce a confident, meaningless diff, so the chain identifier
 * is checked against the target table before anything else runs.
 */

export type TargetName = 'local' | 'arctic-1' | 'atlantic-2';

export interface Target {
    name: TargetName;
    evmChainId: bigint;
    cosmosChainId: string;
    evmRpcUrl: string;
    /**
     * Cosmos REST (LCD). The upgrade module's applied plan and module version
     * map are read from here rather than from the upgrade precompile: the
     * precompile is not registered on every released binary (arctic-1 on v6.6.1
     * answers 0x at 0x…1015), and the one fact the verify phase cannot work
     * without is whether the upgrade ran.
     */
    restUrl: string;
}

/**
 * Endpoints match integration_test/dapp_tests/hardhat.config.js (the public
 * sei-apis.com hosts) rather than the platform wave nodes the load generator
 * uses, so the suite works without access to internal infrastructure. Override
 * with UPGRADE_TEST_EVM_RPC / UPGRADE_TEST_REST to point at a wave node or a
 * private archive.
 */
const TARGETS: Record<TargetName, Omit<Target, 'name'>> = {
    local: {
        evmChainId: 713714n,
        cosmosChainId: 'sei',
        evmRpcUrl: 'http://localhost:8545',
        restUrl: 'http://localhost:1317',
    },
    'arctic-1': {
        evmChainId: 713715n,
        cosmosChainId: 'arctic-1',
        evmRpcUrl: 'https://evm-rpc-arctic-1.sei-apis.com',
        restUrl: 'https://rest-arctic-1.sei-apis.com',
    },
    'atlantic-2': {
        evmChainId: 1328n,
        cosmosChainId: 'atlantic-2',
        evmRpcUrl: 'https://evm-rpc-testnet.sei-apis.com',
        restUrl: 'https://rest-testnet.sei-apis.com',
    },
};

function isTargetName(value: string): value is TargetName {
    return value in TARGETS;
}

export function resolveTarget(env: NodeJS.ProcessEnv = process.env): Target {
    const name = env.UPGRADE_TEST_NETWORK ?? 'arctic-1';
    if (!isTargetName(name)) {
        throw new Error(
            `UPGRADE_TEST_NETWORK=${name} is not one of ${Object.keys(TARGETS).join(', ')}`,
        );
    }
    const target = TARGETS[name];
    return {
        name,
        ...target,
        evmRpcUrl: env.UPGRADE_TEST_EVM_RPC ?? target.evmRpcUrl,
        restUrl: env.UPGRADE_TEST_REST ?? target.restUrl,
    };
}

/**
 * The upgrade whose effects this suite records and verifies. The name has to
 * match the governance plan name exactly, because that string is the key
 * appliedPlan() is stored under.
 */
export function upgradeName(env: NodeJS.ProcessEnv = process.env): string {
    return env.UPGRADE_TEST_PLAN_NAME ?? 'v6.7';
}

/** The mnemonic used for the transaction probes, or '' when none was given. */
export function adminMnemonic(env: NodeJS.ProcessEnv = process.env): string {
    return env.UPGRADE_TEST_MNEMONIC ?? env.DAPP_TESTS_MNEMONIC ?? '';
}

/**
 * The modules the upgrade removes from the module version map. Anything else
 * disappearing is a regression, and a name here that survives means the handler
 * is missing a DeleteModuleVersion call for it.
 */
export function expectedRemovedModules(env: NodeJS.ProcessEnv = process.env): string[] {
    const raw = env.UPGRADE_TEST_REMOVED_MODULES;
    const names = raw ? raw.split(/[\s,]+/).filter(Boolean) : ['capability', 'feegrant', 'ibc', 'transfer'];
    return [...names].sort();
}
