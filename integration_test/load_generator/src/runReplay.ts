/**
 * Replay captured Pacific segments on Arctic-1 or Atlantic-2.
 *
 * Dry-run classification:
 *   REPLAY_DIR=runtime/replay/pacific-1/<capture> npm run replay:run
 *
 * Execute:
 *   TARGET_NETWORK=arctic-1 REPLAY_DIR=... EXECUTE=1 npm run replay:run
 */
import path from 'path';
import { SigningStargateClient, TimeoutError } from '@cosmjs/stargate';
import { TxRaw } from 'cosmjs-types/cosmos/tx/v1beta1/tx';
import { ethers } from 'ethers';
import { buildCosmosReplay, BuiltCosmosReplay } from './replay/cosmosAdapters';
import {
    asSetCodeTransaction,
    buildEvmReplay,
    BuiltEvmReplay,
    ReplayFeeState,
    ReplayFidelity,
} from './replay/evmAdapters';
import {
    REPLAY_DEPLOYMENT_SCHEMA_VERSION,
    REPLAY_V4_CONTRACT_KEYS,
    ReplayBlock,
    ReplayDeploymentManifest,
    ReplayEvmTransaction,
    ReplaySegment,
    ReplayUserManifest,
} from './replay/replayTypes';
import { ReplayLane, ReplayMetrics } from './replay/replayMetrics';
import {
    cleanupConsumedReplaySegments,
    readReplaySegments,
    validateReplaySegments,
} from './replay/corpus';
import {
    BucketAuditRecord,
    BucketAuditWriter,
    BucketOutcome,
} from './replay/bucketAudit';
import { loadReplayConfig, loadTargetConfig, verifyTargetRpc } from './config';
import { readJson, readOptionalJson, writeJsonAtomic } from './io';
import { prepareSemanticFixtures } from './fixturePreparation';
import { cosmosWalletAt, privateKeyAt, replayRegistry } from './keys';
import { replayEntriesForBlock } from './replay/replayScheduling';
import { sourceTraceCounts } from './replay/traceCapture';
import {
    SUSHI_V2_PROVENANCE,
    validateSushiV2Provenance,
} from './sushiV2';

const replayConfig = loadReplayConfig();
const {
    execute: EXECUTE,
    timeScale: TIME_SCALE,
    maxTps: MAX_TPS,
    maxSegments: MAX_SEGMENTS,
    replayThroughBlock: REPLAY_THROUGH_BLOCK,
    followSegments: FOLLOW_SEGMENTS,
    liveReplay: LIVE_REPLAY,
    runDurationSeconds: RUN_DURATION_SECONDS,
    followPollMs: FOLLOW_POLL_MS,
    minBufferMinutes: MIN_BUFFER_MINUTES,
    resumeBufferMinutes: RESUME_BUFFER_MINUTES,
    metricsPort: METRICS_PORT,
    metricsHost: METRICS_HOST,
    privilegedReplayMode: PRIVILEGED_REPLAY_MODE,
    logBuckets: LOG_BUCKETS,
    workerCount: WORKER_COUNT,
    maxPendingPerLane: MAX_PENDING_PER_LANE,
    maxGasPerTransaction: MAX_GAS_PER_TX,
    maxCalldataBytes: MAX_CALLDATA_BYTES,
    maxCosmosBytes: MAX_COSMOS_BYTES,
    maxValueWei: MAX_VALUE_WEI,
    maxCosmosMessages: MAX_COSMOS_MESSAGES,
    fixturePrepareGasLimit: FIXTURE_PREPARE_GAS_LIMIT,
    cleanupConsumedSegments: CLEANUP_CONSUMED_SEGMENTS,
    retainCompletedSegments: RETAIN_COMPLETED_SEGMENTS,
    cleanupIntervalBlocks: CLEANUP_INTERVAL_BLOCKS,
} = replayConfig;
const MAX_COSMOS_MEMO_BYTES = 256;

interface Worker {
    slot: number;
    index: number;
    seiAddress: string;
    evmAddress: string;
    wallet: ethers.Wallet;
    evmNonce: number;
    evmPending: number;
    cosmosPending: number;
    evmQueue: Promise<void>;
    cosmosQueue: Promise<void>;
    cosmosClient?: SigningStargateClient;
}

interface AdapterMetrics {
    offered: number;
    submitted: number;
    included: number;
    includedFailed: number;
    rejected: number;
    skipped: number;
    sourceBytes: number;
    producedBytes: number;
    sourceCalldataBytes: number;
    producedCalldataBytes: number;
    byAdapter: Record<string, number>;
    byFidelity: Record<ReplayFidelity, number>;
    skipReasons: Record<string, number>;
}

interface ReplayBlockCheckpoint {
    schemaVersion: 1;
    targetNetwork: string;
    lastCompletedSourceBlock: number;
    updatedAt: string;
}

