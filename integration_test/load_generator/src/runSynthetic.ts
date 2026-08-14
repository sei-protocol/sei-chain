import fs from 'node:fs/promises';
import path from 'node:path';
import { randomUUID } from 'node:crypto';
import { coins } from '@cosmjs/amino';
import { SigningStargateClient, StargateClient, TimeoutError } from '@cosmjs/stargate';
import { ethers } from 'ethers';
import { loadTargetConfig, verifyTargetCosmosRpc, verifyTargetRpc } from './config';
import { prepareSemanticFixtures } from './fixturePreparation';
import { readJson, writeJsonAtomic } from './io';
import { cosmosWalletAt, privateKeyAt, replayRegistry } from './keys';
import { LoadAuditWriter } from './loadAudit';
import { LoadGeneratorConfig } from './loadConfig';
import { LoadMetrics, LoadOutcome } from './loadMetrics';
import {
    ReplayDeploymentManifest,
    REPLAY_DEPLOYMENT_SCHEMA_VERSION,
    ReplayUserManifest,
} from './replay/replayTypes';
import { defiOperations } from './workloads/defi';
import { nativeTransferOperations } from './workloads/nativetransfers';
import {
    applyOperationWeights,
    chooseOperation,
    nextScheduleAt,
    paceUntil,
    seededRandom,
} from './workloads/scheduler';
import { prepareTokenFixtures, tokenOperations } from './workloads/tokenops';
import { BuiltLoad, LoadOperation, LoadWorker, WorkloadContext } from './workloads/types';

interface SyntheticWorker extends LoadWorker {
    mnemonic: string;
    evmPending: number;
    cosmosPending: number;
    evmQueue: Promise<void>;
    cosmosQueue: Promise<void>;
    cosmosClient?: SigningStargateClient;
}

class FeeOracle {
    private updatedAt = 0;
    private chainId?: bigint;
    private fees?: { maxFeePerGas: bigint; maxPriorityFeePerGas: bigint };

    constructor(private readonly provider: ethers.Provider) {}

    async transactionFields(): Promise<{
        chainId: bigint;
        maxFeePerGas: bigint;
        maxPriorityFeePerGas: bigint;
    }> {
        if (!this.chainId) this.chainId = (await this.provider.getNetwork()).chainId;
        if (!this.fees || Date.now() - this.updatedAt >= 60_000) {
            const fees = await this.provider.getFeeData();
            const gasPrice = fees.gasPrice ?? 50_000_000_000n;
            this.fees = {
                maxFeePerGas: fees.maxFeePerGas ?? gasPrice * 2n,
                maxPriorityFeePerGas: fees.maxPriorityFeePerGas ?? 1_000_000_000n,
            };
            this.updatedAt = Date.now();
        }
        return { chainId: this.chainId, ...this.fees };
    }

    invalidate(): void {
        this.fees = undefined;
    }
}

