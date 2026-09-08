export const REPLAY_SCHEMA_VERSION = 1;
export const PACIFIC_EVM_CHAIN_ID = 1329;
export const PACIFIC_COSMOS_CHAIN_ID = 'pacific-1';
export const EVM_MESSAGE_TYPE = '/seiprotocol.seichain.evm.MsgEVMTransaction';
export const REPLAY_DEPLOYMENT_SCHEMA_VERSION = 5 as const;
export const REPLAY_CONTRACT_KEYS = [
    'weth',
    'factory',
    'router',
    'tokenA',
    'tokenB',
    'pair',
    'nft',
    'erc1155',
    'profileHarness',
    'callGraphHarness',
    'callGraphNode',
    'syntheticCreationHarness',
    'proxyErc20Implementation',
    'dexOutputTokenProxy',
    'v3Pool',
    'v3Router',
    'farmRewardTokenProxy',
    'masterChef',
    'lendingReceiptTokenProxy',
    'lendingOracle',
    'lendingRateModel',
    'lendingComptroller',
    'lendingImplementation',
    'lendingPoolProxy',
    'liquidStakingReceiptTokenProxy',
    'exchangeRateOracle',
    'liquidStakingImplementation',
    'liquidStakingProxy',
    'strategyModule',
    'strategyAdapter',
    'strategyVaultImplementation',
    'strategyVaultProxy',
] as const;

export type ReplayTargetNetwork = string;
export type ReplayTransactionKind = 'transfer' | 'contractCall' | 'contractCreation';

export interface ReplayCallFrame {
    index: number;
    parent: number | null;
    depth: number;
    type: 'CALL' | 'STATICCALL' | 'DELEGATECALL' | 'CREATE' | 'CREATE2';
    selector: string | null;
    inputBytes: number;
    valueNonZero: boolean;
    gas?: string;
    gasUsed?: string;
    error?: string;
    reverted: boolean;
    childrenTruncated?: boolean;
}

export interface ReplayOperationSummary {
    steps: number;
    sload: number;
    sstore: number;
    call: number;
    staticcall: number;
    delegatecall: number;
    create: number;
    create2: number;
    logs: number;
    log0: number;
    log1: number;
    log2: number;
    log3: number;
    log4: number;
    keccak256: number;
}

export interface ReplayStateDiffSummary {
    changedAccounts: number;
    changedStorageSlots: number;
    code: number;
    balance: number;
    nonce: number;
}

export interface ReplayTraceSummary {
    requestedMode: 'calls' | 'full';
    availability: 'available' | 'partial' | 'error';
    calls?: {
        frames: ReplayCallFrame[];
        truncated: boolean;
        sourceFrameCount: number;
    };
    operations?: ReplayOperationSummary;
    stateDiff?: ReplayStateDiffSummary;
    errors?: string[];
}

export interface ReplayEvmReceipt {
    transactionHash: string;
    gasUsed: string;
    effectiveGasPrice?: string;
    status?: string;
    contractAddress?: string | null;
    logs: unknown[];
}

export interface ReplayEvmTransaction {
    hash: string;
    blockNumber: number;
    transactionIndex: number;
    from: string;
    to: string | null;
    nonce: number;
    chainId: string;
    type: number;
    kind: ReplayTransactionKind;
    input: string;
    inputBytes: number;
    selector: string | null;
    value: string;
    gasLimit: string;
    gasPrice?: string;
    maxFeePerGas?: string;
    maxPriorityFeePerGas?: string;
    accessList?: unknown[];
    sourceSerializedBytes?: number;
    recipientCodeHash?: string;
    deployedRuntimeCodeBytes?: number;
    deployedRuntimeCodeHash?: string;
    creationMethod?: 'CREATE' | 'CREATE2';
    trace?: ReplayTraceSummary;
    receipt: ReplayEvmReceipt;
}

export interface ReplayCosmosMessage {
    typeUrl: string;
    valueBase64: string;
}