async function main(): Promise<void> {
    if (FOLLOW_SEGMENTS && (MAX_SEGMENTS || REPLAY_THROUGH_BLOCK)) {
        throw new Error('FOLLOW_SEGMENTS cannot be combined with MAX_SEGMENTS or REPLAY_THROUGH_BLOCK');
    }
    if (LIVE_REPLAY && !FOLLOW_SEGMENTS) {
        throw new Error('LIVE_REPLAY requires FOLLOW_SEGMENTS=1');
    }
    if (
        FOLLOW_SEGMENTS &&
        !LIVE_REPLAY &&
        MIN_BUFFER_MINUTES >= RESUME_BUFFER_MINUTES
    ) {
        throw new Error('MIN_BUFFER_MINUTES must be below RESUME_BUFFER_MINUTES');
    }
    const target = loadTargetConfig();
    const replayDirectory = replayConfig.replayDirectory;
    const segmentCache = new Map<string, ReplaySegment>();
    const segments = await readReplaySegments(replayDirectory, false, segmentCache);
    const throughBounded = REPLAY_THROUGH_BLOCK
        ? segments.filter(segment => segment.source.lastBlock <= REPLAY_THROUGH_BLOCK)
        : segments;
    const selected = MAX_SEGMENTS ? throughBounded.slice(0, MAX_SEGMENTS) : throughBounded;
    if (selected.length === 0) throw new Error(`No replay segments in ${replayDirectory}`);
    validateReplaySegments(selected);
    validatePeakTps(selected);

    console.log(
        `${EXECUTE ? 'Replaying' : 'Inspecting'} ${selected.length} segment(s) from Pacific-1 ` +
            `on ${target.network} at ${TIME_SCALE}x time`,
    );
    console.log(`Corpus: ${replayDirectory}`);

    const metrics = emptyMetrics();
    if (!EXECUTE) {
        const dry = dryRunContext(target.network, target.evmChainId);
        inspectSegments(selected, dry.users, dry.deployment, target.evmChainId, metrics);
        printSummary(metrics);
        return;
    }
    if (!target.mnemonic) {
        throw new Error('TARGET_MNEMONIC or SEI_ADMIN_MNEMONIC is required for replay execution');
    }

    const [manifest, deployment] = await Promise.all([
        readJson<ReplayUserManifest>(target.usersPath),
        readJson<ReplayDeploymentManifest>(target.deploymentPath),
    ]);
    validateTargetManifests(manifest, deployment, target.network, target.evmChainId);
    if (manifest.users.length < 2) throw new Error('At least two replay users are required');

    const provider = new ethers.JsonRpcProvider(target.evmRpcUrl);
    provider.pollingInterval = 200;
    await verifyTargetRpc(target, provider);
    await verifyDeploymentCode(deployment, provider);
    const users = { ...manifest, users: manifest.users.slice(0, WORKER_COUNT) };
    const workers = await createWorkers(users, target.mnemonic, provider);
    const liveMetrics = new ReplayMetrics(
        'pacific-1',
        target.network,
        TIME_SCALE,
        PRIVILEGED_REPLAY_MODE,
    );
    if (METRICS_PORT > 0) {
        await liveMetrics.listen(METRICS_PORT, METRICS_HOST);
        console.log(
            `Prometheus metrics: http://${METRICS_HOST}:${METRICS_PORT}/metrics ` +
                `(health: /healthz)`,
        );
    }
    const auditRunId = Date.now();
    const bucketAudit = new BucketAuditWriter(
        path.resolve(
            replayConfig.bucketAuditPath ??
                path.join(
                    replayDirectory,
                    `bucket-audit-${target.network}-${auditRunId}.jsonl`,
                ),
        ),
        path.resolve(
            replayConfig.unbucketedAuditPath ??
                path.join(
                    replayDirectory,
                    `unbucketed-${target.network}-${auditRunId}.jsonl`,
                ),
        ),
        LOG_BUCKETS,
    );
    await bucketAudit.initialize();
    console.log(`Bucket audit: ${bucketAudit.auditPath}`);
    console.log(`No-semantic-bucket list: ${bucketAudit.unmatchedPath}`);
    let stopRequested = false;
    const requestStop = (signal: string) => {
        if (stopRequested) return;
        stopRequested = true;
        console.log(`Received ${signal}; finishing the current source block...`);
    };
    process.once('SIGINT', () => requestStop('SIGINT'));
    process.once('SIGTERM', () => requestStop('SIGTERM'));

    try {
        if (!replayConfig.skipFixturePrepare) {
            await prepareSemanticFixtures(
                workers,
                deployment,
                FIXTURE_PREPARE_GAS_LIMIT,
            );
        }
        let fees = await readFees(provider);
        let feeUpdatedAt = Date.now();
        let evmCursor = 0;
        let cosmosCursor = 0;
        let sequence = 0;
        const active = new Set<Promise<void>>();
        const checkpointPath = path.join(
            replayDirectory,
            `replay-checkpoint-${target.network}.json`,
        );
        const checkpoint = replayConfig.replayFromStart
            ? undefined
            : await readOptionalJson<ReplayBlockCheckpoint>(checkpointPath);
        if (replayConfig.replayFromStart) {
            console.log('Ignoring the existing replay checkpoint; replaying from corpus start');
        }
        let lastCleanupSourceBlock: number | undefined;
        const cleanupSegments = async (completedThrough: number, force = false): Promise<void> => {
            if (
                !FOLLOW_SEGMENTS ||
                !CLEANUP_CONSUMED_SEGMENTS ||
                (!force &&
                    lastCleanupSourceBlock !== undefined &&
                    completedThrough - lastCleanupSourceBlock < CLEANUP_INTERVAL_BLOCKS)
            ) {
                return;
            }
            const removed = await cleanupConsumedReplaySegments(
                replayDirectory,
                completedThrough,
                RETAIN_COMPLETED_SEGMENTS,
            );
            lastCleanupSourceBlock = completedThrough;
            if (removed.length > 0) {
                console.log(
                    `Cleaned ${removed.length} consumed replay segment(s) through block ` +
                        `${completedThrough}; retained audits and reports`,
                );
            }
        };
        if (checkpoint?.targetNetwork === target.network) {
            await cleanupSegments(checkpoint.lastCompletedSourceBlock, true);
        }
        let nextSourceBlock =
            checkpoint?.targetNetwork === target.network
                ? checkpoint.lastCompletedSourceBlock + 1
                : selected[0].source.firstBlock;
        const firstTimestamp =
            selected
                .flatMap(segment => segment.blocks)
                .find(block => block.number >= nextSourceBlock)?.timestamp ??
            selected[0].source.startTimestamp;
        const replayStartedAt = Date.now();
        const runDeadline = RUN_DURATION_SECONDS
            ? replayStartedAt + RUN_DURATION_SECONDS * 1_000
            : undefined;
        let pausedMilliseconds = 0;
        let lastCompletedSourceBlock = checkpoint?.lastCompletedSourceBlock;
        let bufferPaused = false;
        let peakCheckedThroughBlock = selected[selected.length - 1].source.lastBlock;

        function track(job: Promise<void>): void {
            active.add(job);
            job.finally(() => active.delete(job)).catch(error => {
                // Lane handlers record their own failures; anything reaching
                // this point is an unclassified bug and must not vanish.
                console.error(
                    'Unexpected replay job failure:',
                    error instanceof Error ? error.message : error,
                );
            });
        }

        const pendingFor = (lane: ReplayLane): number =>
            workers.reduce(
                (sum, item) => sum + (lane === 'evm' ? item.evmPending : item.cosmosPending),
                0,
            );

        while (
            !stopRequested &&
            (runDeadline === undefined || Date.now() < runDeadline)
        ) {
            const availableSegments = FOLLOW_SEGMENTS
                ? await readReplaySegments(replayDirectory, false, segmentCache)
                : selected;
            validateReplaySegments(availableSegments);
            const newSegments = availableSegments.filter(
                segment => segment.source.firstBlock > peakCheckedThroughBlock,
            );
            if (newSegments.length > 0) {
                peakCheckedThroughBlock =
                    newSegments[newSegments.length - 1].source.lastBlock;
                const peak = scaledPeakTps(newSegments);
                if (peak > MAX_TPS) {
                    // Aborting a long follow run mid-flight would be worse than
                    // the burst; worker-queue backpressure bounds the overflow.
                    console.warn(
                        `Newly collected segments peak at ${peak.toFixed(1)} tx/s, ` +
                            `above MAX_TPS=${MAX_TPS}; relying on worker-queue backpressure`,
                    );
                }
            }
            const pendingBlocks = availableSegments
                .flatMap(segment => segment.blocks)
                .filter(block => block.number >= nextSourceBlock);
            const lastAvailableBlock =
                availableSegments[availableSegments.length - 1]?.blocks.at(-1);

            if (pendingBlocks.length === 0 || !lastAvailableBlock) {
                if (!FOLLOW_SEGMENTS) break;
                bufferPaused = true;
                liveMetrics.setProgress(
                    0,
                    lastAvailableBlock?.number ?? nextSourceBlock - 1,
                    lastCompletedSourceBlock,
                    true,
                );
                liveMetrics.setRunRemaining(remainingSeconds(runDeadline));
                const pausedAt = Date.now();
                await sleepBounded(FOLLOW_POLL_MS, runDeadline);
                pausedMilliseconds += Date.now() - pausedAt;
                continue;
            }

            const bufferSeconds = Math.max(
                0,
                lastAvailableBlock.timestamp - pendingBlocks[0].timestamp + 1,
            );
            if (
                FOLLOW_SEGMENTS &&
                !LIVE_REPLAY &&
                bufferSeconds < MIN_BUFFER_MINUTES * 60
            ) {
                bufferPaused = true;
            }
            if (
                FOLLOW_SEGMENTS &&
                !LIVE_REPLAY &&
                bufferPaused &&
                bufferSeconds < RESUME_BUFFER_MINUTES * 60
            ) {
                liveMetrics.setProgress(
                    bufferSeconds,
                    lastAvailableBlock.number,
                    lastCompletedSourceBlock,
                    true,
                );
                liveMetrics.setRunRemaining(remainingSeconds(runDeadline));
                const pausedAt = Date.now();
                await sleepBounded(FOLLOW_POLL_MS, runDeadline);
                pausedMilliseconds += Date.now() - pausedAt;
                continue;
            }
            bufferPaused = false;

            liveMetrics.setProgress(
                bufferSeconds,
                lastAvailableBlock.number,
                lastCompletedSourceBlock,
                false,
            );
            for (const block of pendingBlocks) {
                if (
                    stopRequested ||
                    (runDeadline !== undefined && Date.now() >= runDeadline)
                ) {
                    break;
                }
                const targetElapsedMs =
                    ((block.timestamp - firstTimestamp) * 1_000) / TIME_SCALE;
                await sleepUntil(replayStartedAt + pausedMilliseconds + targetElapsedMs);
                if (Date.now() - feeUpdatedAt > 60_000) {
                    try {
                        fees = await readFees(provider);
                    } catch (error) {
                        console.warn(
                            'EVM fee refresh failed; retaining the previous fee snapshot: ' +
                                `${error instanceof Error ? error.message : error}`,
                        );
                    }
                    feeUpdatedAt = Date.now();
                }

                for (const entry of replayEntriesForBlock(block)) {
                    const source = entry.cosmos;
                    const evm = entry.evm;
                    metrics.offered++;
                    const lane: ReplayLane = evm ? 'evm' : 'cosmos';
                    liveMetrics.recordOffered(lane);
                    const currentSequence = sequence++;
                    if (evm) {
                        const worker = workers[evmCursor++ % workers.length];
                        if (worker.evmPending >= MAX_PENDING_PER_LANE) {
                            recordSkip(metrics, 'EVM worker queue full', liveMetrics, 'evm');
                            await bucketAudit.record(evmAuditRecord(
                                evm,
                                entry.sourceCosmosHash,
                                block.number,
                                currentSequence,
                                target.network,
                                'queueFull',
                                'skipped',
                                'skipped',
                                'EVM worker queue full',
                            ));
                            continue;
                        }
                        worker.evmPending++;
                        liveMetrics.setPending('evm', pendingFor('evm'));
                        const job = worker.evmQueue.then(() =>
                            executeEvm(
                                evm,
                                entry.sourceCosmosHash,
                                worker,
                                currentSequence,
                                users,
                                deployment,
                                target.evmChainId,
                                fees,
                                provider,
                                metrics,
                                liveMetrics,
                                bucketAudit,
                                target.network,
                            ),
                        );
                        worker.evmQueue = job.catch(() => undefined);
                        track(
                            job.finally(() => {
                                worker.evmPending--;
                                liveMetrics.setPending('evm', pendingFor('evm'));
                            }),
                        );
                    } else {
                        if (!source) throw new Error('Replay schedule entry has no transaction');
                        const worker = workers[cosmosCursor++ % workers.length];
                        if (worker.cosmosPending >= MAX_PENDING_PER_LANE) {
                            recordSkip(
                                metrics,
                                'Cosmos worker queue full',
                                liveMetrics,
                                'cosmos',
                            );
                            await bucketAudit.record(
                                auditRecord(
                                    source,
                                    block.number,
                                    currentSequence,
                                    target.network,
                                    'queueFull',
                                    'skipped',
                                    'skipped',
                                    'Cosmos worker queue full',
                                ),
                            );
                            continue;
                        }
                        worker.cosmosPending++;
                        liveMetrics.setPending('cosmos', pendingFor('cosmos'));
                        const job = worker.cosmosQueue.then(() =>
                            executeCosmos(
                                source,
                                worker,
                                users,
                                target.cosmosRpcUrl,
                                target.mnemonic,
                                metrics,
                                liveMetrics,
                                bucketAudit,
                                block.number,
                                currentSequence,
                                target.network,
                            ),
                        );
                        worker.cosmosQueue = job.catch(() => undefined);
                        track(
                            job.finally(() => {
                                worker.cosmosPending--;
                                liveMetrics.setPending('cosmos', pendingFor('cosmos'));
                            }),
                        );
                    }
                }

                // A block-level checkpoint limits restart duplication to the current
                // source block while retaining within-block concurrency.
                await Promise.allSettled([...active]);
                lastCompletedSourceBlock = block.number;
                nextSourceBlock = block.number + 1;
                await writeJsonAtomic(checkpointPath, {
                    schemaVersion: 1,
                    targetNetwork: target.network,
                    lastCompletedSourceBlock,
                    updatedAt: new Date().toISOString(),
                } satisfies ReplayBlockCheckpoint);
                await cleanupSegments(lastCompletedSourceBlock);
                liveMetrics.setProgress(
                    Math.max(0, lastAvailableBlock.timestamp - block.timestamp),
                    lastAvailableBlock.number,
                    lastCompletedSourceBlock,
                    false,
                );
                liveMetrics.setRunRemaining(remainingSeconds(runDeadline));
            }

            if (!FOLLOW_SEGMENTS) break;
        }

        await Promise.allSettled([...active]);
        await bucketAudit.flush();
        const reportPath = path.resolve(
            replayConfig.replayReportPath ??
                path.join(
                    replayDirectory,
                    `replay-report-${target.network}-${Date.now()}.json`,
                ),
        );
        await writeJsonAtomic(reportPath, {
            schemaVersion: 1,
            generatedAt: new Date().toISOString(),
            source: 'pacific-1',
            target: target.network,
            timeScale: TIME_SCALE,
            runDurationSeconds: (Date.now() - replayStartedAt) / 1_000,
            privilegedReplayMode: PRIVILEGED_REPLAY_MODE,
            metrics,
            bucketAudit: {
                auditPath: bucketAudit.auditPath,
                unmatchedPath: bucketAudit.unmatchedPath,
                ...bucketAudit.summary(),
            },
        });
        printSummary(metrics);
        console.log(`Bucket summary: ${JSON.stringify(bucketAudit.summary())}`);
        console.log(`Report: ${reportPath}`);
    } finally {
        await bucketAudit.flush();
        for (const worker of workers) worker.cosmosClient?.disconnect();
        await liveMetrics.close();
    }
}

