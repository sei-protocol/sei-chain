import path from 'node:path';
import { Environment } from './config';

export const LOAD_TYPES = ['defi', 'tokenops', 'nativetransfers', 'simulate'] as const;
export type LoadType = (typeof LOAD_TYPES)[number];
export type LoadCommand = 'run' | 'setup' | 'provision';

export interface LoadGeneratorConfig {
    command: LoadCommand;
    type: LoadType;
    tps: number;
    maxTps: number;
    durationSeconds?: number;
    runId: string;
    runtimeDirectory: string;
    workerCount: number;
    partitionIndex: number;
    usersPerPartition: number;
    workerIndexOffset: number;
    usersPerTps: number;
    maxWorkerCount: number;
    maxPendingPerWorker: number;
    cosmosGasPriceUsei: number;
    metricsHost: string;
    metricsPort: number;
    receiptTimeoutMs: number;
    fixturePrepareGasLimit: bigint;
    auditMaxBytes: number;
    auditRetainFiles: number;
    operationWeights: Record<string, number>;
    cw1155Contract?: string;
    execute: boolean;
}

export function loadGeneratorConfig(
    argv: string[] = process.argv.slice(2),
    env: Environment = process.env,
): LoadGeneratorConfig {
    const args = parseArguments(argv);
    const command = commandValue(args.command);
    const type = loadType(args.type ?? env.LOAD_TYPE ?? 'simulate');
    const tps = positiveNumber(args.tps ?? env.TXS_PER_SECOND ?? env.MAX_TPS ?? '10', 'tps');
    const maxTps = positiveNumber(env.MAX_SYNTHETIC_TPS ?? '100', 'MAX_SYNTHETIC_TPS');
    if (type !== 'simulate' && tps > maxTps) {
        throw new Error(
            `tps ${tps} exceeds MAX_SYNTHETIC_TPS ${maxTps}; raise the safety limit explicitly`,
        );
    }
    const usersPerTps = positiveNumber(env.USERS_PER_TPS ?? '2', 'USERS_PER_TPS');
    const maxWorkerCount = positiveInteger(env.MAX_WORKER_COUNT ?? '200', 'MAX_WORKER_COUNT');
    const workerCount = env.WORKER_COUNT?.trim()
        ? positiveInteger(env.WORKER_COUNT, 'WORKER_COUNT')
        : Math.ceil(tps * usersPerTps);
    if (workerCount > maxWorkerCount) {
        throw new Error(
            `worker count ${workerCount} exceeds MAX_WORKER_COUNT ${maxWorkerCount}; ` +
                `raise the safety limit explicitly`,
        );
    }
    const usersPerPartition = positiveInteger(
        env.USERS_PER_PARTITION ?? String(workerCount),
        'USERS_PER_PARTITION',
    );
    if (workerCount > usersPerPartition) {
        throw new Error(
            `worker count ${workerCount} exceeds USERS_PER_PARTITION ${usersPerPartition}; ` +
                `increase the reserved range before increasing active workers`,
        );
    }
    const partitionIndex = nonNegativeInteger(
        env.PARTITION_INDEX ?? '0',
        'PARTITION_INDEX',
    );
    const workerIndexOffset = env.WORKER_INDEX_OFFSET?.trim()
        ? nonNegativeInteger(env.WORKER_INDEX_OFFSET, 'WORKER_INDEX_OFFSET')
        : partitionIndex * usersPerPartition;
    if (!Number.isSafeInteger(workerIndexOffset + usersPerPartition)) {
        throw new Error('worker index range exceeds the safe integer limit');
    }
    const durationRaw = args.duration ?? env.RUN_DURATION_SECONDS;
    const durationSeconds = durationRaw ? positiveNumber(durationRaw, 'duration') : undefined;
    const execute = env.EXECUTE === '1';
    const runId = (args.runId ?? env.RUN_ID ?? '').trim();
    if (execute && command === 'run' && !runId) {
        throw new Error('RUN_ID or --run-id is required when EXECUTE=1');
    }
    const effectiveRunId = runId || 'dry-run';
    return {
        command,
        type,
        tps,
        maxTps,
        durationSeconds,
        runId: effectiveRunId,
        runtimeDirectory: path.resolve(
            args.runtimeDirectory ?? env.LOAD_RUNTIME_DIR ?? `runtime/load-runs/${effectiveRunId}`,
        ),
        workerCount,
        partitionIndex,
        usersPerPartition,
        workerIndexOffset,
        usersPerTps,
        maxWorkerCount,
        maxPendingPerWorker: positiveInteger(
            env.MAX_PENDING_PER_LANE ?? '2',
            'MAX_PENDING_PER_LANE',
        ),
        // Cosmos charges the declared fee in full, so this must track the target network's
        // minimum gas price rather than being set generously.
        cosmosGasPriceUsei: positiveNumber(
            env.COSMOS_GAS_PRICE_USEI ?? '0.025',
            'COSMOS_GAS_PRICE_USEI',
        ),
        metricsHost: (env.METRICS_HOST ?? '127.0.0.1').trim(),
        metricsPort: nonNegativeInteger(env.METRICS_PORT ?? '9465', 'METRICS_PORT'),
        receiptTimeoutMs: positiveInteger(
            env.EVM_RECEIPT_TIMEOUT_MS ?? '60000',
            'EVM_RECEIPT_TIMEOUT_MS',
        ),
        fixturePrepareGasLimit: BigInt(
            positiveInteger(
                env.FIXTURE_PREPARE_GAS_LIMIT ?? '2000000',
                'FIXTURE_PREPARE_GAS_LIMIT',
            ),
        ),
        auditMaxBytes: positiveInteger(
            env.LOAD_AUDIT_MAX_BYTES ?? String(100 * 1024 * 1024),
            'LOAD_AUDIT_MAX_BYTES',
        ),
        auditRetainFiles: positiveInteger(
            env.LOAD_AUDIT_RETAIN_FILES ?? '5',
            'LOAD_AUDIT_RETAIN_FILES',
        ),
        operationWeights: parseWeights(env.LOAD_MIX),
        cw1155Contract: optional(env.CW1155_CONTRACT),
        execute,
    };
}