export async function runSynthetic(config: LoadGeneratorConfig): Promise<void> {
    if (config.type === 'simulate') throw new Error('simulate must use the replay runner');
    if (!config.execute) {
        console.log(
            `Dry-run ${config.type}: ${config.tps} tx/s, ${config.workerCount} workers, ` +
                `pool users ${config.workerIndexOffset + 1}-` +
                `${config.workerIndexOffset + config.workerCount} from reserved range ` +
                `${config.workerIndexOffset + 1}-` +
                `${config.workerIndexOffset + config.usersPerPartition}, ` +
                `${config.durationSeconds ?? 'unbounded'} seconds ` +
                `(safety ceiling ${config.maxTps} tx/s)`,
        );
        return;
    }
    const target = loadTargetConfig();
    if (!target.mnemonic) throw new Error('TARGET_MNEMONIC is required');
    await fs.mkdir(config.runtimeDirectory, { recursive: true });
    const executionId = randomUUID();
    const startedAt = new Date().toISOString();
    await writeJsonAtomic(path.join(config.runtimeDirectory, 'run.json'), {
        runId: config.runId,
        executionId,
        type: config.type,
        tps: config.tps,
        maxTps: config.maxTps,
        workerCount: config.workerCount,
        partitionIndex: config.partitionIndex,
        usersPerPartition: config.usersPerPartition,
        workerIndexOffset: config.workerIndexOffset,
        workerIndexStart: config.workerIndexOffset + 1,
        workerIndexEnd: config.workerIndexOffset + config.workerCount,
        reservedUserIndexEnd: config.workerIndexOffset + config.usersPerPartition,
        usersPerTps: config.usersPerTps,
        startedAt,
    });
    const [usersManifest, deployment] = await Promise.all([
        readJson<ReplayUserManifest>(target.usersPath),
        readJson<ReplayDeploymentManifest>(target.deploymentPath),
    ]);
    validateManifests(usersManifest, deployment, target.network, target.evmChainId);
    const selectedUsers = selectWorkerUsers(
        usersManifest.users,
        config.workerIndexOffset,
        config.workerCount,
    );

    const provider = new ethers.JsonRpcProvider(target.evmRpcUrl);
    provider.pollingInterval = 200;
    const metrics = new LoadMetrics(config.type, config.tps);
    const audit = new LoadAuditWriter(
        path.join(config.runtimeDirectory, 'transactions.jsonl'),
        config.auditMaxBytes,
        config.auditRetainFiles,
    );
    const abort = new AbortController();
    const requestStop = (signal: string) => {
        if (abort.signal.aborted) return;
        console.log(`Received ${signal}; stopping new submissions...`);
        abort.abort();
    };
    process.once('SIGINT', () => requestStop('SIGINT'));
    process.once('SIGTERM', () => requestStop('SIGTERM'));

    const workers: SyntheticWorker[] = [];
    try {
        await audit.initialize();
        if (config.metricsPort > 0) {
            await metrics.listen(config.metricsPort, config.metricsHost);
            console.log(`Metrics: http://${config.metricsHost}:${config.metricsPort}/metrics`);
        }
        await verifyTargetRpc(target, provider);
        const verifier = await StargateClient.connect(target.cosmosRpcUrl);
        try {
            await verifyTargetCosmosRpc(target, verifier);
        } finally {
            verifier.disconnect();
        }
        workers.push(
            ...(await createWorkers(selectedUsers, target.mnemonic, provider)),
        );
        if (workers.length < 2) throw new Error('At least two provisioned workers are required');
        const context: WorkloadContext = {
            runId: config.runId,
            executionId,
            deployment,
            provider,
            workers,
            cw1155Contract: config.cw1155Contract,
        };
        if (config.type === 'defi') {
            await prepareSemanticFixtures(
                workers,
                deployment,
                config.fixturePrepareGasLimit,
                config.receiptTimeoutMs,
            );
        } else if (config.type === 'tokenops') {
            await prepareTokenFixtures(workers, context, config.receiptTimeoutMs);
        }
        const operations = applyOperationWeights(
            operationsFor(config.type, context),
            config.operationWeights,
        );
        metrics.setReady(true);
        console.log(
            `Starting ${config.type} load at ${config.tps} tx/s with ${workers.length} workers ` +
                `(safety ceiling ${config.maxTps} tx/s)`,
        );
        await runSchedule(config, target.cosmosRpcUrl, workers, operations, metrics, audit, abort);
    } finally {
        metrics.setReady(false);
        abort.abort();
        await Promise.allSettled(
            workers.map(async worker => {
                await Promise.allSettled([worker.evmQueue, worker.cosmosQueue]);
                worker.cosmosClient?.disconnect();
            }),
        );
        await audit.flush();
        await metrics.close();
        provider.destroy();
    }
}