async function executeEvm(
    source: NonNullable<ReplayBlock['transactions'][number]['evm']>,
    sourceCosmosHash: string | undefined,
    worker: Worker,
    sequence: number,
    users: ReplayUserManifest,
    deployment: ReplayDeploymentManifest,
    chainId: bigint,
    fees: ReplayFeeState,
    provider: ethers.JsonRpcProvider,
    metrics: AdapterMetrics,
    liveMetrics: ReplayMetrics,
    bucketAudit: BucketAuditWriter,
    targetNetwork: string,
): Promise<void> {
    const startedAt = process.hrtime.bigint();
    let built: BuiltEvmReplay;
    try {
        built = buildEvmReplay(source, {
            chainId,
            deployment,
            users: users.users,
            workerIndex: worker.slot,
            sequence,
            nonce: worker.evmNonce,
            fees,
            maxGasPerTransaction: MAX_GAS_PER_TX,
            maxValueWei: MAX_VALUE_WEI,
            maxCalldataBytes: MAX_CALLDATA_BYTES,
        });
    } catch (error) {
        const reason = `EVM adapter error: ${error instanceof Error ? error.message : error}`;
        recordBuilt(metrics, 'adapterError', 'skipped');
        liveMetrics.recordAdapted('evm', 'adapterError', 'skipped');
        recordSkip(metrics, reason, liveMetrics, 'evm');
        await bucketAudit.record(evmAuditRecord(
            source,
            sourceCosmosHash,
            source.blockNumber,
            sequence,
            targetNetwork,
            'adapterError',
            'skipped',
            'skipped',
            reason,
        ));
        console.error(`EVM adapter user ${worker.index}: ${reason}`);
        return;
    }
    liveMetrics.recordTraceProfile(source);
    recordBuilt(metrics, built.adapter, built.fidelity);
    liveMetrics.recordAdapted('evm', built.adapter, built.fidelity);
    metrics.sourceCalldataBytes += built.sourceCalldataBytes;
    metrics.producedCalldataBytes += built.producedCalldataBytes;
    metrics.sourceBytes += source.sourceSerializedBytes ?? 0;
    liveMetrics.recordBytes('evm', 'source_calldata', built.sourceCalldataBytes);
    liveMetrics.recordBytes('evm', 'produced_calldata', built.producedCalldataBytes);
    liveMetrics.recordBytes('evm', 'source_transaction', source.sourceSerializedBytes ?? 0);
    liveMetrics.recordGas('evm', 'source', BigInt(source.receipt.gasUsed));
    if (!built.transaction) {
        recordSkip(metrics, built.reason ?? 'EVM adapter skipped', liveMetrics, 'evm');
        await bucketAudit.record({
            ...evmAuditRecord(
                source,
                sourceCosmosHash,
                source.blockNumber,
                sequence,
                targetNetwork,
                built.adapter,
                built.fidelity,
                'skipped',
                built.reason,
            ),
            targetCalldataBytes: built.producedCalldataBytes,
        });
        return;
    }
    let targetHash: string | undefined;
    let targetTransactionBytes: number | undefined;
    let outcome: BucketOutcome = 'included';
    try {
        let transaction = built.transaction;
        if (source.type === 4) {
            const implementation = deployment.contracts.profileHarness;
            if (!implementation) {
                throw new Error('Deployment is missing ProfileLoadHarness for EIP-7702 replay');
            }
            const authorization = await worker.wallet.authorize({
                address: implementation,
                chainId,
                nonce: worker.evmNonce + 1,
            });
            transaction = asSetCodeTransaction(transaction, authorization);
        }
        const signed = await worker.wallet.signTransaction(transaction);
        targetTransactionBytes = ethers.getBytes(signed).length;
        metrics.producedBytes += targetTransactionBytes;
        liveMetrics.recordBytes(
            'evm',
            'produced_transaction',
            targetTransactionBytes,
        );
        const response = await provider.broadcastTransaction(signed);
        targetHash = response.hash;
        // A type-4 transaction consumes two nonces: one for the transaction and
        // one when its self-authorization is applied (EIP-7702).
        worker.evmNonce += source.type === 4 ? 2 : 1;
        metrics.submitted++;
        liveMetrics.recordOutcome('evm', 'submitted');
        try {
            const receipt = await response.wait();
            if (!receipt) throw new Error('EVM receipt unavailable');
            metrics.included++;
            liveMetrics.recordOutcome('evm', 'included');
            liveMetrics.recordGas('evm', 'target', receipt.gasUsed);
            if (receipt.status === 0) {
                metrics.includedFailed++;
                liveMetrics.recordOutcome('evm', 'included_failed');
                outcome = 'included_failed';
            }
        } catch (error) {
            const receipt = (error as { receipt?: ethers.TransactionReceipt }).receipt;
            if (!receipt || receipt.status !== 0) throw error;
            metrics.included++;
            metrics.includedFailed++;
            liveMetrics.recordOutcome('evm', 'included');
            liveMetrics.recordOutcome('evm', 'included_failed');
            liveMetrics.recordGas('evm', 'target', receipt.gasUsed);
            outcome = 'included_failed';
        }
        liveMetrics.observeSubmission(
            'evm',
            outcome,
            Number(process.hrtime.bigint() - startedAt) / 1e9,
        );
        await bucketAudit.record({
            ...evmAuditRecord(
                source,
                sourceCosmosHash,
                source.blockNumber,
                sequence,
                targetNetwork,
                built.adapter,
                built.fidelity,
                outcome,
            ),
            targetHash,
            targetTransactionBytes,
            targetCalldataBytes: built.producedCalldataBytes,
        });
    } catch (error) {
        metrics.rejected++;
        liveMetrics.recordOutcome('evm', 'rejected');
        liveMetrics.observeSubmission(
            'evm',
            'rejected',
            Number(process.hrtime.bigint() - startedAt) / 1e9,
        );
        await bucketAudit.record({
            ...evmAuditRecord(
                source,
                sourceCosmosHash,
                source.blockNumber,
                sequence,
                targetNetwork,
                built.adapter,
                built.fidelity,
                'rejected',
            ),
            targetHash,
            targetTransactionBytes,
            targetCalldataBytes: built.producedCalldataBytes,
            error: error instanceof Error ? error.message : String(error),
        });
        try {
            worker.evmNonce = await provider.getTransactionCount(worker.evmAddress, 'pending');
        } catch {
            // Retain the local nonce and retry synchronization after the next rejection.
        }
        console.error(
            `EVM ${built.adapter} user ${worker.index}: ` +
                `${error instanceof Error ? error.message : error}`,
        );
    }
}