interface ParsedArguments {
    command?: string;
    type?: string;
    tps?: string;
    duration?: string;
    runId?: string;
    runtimeDirectory?: string;
}

function parseArguments(argv: string[]): ParsedArguments {
    const result: ParsedArguments = {};
    const values = new Map<string, keyof Omit<typeof result, 'command'>>([
        ['--type', 'type'],
        ['--tps', 'tps'],
        ['--duration', 'duration'],
        ['--run-id', 'runId'],
        ['--runtime-dir', 'runtimeDirectory'],
    ]);
    for (let index = 0; index < argv.length; index++) {
        const arg = argv[index];
        if (!arg.startsWith('-') && !result.command) {
            result.command = arg;
            continue;
        }
        const [flag, inline] = arg.split('=', 2);
        const key = values.get(flag);
        if (!key) throw new Error(`Unknown argument ${arg}`);
        const value = inline ?? argv[++index];
        if (!value || value.startsWith('--')) throw new Error(`${flag} requires a value`);
        result[key] = value;
    }
    return result;
}

function commandValue(value?: string): LoadCommand {
    const command = value ?? 'run';
    if (command !== 'run' && command !== 'setup' && command !== 'provision') {
        throw new Error('Command must be run, setup, or provision');
    }
    return command;
}

function loadType(value: string): LoadType {
    if (!LOAD_TYPES.includes(value as LoadType)) {
        throw new Error(`type must be one of ${LOAD_TYPES.join(', ')}`);
    }
    return value as LoadType;
}

function parseWeights(value?: string): Record<string, number> {
    if (!value?.trim()) return {};
    return Object.fromEntries(
        value.split(',').map(entry => {
            const [name, weight] = entry.split(':').map(part => part.trim());
            if (!name || !weight) throw new Error('LOAD_MIX must use operation:weight entries');
            return [name, positiveNumber(weight, `LOAD_MIX ${name}`)];
        }),
    );
}

function positiveNumber(value: string, name: string): number {
    const parsed = Number(value);
    if (!Number.isFinite(parsed) || parsed <= 0)
        throw new Error(`${name} must be greater than zero`);
    return parsed;
}

function positiveInteger(value: string, name: string): number {
    const parsed = positiveNumber(value, name);
    if (!Number.isInteger(parsed)) throw new Error(`${name} must be an integer`);
    return parsed;
}

function nonNegativeInteger(value: string, name: string): number {
    // An empty value reaches here when the downward API cannot resolve a field, so it must
    // fail rather than parse as zero: PARTITION_INDEX 0 puts every pod on the same accounts.
    const parsed = value.trim() ? Number(value) : Number.NaN;
    if (!Number.isInteger(parsed) || parsed < 0) {
        throw new Error(`${name} must be a non-negative integer`);
    }
    return parsed;
}

function optional(value?: string): string | undefined {
    const trimmed = value?.trim();
    return trimmed || undefined;
}
