import 'dotenv/config';
import path from 'node:path';
import { ReplayTargetNetwork } from './replay/replayTypes';

export type Environment = NodeJS.ProcessEnv;

const PACIFIC_EVM_RPC = 'https://node-wave-0-evm.pacific-1.platform.sei.io/';
const PACIFIC_COSMOS_RPC = 'https://node-wave-0-rpc.pacific-1.platform.sei.io/';
const DEFAULT_REPLAY_DIR = 'runtime/replay/pacific-1/pacific-1-20m';

const TARGETS = {
    'arctic-1': {
        evmChainId: 713715n,
        cosmosChainId: 'arctic-1',
        evmRpcUrl: 'https://node-wave-0-evm.arctic-1.platform.sei.io/',
        cosmosRpcUrl: 'https://node-wave-0-rpc.arctic-1.platform.sei.io/',
    },
    'atlantic-2': {
        evmChainId: 1328n,
        cosmosChainId: 'atlantic-2',
        evmRpcUrl: 'https://node-wave-0-evm.atlantic-2.platform.sei.io/',
        cosmosRpcUrl: 'https://node-wave-0-rpc.atlantic-2.platform.sei.io/',
    },
} as const;

export interface TargetConfig {
    network: ReplayTargetNetwork;
    evmChainId: bigint;
    cosmosChainId: string;
    evmRpcUrl: string;
    cosmosRpcUrl: string;
    usersPath: string;
    deploymentPath: string;
    mnemonic: string;
}

export function loadTargetConfig(env: Environment = process.env): TargetConfig {
    const network = string(env, 'TARGET_NETWORK', 'arctic-1');
    if (network !== 'arctic-1' && network !== 'atlantic-2') {
        throw new Error('TARGET_NETWORK must be "arctic-1" or "atlantic-2"');
    }
    const defaults = TARGETS[network];
    const userCount = positiveInteger(env, 'USER_COUNT', 100);
    return {
        network,
        ...defaults,
        evmRpcUrl: string(env, 'TARGET_EVM_RPC', defaults.evmRpcUrl),
        cosmosRpcUrl: string(env, 'TARGET_COSMOS_RPC', defaults.cosmosRpcUrl),
        usersPath: resolvePath(
            env,
            'LOAD_USERS',
            `runtime/replay-users/${network}-${userCount}.json`,
        ),
        deploymentPath: resolvePath(
            env,
            'LOAD_DEPLOYMENT',
            `runtime/replay-deployments/${network}-v4.json`,
        ),
        mnemonic: string(env, 'TARGET_MNEMONIC', string(env, 'SEI_ADMIN_MNEMONIC', '')),
    };
}

export function loadCaptureConfig(env: Environment = process.env) {
    const captureId = string(
        env,
        'CAPTURE_ID',
        new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, 'Z'),
    );
    return {
        evmRpcUrl: string(env, 'MAINNET_RPC', string(env, 'PACIFIC_EVM_RPC', PACIFIC_EVM_RPC)),
        cosmosRpcUrl: string(
            env,
            'COSMOS_RPC',
            string(env, 'PACIFIC_COSMOS_RPC', PACIFIC_COSMOS_RPC),
        ),
        recordMinutes: positiveNumber(env, 'RECORD_MINUTES', 20),
        segmentBlocks: positiveInteger(env, 'SEGMENT_BLOCKS', 200),
        tipLagBlocks: nonNegativeInteger(env, 'TIP_LAG_BLOCKS', 10),
        startBlock: optionalPositiveInteger(env, 'START_BLOCK'),
        endBlock: optionalPositiveInteger(env, 'END_BLOCK'),
        captureId,
        replayDirectory: resolvePath(env, 'REPLAY_DIR', DEFAULT_REPLAY_DIR),
        evmConcurrency: positiveInteger(env, 'RPC_CONCURRENCY', 2),
        cosmosConcurrency: positiveInteger(env, 'COSMOS_RPC_CONCURRENCY', 6),
        blocksPerBatch: boundedPositiveInteger(env, 'BLOCKS_PER_BATCH', 20, 20),
        ...traceConfig(env),
        allowUnlaggedTip: flag(env, 'ALLOW_UNLAGGED_TIP'),
    };
}