export interface ReplayCosmosTransaction {
    index: number;
    hash: string;
    rawBase64: string;
    transactionBytes: number;
    memo: string;
    messages: ReplayCosmosMessage[];
    fee: Record<string, string>;
    gasLimit: string;
    result: {
        code: number;
        gasWanted: string;
        gasUsed: string;
        eventCount: number;
    };
    isEvm: boolean;
    evmLink?: {
        hash?: string;
        typeUrl?: string;
        method: 'signed_payload' | 'unresolved';
        reason?: string;
    };
    evm?: ReplayEvmTransaction;
}

export interface ReplayBlock {
    number: number;
    hash: string;
    parentHash: string;
    cosmosHash: string;
    cosmosParentHash: string;
    timestamp: number;
    gasLimit: string;
    gasUsed: string;
    baseFeePerGas?: string;
    transactions: ReplayCosmosTransaction[];
    unlinkedEvmTransactionHashes?: string[];
    /** Additive schema-v1 field. Ordering against Cosmos-only transactions is approximate. */
    unlinkedEvmTransactions?: ReplayEvmTransaction[];
}

export interface ReplaySegment {
    schemaVersion: typeof REPLAY_SCHEMA_VERSION;
    capturedAt: string;
    source: {
        network: 'pacific-1';
        evmChainId: typeof PACIFIC_EVM_CHAIN_ID;
        cosmosChainId: typeof PACIFIC_COSMOS_CHAIN_ID;
        evmRpcUrl: string;
        cosmosRpcUrl: string;
        firstBlock: number;
        lastBlock: number;
        blockCount: number;
        startTimestamp: number;
        endTimestamp: number;
        durationSeconds: number;
        tipLag: number;
    };
    continuity: {
        firstParentHash: string;
        lastBlockHash: string;
        firstCosmosParentHash: string;
        lastCosmosBlockHash: string;
    };
    totals: {
        canonicalTransactions: number;
        evmTransactions: number;
        cosmosOnlyTransactions: number;
        linkedEvmTransactions?: number;
        unresolvedEvmWrappers?: number;
        unlinkedEvmTransactions?: number;
        sourceBytes: number;
    };
    blocks: ReplayBlock[];
}

export interface ReplayCheckpoint {
    schemaVersion: 1;
    sourceNetwork: 'pacific-1';
    nextCollectHeight: number;
    lastCollectedHeight: number;
    lastCollectedEvmHash: string;
    lastCollectedCosmosHash: string;
    updatedAt: string;
}

export interface ReplayUserManifest {
    schemaVersion: 1;
    network: ReplayTargetNetwork;
    chainId: number;
    users: Array<{
        index: number;
        derivationPath: string;
        seiAddress: string;
        evmAddress: string;
        balanceUsei?: string;
    }>;
}

export interface ReplayDeploymentManifest {
    schemaVersion: typeof REPLAY_DEPLOYMENT_SCHEMA_VERSION;
    network: ReplayTargetNetwork;
    chainId: number;
    sushiV2: {
        repository: string;
        commit: string;
        pairInitCodeHash: string;
        factoryCreationCodeHash: string;
        routerCreationCodeHash: string;
    };
    codeHashes?: Record<string, string>;
    contracts: {
        weth?: string;
        factory?: string;
        router?: string;
        tokenA?: string;
        tokenB?: string;
        pair?: string;
        nft?: string;
        erc1155?: string;
        profileHarness?: string;
        callGraphHarness?: string;
        callGraphNode?: string;
        syntheticCreationHarness?: string;
        proxyErc20Implementation?: string;
        dexOutputTokenProxy?: string;
        v3Pool?: string;
        v3Router?: string;
        farmRewardTokenProxy?: string;
        masterChef?: string;
        lendingReceiptTokenProxy?: string;
        lendingOracle?: string;
        lendingRateModel?: string;
        lendingComptroller?: string;
        lendingImplementation?: string;
        lendingPoolProxy?: string;
        liquidStakingReceiptTokenProxy?: string;
        exchangeRateOracle?: string;
        liquidStakingImplementation?: string;
        liquidStakingProxy?: string;
        strategyModule?: string;
        strategyAdapter?: string;
        strategyVaultImplementation?: string;
        strategyVaultProxy?: string;
        [name: string]: string | undefined;
    };
}