async function executeCosmos(
    source: ReplayBlock['transactions'][number],
    worker: Worker,
    users: ReplayUserManifest,
    cosmosRpcUrl: string,
    mnemonic: string,
    metrics: AdapterMetrics,
    liveMetrics: ReplayMetrics,
    bucketAudit: BucketAuditWriter,
    sourceBlock: number,
    sequence: number,
    targetNetwork: string,
): Promise<void> {
    const startedAt = process.hrtime.bigint();
    let built: BuiltCosmosReplay;
    try {
        built = buildCosmosReplay(source, {
            users: users.users,
            workerIndex: worker.slot,
            maxMessages: MAX_COSMOS_MESSAGES,
            privilegedMode: PRIVILEGED_REPLAY_MODE,
        });
    } catch (error) {
        const reason = `Cosmos adapter error: ${error instanceof Error ? error.message : error}`;
        recordBuilt(metrics, 'adapterError', 'skipped');
        liveMetrics.recordAdapted('cosmos', 'adapterError', 'skipped');
        recordSkip(metrics, reason, liveMetrics, 'cosmos');
        await bucketAudit.record(
            auditRecord(
                source,
                sourceBlock,
                sequence,
                targetNetwork,
                'adapterError',
                'skipped',
                'skipped',
                reason,
            ),
        );
        console.error(`Cosmos adapter user ${worker.index}: ${reason}`);
        return;
    }
    recordBuilt(metrics, built.adapter, built.fidelity);
    liveMetrics.recordAdapted('cosmos', built.adapter, built.fidelity);
    metrics.sourceBytes += source.transactionBytes;
    liveMetrics.recordBytes('cosmos', 'source_transaction', source.transactionBytes);
    liveMetrics.recordGas('cosmos', 'source', BigInt(source.result.gasUsed));
    if (!built.messages || !built.fee) {
        recordSkip(metrics, built.reason ?? 'Cosmos adapter skipped', liveMetrics, 'cosmos');
        await bucketAudit.record(
            auditRecord(
                source,
                sourceBlock,
                sequence,
                targetNetwork,
                built.adapter,
                built.fidelity,
                'skipped',
                built.reason,
            ),
        );
        return;
    }
    let targetTransactionBytes: number | undefined;
    let targetHash: string | undefined;
    try {
        const client = await cosmosClient(worker, cosmosRpcUrl, mnemonic);
        const bytes = await signCosmosToTargetSize(
            client,
            worker.seiAddress,
            built.messages,
            built.fee,
            built.memoPrefix ?? '',
            Math.min(built.targetTransactionBytes, MAX_COSMOS_BYTES),
        );
        targetTransactionBytes = bytes.length;
        metrics.producedBytes += targetTransactionBytes;
        liveMetrics.recordBytes('cosmos', 'produced_transaction', bytes.length);
        // broadcastTx polls through inclusion, so mempool acceptance and the
        // execution result are observed together on this lane.
        const result = await client.broadcastTx(bytes);
        metrics.submitted++;
        liveMetrics.recordOutcome('cosmos', 'submitted');
        targetHash = result.transactionHash;
        metrics.included++;
        liveMetrics.recordOutcome('cosmos', 'included');
        liveMetrics.recordGas('cosmos', 'target', result.gasUsed);
        if (result.code !== 0) {
            metrics.includedFailed++;
            liveMetrics.recordOutcome('cosmos', 'included_failed');
        }
        liveMetrics.observeSubmission(
            'cosmos',
            result.code === 0 ? 'included' : 'included_failed',
            Number(process.hrtime.bigint() - startedAt) / 1e9,
        );
        await bucketAudit.record({
            ...auditRecord(
                source,
                sourceBlock,
                sequence,
                targetNetwork,
                built.adapter,
                built.fidelity,
                result.code === 0 ? 'included' : 'included_failed',
                built.reason,
            ),
            targetHash,
            targetTransactionBytes,
        });
    } catch (error) {
        if (error instanceof TimeoutError) {
            // The node accepted the transaction; it just missed the poll window.
            metrics.submitted++;
            liveMetrics.recordOutcome('cosmos', 'submitted');
        }
        metrics.rejected++;
        liveMetrics.recordOutcome('cosmos', 'rejected');
        liveMetrics.observeSubmission(
            'cosmos',
            'rejected',
            Number(process.hrtime.bigint() - startedAt) / 1e9,
        );
        worker.cosmosClient?.disconnect();
        worker.cosmosClient = undefined;
        await bucketAudit.record({
            ...auditRecord(
                source,
                sourceBlock,
                sequence,
                targetNetwork,
                built.adapter,
                built.fidelity,
                'rejected',
                built.reason,
            ),
            targetHash,
            targetTransactionBytes,
            error: error instanceof Error ? error.message : String(error),
        });
        console.error(
            `Cosmos ${built.adapter} user ${worker.index}: ` +
                `${error instanceof Error ? error.message : error}`,
        );
    }
}