export function loadReplayConfig(env: Environment = process.env) {
    return {
        execute: flag(env, 'EXECUTE'),
        timeScale: positiveNumber(env, 'TIME_SCALE', 1),
        maxTps: positiveNumber(env, 'MAX_TPS', 25),
        maxSegments: optionalPositiveInteger(env, 'MAX_SEGMENTS'),
        replayThroughBlock: optionalPositiveInteger(env, 'REPLAY_THROUGH_BLOCK'),
        followSegments: flag(env, 'FOLLOW_SEGMENTS'),
        liveReplay: flag(env, 'LIVE_REPLAY'),
        runDurationSeconds: runDurationSeconds(env),
        followPollMs: positiveInteger(env, 'FOLLOW_POLL_MS', 2_000),
        minBufferMinutes: positiveNumber(env, 'MIN_BUFFER_MINUTES', 5),
        resumeBufferMinutes: positiveNumber(env, 'RESUME_BUFFER_MINUTES', 20),
        metricsPort: nonNegativeInteger(env, 'METRICS_PORT', 9465),
        metricsHost: string(env, 'METRICS_HOST', '0.0.0.0'),
        privilegedReplayMode: privilegedMode(env),
        logBuckets: flag(env, 'LOG_BUCKETS'),
        workerCount: positiveInteger(env, 'WORKER_COUNT', 20),
        maxPendingPerLane: positiveInteger(env, 'MAX_PENDING_PER_LANE', 2),
        maxGasPerTransaction: BigInt(positiveInteger(env, 'MAX_GAS_PER_TX', 5_000_000)),
        maxCalldataBytes: minimumInteger(env, 'MAX_CALLDATA_BYTES', 131_072, 260),
        maxCosmosBytes: positiveInteger(env, 'MAX_COSMOS_BYTES', 1_000_000),
        maxValueWei: positiveBigInt(env, 'MAX_VALUE_WEI', 1_000_000_000_000_000n),
        maxCosmosMessages: positiveInteger(env, 'MAX_COSMOS_MESSAGES', 10),
        replayDirectory: resolvePath(env, 'REPLAY_DIR', DEFAULT_REPLAY_DIR),
        bucketAuditPath: env.BUCKET_AUDIT_PATH,
        unbucketedAuditPath: env.UNBUCKETED_AUDIT_PATH,
        replayReportPath: env.REPLAY_REPORT,
        skipFixturePrepare: flag(env, 'SKIP_FIXTURE_PREPARE'),
        replayFromStart: flag(env, 'REPLAY_FROM_START'),
        cleanupConsumedSegments: booleanOption(env, 'CLEANUP_CONSUMED_SEGMENTS', false),
        retainCompletedSegments: nonNegativeInteger(env, 'RETAIN_COMPLETED_SEGMENTS', 1),
        cleanupIntervalBlocks: positiveInteger(env, 'CLEANUP_INTERVAL_BLOCKS', 200),
        fixturePrepareGasLimit: BigInt(
            positiveInteger(env, 'FIXTURE_PREPARE_GAS_LIMIT', 2_000_000),
        ),
    };
}