async function runSchedule(
    config: LoadGeneratorConfig,
    cosmosRpcUrl: string,
    workers: SyntheticWorker[],
    operations: LoadOperation[],
    metrics: LoadMetrics,
    audit: LoadAuditWriter,
    abort: AbortController,
): Promise<void> {
    const active = new Set<Promise<void>>();
    const random = seededRandom(config.runId);
    const startedAt = Date.now();
    const feeOracle = new FeeOracle(workers[0].wallet.provider!);
    const deadline = config.durationSeconds
        ? startedAt + config.durationSeconds * 1_000
        : undefined;
    const deadlineTimer = deadline
        ? setTimeout(() => abort.abort(), Math.max(0, deadline - Date.now()))
        : undefined;
    let sequence = 0;
    let scheduledAt = startedAt;
    try {
        while (!abort.signal.aborted && (!deadline || Date.now() < deadline)) {
            if (!(await paceUntil(scheduledAt, abort.signal))) break;
            scheduledAt = nextScheduleAt(scheduledAt, config.tps);
            const operationSequence = sequence;
            const operation = chooseOperation(operations, random());
            const worker = workers[operationSequence % workers.length];
            const pending = operation.lane === 'evm' ? worker.evmPending : worker.cosmosPending;
            if (pending >= config.maxPendingPerWorker) {
                metrics.record(operation.lane, operation.name, 'skipped');
                await audit.record(
                    auditRecord(config, worker, operation, operationSequence, 'skipped'),
                );
                sequence++;
                continue;
            }
            if (operation.lane === 'evm') worker.evmPending++;
            else worker.cosmosPending++;
            metrics.setPending(
                operation.lane,
                workers.reduce(
                    (sum, item) =>
                        sum + (operation.lane === 'evm' ? item.evmPending : item.cosmosPending),
                    0,
                ),
            );
            const previous = operation.lane === 'evm' ? worker.evmQueue : worker.cosmosQueue;
            const task = previous.then(() =>
                executeOperation(
                    config,
                    cosmosRpcUrl,
                    worker,
                    operation,
                    operationSequence,
                    metrics,
                    audit,
                    feeOracle,
                ),
            );
            const settled = task
                .catch(error => {
                    console.error(
                        `${operation.name} worker ${worker.index}: ` +
                            `${error instanceof Error ? error.message : error}`,
                    );
                })
                .finally(() => {
                    if (operation.lane === 'evm') worker.evmPending--;
                    else worker.cosmosPending--;
                    metrics.setPending(
                        operation.lane,
                        workers.reduce(
                            (sum, item) =>
                                sum +
                                (operation.lane === 'evm' ? item.evmPending : item.cosmosPending),
                            0,
                        ),
                    );
                    active.delete(settled);
                });
            if (operation.lane === 'evm') worker.evmQueue = settled;
            else worker.cosmosQueue = settled;
            active.add(settled);
            sequence++;
        }
    } finally {
        if (deadlineTimer) clearTimeout(deadlineTimer);
    }
    abort.abort();
    await Promise.allSettled(active);
}

async function executeOperation(
    config: LoadGeneratorConfig,
    cosmosRpcUrl: string,
    worker: SyntheticWorker,
    operation: LoadOperation,
    sequence: number,
    metrics: LoadMetrics,
    audit: LoadAuditWriter,
    feeOracle: FeeOracle,
): Promise<void> {
    const startedAt = process.hrtime.bigint();
    let hash: string | undefined;
    let outcome: LoadOutcome = 'rejected';
    let errorMessage: string | undefined;
    try {
        const built = await operation.build(worker, sequence);
        if (built.lane !== operation.lane) {
            throw new Error(`${operation.name} built the wrong lane`);
        }
        if (built.lane === 'evm') {
            hash = await executeEvm(
                worker,
                built,
                config.receiptTimeoutMs,
                metrics,
                operation.name,
                feeOracle,
            );
        } else {
            hash = await executeCosmos(worker, built, cosmosRpcUrl, metrics, operation.name);
        }
        outcome = 'included';
    } catch (error) {
        const submittedHash = (error as { transactionHash?: string }).transactionHash;
        hash = hash ?? submittedHash;
        outcome = (error as { loadOutcome?: LoadOutcome }).loadOutcome ?? 'rejected';
        errorMessage = error instanceof Error ? error.message : String(error);
        metrics.record(operation.lane, operation.name, outcome);
    }
    metrics.observe(
        operation.lane,
        operation.name,
        outcome,
        Number(process.hrtime.bigint() - startedAt) / 1e9,
    );
    await audit.record({
        ...auditRecord(config, worker, operation, sequence, outcome),
        hash,
        error: errorMessage,
    });
}