async function signCosmosToTargetSize(
    client: SigningStargateClient,
    sender: string,
    messages: Parameters<SigningStargateClient['sign']>[1],
    fee: Parameters<SigningStargateClient['sign']>[2],
    prefix: string,
    targetBytes: number,
): Promise<Uint8Array> {
    let memoLength = Math.min(Buffer.byteLength(prefix, 'utf8'), MAX_COSMOS_MEMO_BYTES);
    let memo = fixedAsciiMemo(prefix, memoLength);
    let bytes = TxRaw.encode(await client.sign(sender, messages, fee, memo)).finish();
    for (let attempt = 0; attempt < 5 && bytes.length !== targetBytes; attempt++) {
        const nextMemoLength = Math.min(
            MAX_COSMOS_MEMO_BYTES,
            Math.max(0, memoLength + targetBytes - bytes.length),
        );
        if (nextMemoLength === memoLength) break;
        memoLength = nextMemoLength;
        memo = fixedAsciiMemo(prefix, memoLength);
        bytes = TxRaw.encode(await client.sign(sender, messages, fee, memo)).finish();
    }
    return bytes;
}

function fixedAsciiMemo(prefix: string, targetBytes: number): string {
    if (targetBytes <= 0) return '';
    const prefixBytes = Buffer.from(prefix, 'utf8').subarray(0, targetBytes);
    return Buffer.concat([
        prefixBytes,
        Buffer.alloc(Math.max(0, targetBytes - prefixBytes.length), 0x78),
    ]).toString('utf8');
}