export function loadBufferedConfig(env: Environment = process.env) {
    const segmentBlocks = positiveInteger(env, 'SEGMENT_BLOCKS', 200);
    const startMode = string(env, 'BUFFER_START_MODE', 'latest');
    if (startMode !== 'latest' && startMode !== 'resume') {
        throw new Error('BUFFER_START_MODE must be "latest" or "resume"');
    }
    return {
        execute: flag(env, 'EXECUTE'),
        replayDirectory: resolvePath(env, 'REPLAY_DIR', DEFAULT_REPLAY_DIR),
        startMode,
        initialBufferBlocks: positiveInteger(
            env,
            'INITIAL_BUFFER_BLOCKS',
            segmentBlocks,
        ),
        minBufferMinutes: positiveNumber(env, 'MIN_BUFFER_MINUTES', 5),
        resumeBufferMinutes: positiveNumber(env, 'RESUME_BUFFER_MINUTES', 20),
        segmentBlocks,
        replayBatchSegments: positiveInteger(env, 'REPLAY_BATCH_SEGMENTS', 5),
        tipLagBlocks: nonNegativeInteger(env, 'TIP_LAG_BLOCKS', 10),
        sourcePollMs: positiveInteger(env, 'SOURCE_POLL_MS', 2_000),
        runDurationSeconds: runDurationSeconds(env, 2 * 60 * 60),
        timeScale: positiveNumber(env, 'TIME_SCALE', 1),
        evmRpcUrl: string(env, 'MAINNET_RPC', string(env, 'PACIFIC_EVM_RPC', PACIFIC_EVM_RPC)),
        cosmosRpcUrl: string(
            env,
            'COSMOS_RPC',
            string(env, 'PACIFIC_COSMOS_RPC', PACIFIC_COSMOS_RPC),
        ),
        evmConcurrency: positiveInteger(env, 'RPC_CONCURRENCY', 2),
        cosmosConcurrency: positiveInteger(env, 'COSMOS_RPC_CONCURRENCY', 6),
        blocksPerBatch: boundedPositiveInteger(env, 'BLOCKS_PER_BATCH', 20, 20),
        ...traceConfig(env),
        allowBufferDrain: flag(env, 'ALLOW_BUFFER_DRAIN'),
        cleanupConsumedSegments: booleanOption(env, 'CLEANUP_CONSUMED_SEGMENTS', true),
    };
}

export function loadProvisionConfig(env: Environment = process.env) {
    const fundSei = positiveNumber(env, 'FUND_SEI', 100);
    return {
        execute: flag(env, 'EXECUTE'),
        userCount: positiveInteger(env, 'USER_COUNT', 100),
        fundSei,
        targetUsei: BigInt(Math.round(fundSei * 1_000_000)),
    };
}

export function loadDeployConfig(env: Environment = process.env) {
    return {
        execute: flag(env, 'EXECUTE'),
        forceDeploy: flag(env, 'FORCE_DEPLOY'),
        tokenSupply: string(env, 'REPLAY_TOKEN_SUPPLY', '1000000000'),
        liquidity: string(env, 'REPLAY_LIQUIDITY', '100000000'),
    };
}

export const replayDirectory = (env: Environment = process.env): string =>
    resolvePath(env, 'REPLAY_DIR', DEFAULT_REPLAY_DIR);

export async function verifyTargetRpc(
    config: TargetConfig,
    provider: { getNetwork(): Promise<{ chainId: bigint }> },
): Promise<void> {
    const actual = (await provider.getNetwork()).chainId;
    if (actual !== config.evmChainId) {
        throw new Error(
            `Refusing ${config.evmRpcUrl}: chain ${actual}, expected ` +
                `${config.network} (${config.evmChainId})`,
        );
    }
}

export async function verifyTargetCosmosRpc(
    config: TargetConfig,
    client: { getChainId(): Promise<string> },
): Promise<void> {
    const actual = await client.getChainId();
    if (actual !== config.cosmosChainId) {
        throw new Error(`Refusing Cosmos chain ${actual}; expected ${config.cosmosChainId}`);
    }
}

function privilegedMode(env: Environment): 'shape' | 'skip' {
    const value = string(env, 'PRIVILEGED_REPLAY_MODE', 'shape');
    if (value !== 'shape' && value !== 'skip') {
        throw new Error('PRIVILEGED_REPLAY_MODE must be "shape" or "skip"');
    }
    return value;
}

function string(env: Environment, key: string, fallback: string): string {
    const value = env[key]?.trim();
    return value ? value : fallback;
}