async function executeEvm(
    worker: SyntheticWorker,
    built: Extract<BuiltLoad, { lane: 'evm' }>,
    timeoutMs: number,
    metrics: LoadMetrics,
    operation: string,
    feeOracle: FeeOracle,
): Promise<string> {
    const fees = await feeOracle.transactionFields();
    const signed = await worker.wallet.signTransaction({
        ...built.transaction,
        chainId: fees.chainId,
        nonce: worker.evmNonce,
        type: 2,
        maxFeePerGas: fees.maxFeePerGas,
        maxPriorityFeePerGas: fees.maxPriorityFeePerGas,
    });
    let response: ethers.TransactionResponse;
    try {
        response = await worker.wallet.provider!.broadcastTransaction(signed);
    } catch (error) {
        feeOracle.invalidate();
        worker.evmNonce = await worker.wallet.provider!.getTransactionCount(
            worker.evmAddress,
            'pending',
        );
        throw error;
    }
    worker.evmNonce++;
    metrics.record('evm', operation, 'submitted');
    let receipt: ethers.TransactionReceipt | null;
    try {
        receipt = await response.wait(1, timeoutMs);
    } catch (error) {
        const failedReceipt = (error as { receipt?: ethers.TransactionReceipt }).receipt;
        if (failedReceipt?.status === 0) {
            throw outcomeError('EVM transaction reverted', 'included_failed', response.hash);
        }
        feeOracle.invalidate();
        await resyncEvmNonce(worker);
        throw outcomeError(
            error instanceof Error ? error.message : String(error),
            'poll_timeout',
            response.hash,
        );
    }
    if (!receipt) throw outcomeError('EVM receipt unavailable', 'poll_timeout', response.hash);
    if (receipt.status === 0) {
        throw outcomeError('EVM transaction reverted', 'included_failed', response.hash);
    }
    metrics.record('evm', operation, 'included');
    return response.hash;
}

async function resyncEvmNonce(worker: SyntheticWorker): Promise<void> {
    try {
        worker.evmNonce = await worker.wallet.provider!.getTransactionCount(
            worker.evmAddress,
            'pending',
        );
    } catch {
        // Keep the local nonce; the next broadcast rejection will retry synchronization.
    }
}

async function executeCosmos(
    worker: SyntheticWorker,
    built: Extract<BuiltLoad, { lane: 'cosmos' }>,
    cosmosRpcUrl: string,
    metrics: LoadMetrics,
    operation: string,
): Promise<string> {
    const client = await cosmosClient(worker, cosmosRpcUrl);
    try {
        const result = await client.signAndBroadcast(
            worker.seiAddress,
            [...built.messages],
            {
                amount: coins(built.feeUsei ?? '25000', 'usei'),
                gas: built.gas ?? '250000',
            },
            built.memo ?? `loadgen ${operation}`,
        );
        metrics.record('cosmos', operation, 'submitted');
        if (result.code !== 0) {
            throw outcomeError(
                `Cosmos transaction failed: ${result.rawLog}`,
                'included_failed',
                result.transactionHash,
            );
        }
        metrics.record('cosmos', operation, 'included');
        return result.transactionHash;
    } catch (error) {
        if (error instanceof TimeoutError) {
            metrics.record('cosmos', operation, 'submitted');
            worker.cosmosClient?.disconnect();
            worker.cosmosClient = undefined;
            throw outcomeError(error.message, 'poll_timeout');
        }
        throw error;
    }
}