async function cosmosClient(
    worker: Worker,
    rpcUrl: string,
    mnemonic: string,
): Promise<SigningStargateClient> {
    if (worker.cosmosClient) return worker.cosmosClient;
    const wallet = await cosmosWalletAt(mnemonic, worker.index);
    worker.cosmosClient = await SigningStargateClient.connectWithSigner(rpcUrl, wallet, {
        registry: replayRegistry(),
        broadcastPollIntervalMs: 200,
    });
    return worker.cosmosClient;
}

async function createWorkers(
    manifest: ReplayUserManifest,
    mnemonic: string,
    provider: ethers.JsonRpcProvider,
): Promise<Worker[]> {
    return Promise.all(
        manifest.users.map(async (user, slot) => {
            const wallet = new ethers.Wallet(privateKeyAt(mnemonic, user.index), provider);
            if (wallet.address.toLowerCase() !== user.evmAddress.toLowerCase()) {
                throw new Error(`Derived EVM address mismatch for user ${user.index}`);
            }
            const cosmosWallet = await cosmosWalletAt(mnemonic, user.index);
            const seiAddress = (await cosmosWallet.getAccounts())[0].address;
            if (seiAddress !== user.seiAddress) {
                throw new Error(`Derived Sei address mismatch for user ${user.index}`);
            }
            return {
                slot,
                index: user.index,
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

function inspectSegments(
    segments: ReplaySegment[],
    users: ReplayUserManifest,
    deployment: ReplayDeploymentManifest,
    chainId: bigint,
    metrics: AdapterMetrics,
): void {
    const fees = {
        gasPrice: 1_000_000_000n,
        maxFeePerGas: 2_000_000_000n,
        maxPriorityFeePerGas: 1_000_000_000n,
    };
    let sequence = 0;
    for (const segment of segments) {
        for (const block of segment.blocks) {
            for (const entry of replayEntriesForBlock(block)) {
                const source = entry.cosmos;
                const evm = entry.evm;
                metrics.offered++;
                if (evm) {
                    const built = buildEvmReplay(evm, {
                        chainId,
                        deployment,
                        users: users.users,
                        workerIndex: sequence % users.users.length,
                        sequence,
                        nonce: sequence,
                        fees,
                        maxGasPerTransaction: MAX_GAS_PER_TX,
                        maxValueWei: MAX_VALUE_WEI,
                        maxCalldataBytes: MAX_CALLDATA_BYTES,
                    });
                    recordBuilt(metrics, built.adapter, built.fidelity);
                    metrics.sourceBytes += evm.sourceSerializedBytes ?? 0;
                    metrics.sourceCalldataBytes += built.sourceCalldataBytes;
                    metrics.producedCalldataBytes += built.producedCalldataBytes;
                    if (!built.transaction) recordSkip(metrics, built.reason ?? 'EVM adapter skipped');
                } else {
                    if (!source) throw new Error('Replay schedule entry has no transaction');
                    const built = buildCosmosReplay(source, {
                        users: users.users,
                        workerIndex: sequence % users.users.length,
                        maxMessages: MAX_COSMOS_MESSAGES,
                        privilegedMode: PRIVILEGED_REPLAY_MODE,
                    });
                    recordBuilt(metrics, built.adapter, built.fidelity);
                    metrics.sourceBytes += source.transactionBytes;
                    if (!built.messages) {
                        recordSkip(metrics, built.reason ?? 'Cosmos adapter skipped');
                    }
                }
                sequence++;
            }
        }
    }
}

function scaledPeakTps(segments: ReplaySegment[]): number {
    const buckets = new Map<number, number>();
    for (const block of segments.flatMap(segment => segment.blocks)) {
        buckets.set(
            block.timestamp,
            (buckets.get(block.timestamp) ?? 0) + replayEntriesForBlock(block).length,
        );
    }
    return Math.max(0, ...buckets.values()) * TIME_SCALE;
}

function validatePeakTps(segments: ReplaySegment[]): void {
    const peak = scaledPeakTps(segments);
    if (peak > MAX_TPS) {
        throw new Error(
            `Scaled source peaks at ${peak.toFixed(1)} tx/s, above MAX_TPS=${MAX_TPS}; ` +
                'lower TIME_SCALE or explicitly raise MAX_TPS',
        );
    }
}

function validateTargetManifests(
    users: ReplayUserManifest,
    deployment: ReplayDeploymentManifest,
    network: string,
    chainId: bigint,
): void {
    if (
        users.schemaVersion !== 1 ||
        users.network !== network ||
        deployment.network !== network ||
        BigInt(users.chainId) !== chainId ||
        BigInt(deployment.chainId) !== chainId
    ) {
        throw new Error('Replay user or deployment manifest does not match the target network');
    }
    if (deployment.schemaVersion !== REPLAY_DEPLOYMENT_SCHEMA_VERSION) {
        throw new Error(
            `Replay deployment schema ${deployment.schemaVersion}; ` +
                `expected schema ${REPLAY_DEPLOYMENT_SCHEMA_VERSION}`,
        );
    }
    validateSushiV2Provenance(deployment);
    for (const name of REPLAY_V4_CONTRACT_KEYS) {
        if (!deployment.contracts[name]) throw new Error(`Deployment is missing ${name}`);
    }
}

async function verifyDeploymentCode(
    deployment: ReplayDeploymentManifest,
    provider: ethers.Provider,
): Promise<void> {
    for (const [name, address] of Object.entries(deployment.contracts)) {
        if (!address) continue;
        const code = await provider.getCode(address);
        if (code === '0x') {
            throw new Error(`Deployment ${name} has no code at ${address}`);
        }
        const expectedHash = deployment.codeHashes?.[name];
        if (expectedHash && ethers.keccak256(code) !== expectedHash) {
            throw new Error(`Deployment ${name} bytecode does not match its manifest`);
        }
    }
}

async function readFees(provider: ethers.Provider): Promise<ReplayFeeState> {
    const fee = await provider.getFeeData();
    const gasPrice = fee.gasPrice ?? 50_000_000_000n;
    return {
        gasPrice,
        maxFeePerGas: fee.maxFeePerGas ?? gasPrice * 2n,
        maxPriorityFeePerGas: fee.maxPriorityFeePerGas ?? 1_000_000_000n,
    };
}

function dryRunContext(
    network: ReplayUserManifest['network'],
    chainId: bigint,
): { users: ReplayUserManifest; deployment: ReplayDeploymentManifest } {
    const addresses = Array.from({ length: 31 }, (_, index) =>
        new ethers.Wallet(ethers.id(`dry-run-${index}`)).address,
    );
    return {
        users: {
            schemaVersion: 1,
            network,
            chainId: Number(chainId),
            users: addresses.map((address, index) => ({
                index,
                derivationPath: `dry-run/${index}`,
                seiAddress: `sei1dryrun${String(index).padStart(32, '0')}`,
                evmAddress: address,
            })),
        },
        deployment: {
            schemaVersion: REPLAY_DEPLOYMENT_SCHEMA_VERSION,
            network,
            chainId: Number(chainId),
            sushiV2: SUSHI_V2_PROVENANCE,
            contracts: {
                tokenA: addresses[0],
                tokenB: addresses[1],
                router: addresses[2],
                nft: addresses[3],
                profileHarness: addresses[4],
                callGraphHarness: addresses[5],
                callGraphNode: addresses[6],
                syntheticCreationHarness: addresses[30],
                weth: addresses[7],
                factory: addresses[8],
                pair: addresses[9],
                proxyErc20Implementation: addresses[10],
                dexOutputTokenProxy: addresses[11],
                v3Pool: addresses[12],
                v3Router: addresses[13],
                farmRewardTokenProxy: addresses[14],
                masterChef: addresses[15],
                lendingReceiptTokenProxy: addresses[16],
                lendingOracle: addresses[17],
                lendingRateModel: addresses[18],
                lendingComptroller: addresses[19],
                lendingImplementation: addresses[20],
                lendingPoolProxy: addresses[21],
                liquidStakingReceiptTokenProxy: addresses[22],
                exchangeRateOracle: addresses[23],
                liquidStakingImplementation: addresses[24],
                liquidStakingProxy: addresses[25],
                strategyModule: addresses[26],
                strategyAdapter: addresses[27],
                strategyVaultImplementation: addresses[28],
                strategyVaultProxy: addresses[29],
            },
        },
    };
}

function emptyMetrics(): AdapterMetrics {
    return {
        offered: 0,
        submitted: 0,
        included: 0,
        includedFailed: 0,
        rejected: 0,
        skipped: 0,
        sourceBytes: 0,
        producedBytes: 0,
        sourceCalldataBytes: 0,
        producedCalldataBytes: 0,
        byAdapter: {},
        byFidelity: {
            semantic: 0,
            'trace-shape': 0,
            'creation-shape': 0,
            shape: 0,
            skipped: 0,
        },
        skipReasons: {},
    };
}

function auditRecord(
    source: ReplayBlock['transactions'][number],
    sourceBlock: number,
    sequence: number,
    targetNetwork: string,
    adapter: string,
    fidelity: ReplayFidelity,
    outcome: BucketOutcome,
    reason?: string,
): BucketAuditRecord {
    const evm = source.evm;
    return {
        recordedAt: new Date().toISOString(),
        sourceNetwork: 'pacific-1',
        targetNetwork,
        sourceBlock,
        sourceCosmosHash: source.hash,
        sourceEvmHash: evm?.hash,
        lane: source.isEvm && evm ? 'evm' : 'cosmos',
        sequence,
        sourceMessageTypes: source.messages.map(message => message.typeUrl),
        sourceEvmKind: evm?.kind,
        sourceEvmType: evm?.type,
        sourceSelector: evm?.selector,
        sourceTransactionBytes: evm?.sourceSerializedBytes ?? source.transactionBytes,
        sourceCalldataBytes: evm?.inputBytes,
        adapter,
        fidelity,
        reason,
        outcome,
    };
}

function evmAuditRecord(
    evm: ReplayEvmTransaction,
    sourceCosmosHash: string | undefined,
    sourceBlock: number,
    sequence: number,
    targetNetwork: string,
    adapter: string,
    fidelity: ReplayFidelity,
    outcome: BucketOutcome,
    reason?: string,
): BucketAuditRecord {
    return {
        recordedAt: new Date().toISOString(),
        sourceNetwork: 'pacific-1',
        targetNetwork,
        sourceBlock,
        sourceCosmosHash,
        sourceEvmHash: evm.hash,
        lane: 'evm',
        sequence,
        sourceEvmKind: evm.kind,
        sourceEvmType: evm.type,
        sourceSelector: evm.selector,
        sourceTransactionBytes: evm.sourceSerializedBytes ?? 0,
        sourceCalldataBytes: evm.inputBytes,
        ...traceAuditFields(evm),
        adapter,
        fidelity,
        reason,
        outcome,
    };
}

function traceAuditFields(evm: ReplayEvmTransaction): Partial<BucketAuditRecord> {
    const counts = sourceTraceCounts(evm.trace);
    return {
        traceAvailability: evm.trace?.availability ?? 'not-captured',
        sourceFrames: counts.frames,
        sourceDelegatecalls: counts.delegatecalls,
        sourceReads: counts.reads,
        sourceWrites: counts.writes,
        sourceChangedAccounts: counts.changedAccounts,
        sourceChangedStorageSlots: counts.changedStorageSlots,
        sourceDeployedRuntimeBytes: evm.deployedRuntimeCodeBytes,
        sourceCreationMethod: evm.creationMethod,
    };
}

function recordBuilt(metrics: AdapterMetrics, adapter: string, fidelity: ReplayFidelity): void {
    increment(metrics.byAdapter, adapter);
    metrics.byFidelity[fidelity]++;
}

function recordSkip(
    metrics: AdapterMetrics,
    reason: string,
    liveMetrics?: ReplayMetrics,
    lane?: ReplayLane,
): void {
    metrics.skipped++;
    increment(metrics.skipReasons, reason);
    if (liveMetrics && lane) liveMetrics.recordSkip(lane, skipReasonLabel(reason));
}

function increment(record: Record<string, number>, key: string): void {
    record[key] = (record[key] ?? 0) + 1;
}

function printSummary(metrics: AdapterMetrics): void {
    console.log('\nReplay summary');
    console.log(
        `  offered=${metrics.offered} submitted=${metrics.submitted} included=${metrics.included} ` +
            `failed=${metrics.includedFailed} rejected=${metrics.rejected} skipped=${metrics.skipped}`,
    );
    console.log(`  fidelity=${JSON.stringify(metrics.byFidelity)}`);
    console.log(`  adapters=${JSON.stringify(metrics.byAdapter)}`);
    console.log(
        `  calldata source=${metrics.sourceCalldataBytes} produced=${metrics.producedCalldataBytes}`,
    );
    if (Object.keys(metrics.skipReasons).length > 0) {
        console.log(`  skips=${JSON.stringify(metrics.skipReasons)}`);
    }
}

async function sleepUntil(timestamp: number): Promise<void> {
    const delay = timestamp - Date.now();
    if (delay > 0) await new Promise(resolve => setTimeout(resolve, delay));
}

async function sleepBounded(milliseconds: number, deadline: number | undefined): Promise<void> {
    const bounded =
        deadline === undefined ? milliseconds : Math.min(milliseconds, Math.max(0, deadline - Date.now()));
    if (bounded > 0) await new Promise(resolve => setTimeout(resolve, bounded));
}

function remainingSeconds(deadline: number | undefined): number | undefined {
    return deadline === undefined ? undefined : (deadline - Date.now()) / 1_000;
}

function skipReasonLabel(reason: string): string {
    if (reason.includes('queue full')) return 'worker_queue_full';
    if (reason.includes('Privileged/system')) return 'privileged_system';
    if (reason.includes('calldata')) return 'calldata_limit';
    if (reason.includes('adapter error')) return 'adapter_error';
    return 'adapter_skipped';
}

main().catch(error => {
    console.error('Fatal:', error instanceof Error ? error.message : error);
    process.exit(1);
});