function flag(env: Environment, key: string): boolean {
    return env[key] === '1';
}

function booleanOption(env: Environment, key: string, fallback: boolean): boolean {
    const value = env[key]?.trim();
    if (!value) return fallback;
    if (value === '1') return true;
    if (value === '0') return false;
    throw new Error(`${key} must be "0" or "1"`);
}

function resolvePath(env: Environment, key: string, fallback: string): string {
    return path.resolve(string(env, key, fallback));
}

function positiveNumber(env: Environment, key: string, fallback: number): number {
    const value = Number(string(env, key, String(fallback)));
    if (!Number.isFinite(value) || value <= 0) throw new Error(`${key} must be greater than zero`);
    return value;
}

function positiveInteger(env: Environment, key: string, fallback: number): number {
    const value = positiveNumber(env, key, fallback);
    if (!Number.isInteger(value)) throw new Error(`${key} must be an integer`);
    return value;
}

function nonNegativeInteger(env: Environment, key: string, fallback: number): number {
    const value = Number(string(env, key, String(fallback)));
    if (!Number.isInteger(value) || value < 0) {
        throw new Error(`${key} must be a non-negative integer`);
    }
    return value;
}

function minimumInteger(
    env: Environment,
    key: string,
    fallback: number,
    minimum: number,
): number {
    const value = positiveInteger(env, key, fallback);
    if (value < minimum) throw new Error(`${key} must be at least ${minimum}`);
    return value;
}

function optionalPositiveInteger(env: Environment, key: string): number | undefined {
    if (!env[key]) return undefined;
    return positiveInteger(env, key, 1);
}

function runDurationSeconds(
    env: Environment,
    fallback?: number,
): number | undefined {
    if (env.RUN_DURATION_SECONDS?.trim()) {
        return positiveInteger(env, 'RUN_DURATION_SECONDS', fallback ?? 1);
    }
    if (env.RUN_DURATION_HOURS?.trim()) {
        const seconds = positiveNumber(env, 'RUN_DURATION_HOURS', 1) * 60 * 60;
        if (!Number.isSafeInteger(seconds)) {
            throw new Error('RUN_DURATION_HOURS must resolve to a whole number of seconds');
        }
        return seconds;
    }
    return fallback;
}

function positiveBigInt(env: Environment, key: string, fallback: bigint): bigint {
    const value = BigInt(string(env, key, fallback.toString()));
    if (value <= 0n) throw new Error(`${key} must be greater than zero`);
    return value;
}

function traceConfig(env: Environment): {
    traceCaptureMode: 'off' | 'calls' | 'full';
    traceConcurrency: number;
    traceMaxDepth: number;
    traceMaxFrames: number;
    traceTimeoutMs: number;
    traceMaxRetries: number;
} {
    const traceCaptureMode = string(env, 'TRACE_CAPTURE_MODE', 'calls');
    if (traceCaptureMode !== 'off' && traceCaptureMode !== 'calls' && traceCaptureMode !== 'full') {
        throw new Error('TRACE_CAPTURE_MODE must be "off", "calls", or "full"');
    }
    return {
        traceCaptureMode,
        traceConcurrency: boundedPositiveInteger(env, 'TRACE_CONCURRENCY', 1, 8),
        traceMaxDepth: boundedPositiveInteger(env, 'TRACE_MAX_DEPTH', 8, 32),
        traceMaxFrames: boundedPositiveInteger(env, 'TRACE_MAX_FRAMES', 64, 256),
        traceTimeoutMs: boundedPositiveInteger(env, 'TRACE_TIMEOUT_MS', 30_000, 300_000),
        traceMaxRetries: boundedPositiveInteger(env, 'TRACE_MAX_RETRIES', 3, 10),
    };
}

function boundedPositiveInteger(
    env: Environment,
    key: string,
    fallback: number,
    maximum: number,
): number {
    const value = positiveInteger(env, key, fallback);
    if (value > maximum) throw new Error(`${key} must be at most ${maximum}`);
    return value;
}