async function cosmosClient(
    worker: SyntheticWorker,
    rpcUrl: string,
): Promise<SigningStargateClient> {
    if (worker.cosmosClient) return worker.cosmosClient;
    const wallet = await cosmosWalletAt(worker.mnemonic, worker.index);
    worker.cosmosClient = await SigningStargateClient.connectWithSigner(rpcUrl, wallet, {
        registry: replayRegistry(),
        broadcastPollIntervalMs: 200,
        broadcastTimeoutMs: 60_000,
    });
    return worker.cosmosClient;
}

export function selectWorkerUsers(
    users: ReplayUserManifest['users'],
    offset: number,
    count: number,
): ReplayUserManifest['users'] {
    const end = offset + count;
    if (end > users.length) {
        throw new Error(
            `User pool has ${users.length} workers but this pod requires indexes ` +
                `${offset + 1}-${end}; provision a larger pool or reduce replicas`,
        );
    }
    const selected = users.slice(offset, end);
    selected.forEach((user, index) => {
        const expected = offset + index + 1;
        if (user.index !== expected) {
            throw new Error(
                `User pool entry ${offset + index} has derivation index ${user.index}, ` +
                    `expected ${expected}`,
            );
        }
    });
    return selected;
}

async function createWorkers(
    users: ReplayUserManifest['users'],
    mnemonic: string,
    provider: ethers.JsonRpcProvider,
): Promise<SyntheticWorker[]> {
    return Promise.all(
        users.map(async (user, slot) => {
            const wallet = new ethers.Wallet(privateKeyAt(mnemonic, user.index), provider);
            const cosmosWallet = await cosmosWalletAt(mnemonic, user.index);
            const seiAddress = (await cosmosWallet.getAccounts())[0].address;
            if (
                wallet.address.toLowerCase() !== user.evmAddress.toLowerCase() ||
                seiAddress !== user.seiAddress
            ) {
                throw new Error(`Derived address mismatch for worker ${user.index}`);
            }
            return {
                slot,
                index: user.index,
                mnemonic,
                seiAddress,
                evmAddress: wallet.address,
                wallet,
                evmNonce: await provider.getTransactionCount(wallet.address, 'pending'),
                evmPending: 0,
                cosmosPending: 0,
                evmQueue: Promise.resolve(),
                cosmosQueue: Promise.resolve(),
            };
        }),
    );
}

function operationsFor(
    type: Exclude<LoadGeneratorConfig['type'], 'simulate'>,
    context: WorkloadContext,
): LoadOperation[] {
    if (type === 'defi') return defiOperations(context);
    if (type === 'tokenops') return tokenOperations(context);
    return nativeTransferOperations(context);
}

function validateManifests(
    users: ReplayUserManifest,
    deployment: ReplayDeploymentManifest,
    network: string,
    chainId: bigint,
): void {
    if (
        users.schemaVersion !== 1 ||
        users.network !== network ||
        users.chainId !== Number(chainId)
    ) {
        throw new Error('User manifest does not match the target');
    }
    if (
        deployment.schemaVersion !== REPLAY_DEPLOYMENT_SCHEMA_VERSION ||
        deployment.network !== network ||
        deployment.chainId !== Number(chainId)
    ) {
        throw new Error('Deployment manifest does not match the target');
    }
}

function auditRecord(
    config: LoadGeneratorConfig,
    worker: SyntheticWorker,
    operation: LoadOperation,
    sequence: number,
    outcome: LoadOutcome,
) {
    return {
        timestamp: new Date().toISOString(),
        runId: config.runId,
        loadType: config.type,
        sequence,
        worker: worker.index,
        operation: operation.name,
        lane: operation.lane,
        outcome,
    };
}

function outcomeError(message: string, loadOutcome: LoadOutcome, transactionHash?: string): Error {
    return Object.assign(new Error(message), { loadOutcome, transactionHash });
}
