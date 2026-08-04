/**
 * Maintain an initial Pacific history buffer while replaying and continuously
 * collecting new contiguous segments.
 *
 * Dry-run one planned batch:
 *   REPLAY_DIR=runtime/replay/pacific-1/pacific-1-20m npm run replay:buffered
 *
 * Continuous execution:
 *   TARGET_NETWORK=arctic-1 REPLAY_DIR=... EXECUTE=1 npm run replay:buffered
 */
import fs from 'fs/promises';
import path from 'path';
import { ChildProcess, spawn } from 'child_process';
import { PacificSource, writeSegmentAtomic } from './replay/pacificSource';
import { ReplayCheckpoint, ReplaySegment } from './replay/replayTypes';
import { loadBufferedConfig } from './config';
import { writeJsonAtomic } from './io';
import {
    readReplaySegments,
    removeAllReplaySegments,
    validateReplaySegments,
} from './replay/corpus';

const bufferedConfig = loadBufferedConfig();
const {
    execute: EXECUTE,
    replayDirectory: REPLAY_DIRECTORY,
    startMode: BUFFER_START_MODE,
    initialBufferBlocks: INITIAL_BUFFER_BLOCKS,
    minBufferMinutes: MIN_BUFFER_MINUTES,
    resumeBufferMinutes: RESUME_BUFFER_MINUTES,
    segmentBlocks: SEGMENT_BLOCKS,
    replayBatchSegments: REPLAY_BATCH_SEGMENTS,
    tipLagBlocks: TIP_LAG_BLOCKS,
    sourcePollMs: SOURCE_POLL_MS,
    runDurationSeconds: RUN_DURATION_SECONDS,
    timeScale: TIME_SCALE,
    evmRpcUrl: EVM_RPC,
    cosmosRpcUrl: COSMOS_RPC,
    cleanupConsumedSegments: CLEANUP_CONSUMED_SEGMENTS,
} = bufferedConfig;
const CAPTURE_CHECKPOINT = path.join(REPLAY_DIRECTORY, 'capture-checkpoint.json');

let stopping = false;
let currentChild: ChildProcess | undefined;
const segmentCache = new Map<string, ReplaySegment>();

async function main(): Promise<void> {
    if (TIME_SCALE > 1 && !bufferedConfig.allowBufferDrain) {
        throw new Error(
            'Buffered continuous replay cannot sustain TIME_SCALE > 1; ' +
                'set TIME_SCALE<=1 or ALLOW_BUFFER_DRAIN=1 explicitly',
        );
    }
    if (MIN_BUFFER_MINUTES >= RESUME_BUFFER_MINUTES) {
        throw new Error('MIN_BUFFER_MINUTES must be below RESUME_BUFFER_MINUTES');
    }

    const source = new PacificSource({
        evmRpcUrl: EVM_RPC,
        cosmosRpcUrl: COSMOS_RPC,
        evmConcurrency: bufferedConfig.evmConcurrency,
        cosmosConcurrency: bufferedConfig.cosmosConcurrency,
        blocksPerBatch: bufferedConfig.blocksPerBatch,
        traceCaptureMode: bufferedConfig.traceCaptureMode,
        traceConcurrency: bufferedConfig.traceConcurrency,
        traceMaxDepth: bufferedConfig.traceMaxDepth,
        traceMaxFrames: bufferedConfig.traceMaxFrames,
        traceTimeoutMs: bufferedConfig.traceTimeoutMs,
        traceMaxRetries: bufferedConfig.traceMaxRetries,
    });
    await source.verifyChain();
    await ensureInitialBuffer(source);
    const initialSegments = await readReplaySegments(REPLAY_DIRECTORY, true, segmentCache);
    validateReplaySegments(initialSegments);
    const initialBuffer = availableBufferSeconds(initialSegments, undefined);
    if (initialSegments.length === 0) throw new Error('Initial replay buffer is empty');
    console.log(
        `Pacific replay buffer ready: ${initialSegments.reduce(
            (sum, segment) => sum + segment.source.blockCount,
            0,
        )} blocks (${(initialBuffer / 60).toFixed(1)} minutes), ` +
            `${initialSegments.length} segment(s)`,
    );

    if (!EXECUTE) {
        const through = initialSegments[
            Math.min(REPLAY_BATCH_SEGMENTS, initialSegments.length) - 1
        ].source.lastBlock;
        console.log(
            `Dry-run: inspect first ${Math.min(REPLAY_BATCH_SEGMENTS, initialSegments.length)} ` +
                `segments through block ${through}. Set EXECUTE=1 for continuous collection and replay.`,
        );
        await runReplayBatch(through);
        return;
    }

    const stop = (signal: string) => {
        if (stopping) return;
        console.log(`Received ${signal}; stopping buffered replay...`);
        stopping = true;
        currentChild?.kill('SIGTERM');
    };
    process.once('SIGINT', () => stop('SIGINT'));
    process.once('SIGTERM', () => stop('SIGTERM'));

    const collector = collectContinuously(source);
    try {
        console.log(
            `Starting one persistent replay process for ${RUN_DURATION_SECONDS}s; ` +
                `metrics remain available across all appended segments`,
        );
        await runContinuousReplay();
    } finally {
        stopping = true;
        await collector;
    }
    console.log('Bounded buffered replay completed');
}

async function ensureInitialBuffer(source: PacificSource): Promise<void> {
    const existing = await readReplaySegments(REPLAY_DIRECTORY, true);
    if (BUFFER_START_MODE === 'resume' && existing.length > 0) {
        const nextHeight = existing.at(-1)!.source.lastBlock + 1;
        try {
            await source.blockTimestamp(nextHeight);
        } catch (error) {
            if (isPrunedError(error)) {
                console.warn(
                    `Existing cursor ${nextHeight} is pruned; starting from the latest safe window`,
                );
                await archiveExistingCorpus();
                await captureLatestWindow(source);
                return;
            }
            console.warn(
                `Could not preflight resume height ${nextHeight}; collector will retry it: ` +
                    `${error instanceof Error ? error.message : error}`,
            );
        }
        console.log(
            `Resuming buffered capture after block ${existing.at(-1)!.source.lastBlock}`,
        );
        return;
    }
    if (BUFFER_START_MODE === 'latest') {
        await archiveExistingCorpus();
    }
    await captureLatestWindow(source);
}

async function captureLatestWindow(source: PacificSource): Promise<void> {
    await fs.mkdir(REPLAY_DIRECTORY, { recursive: true });
    const safeTip = (await source.latestHeight()) - TIP_LAG_BLOCKS;
    const start = Math.max(1, safeTip - INITIAL_BUFFER_BLOCKS + 1);
    console.log(
        `Recording latest safe Pacific window ${start}..${safeTip} ` +
            `(${safeTip - start + 1} blocks)...`,
    );
    await spawnNpm('replay:capture', {
        SEGMENT_BLOCKS: String(SEGMENT_BLOCKS),
        TIP_LAG_BLOCKS: String(TIP_LAG_BLOCKS),
        START_BLOCK: String(start),
        END_BLOCK: String(safeTip),
        REPLAY_DIR: REPLAY_DIRECTORY,
    });
}

async function archiveExistingCorpus(): Promise<void> {
    try {
        const entries = await fs.readdir(REPLAY_DIRECTORY);
        if (entries.length === 0) return;
        const suffix = new Date().toISOString().replace(/[-:.]/g, '');
        const archive = `${REPLAY_DIRECTORY}-archive-${suffix}`;
        await fs.rename(REPLAY_DIRECTORY, archive);
        const removed = CLEANUP_CONSUMED_SEGMENTS
            ? await removeAllReplaySegments(archive)
            : [];
        console.log(
            `Archived previous replay artifacts to ${archive}` +
                (removed.length > 0 ? `; removed ${removed.length} captured segment(s)` : ''),
        );
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
    }
}

async function collectContinuously(source: PacificSource): Promise<void> {
    let failures = 0;
    while (!stopping) {
        try {
            const segments = await readReplaySegments(REPLAY_DIRECTORY, true, segmentCache);
            if (segments.length === 0) throw new Error('Initial replay buffer disappeared');
            validateReplaySegments(segments);
            const previous = segments[segments.length - 1];
            const start = previous.source.lastBlock + 1;
            const safeTip = (await source.latestHeight()) - TIP_LAG_BLOCKS;
            if (safeTip < start) {
                await sleep(SOURCE_POLL_MS);
                continue;
            }
            const end = Math.min(start + SEGMENT_BLOCKS - 1, safeTip);

            const segment = await source.captureSegment(start, end, TIP_LAG_BLOCKS);
            if (
                segment.continuity.firstParentHash.toLowerCase() !==
                    previous.continuity.lastBlockHash.toLowerCase() ||
                (segment.continuity.firstCosmosParentHash &&
                    segment.continuity.firstCosmosParentHash.toLowerCase() !==
                        previous.continuity.lastCosmosBlockHash.toLowerCase())
            ) {
                throw new Error(`Source continuity mismatch before block ${start}`);
            }
            await writeSegmentAtomic(REPLAY_DIRECTORY, segment);
            const checkpoint: ReplayCheckpoint = {
                schemaVersion: 1,
                sourceNetwork: 'pacific-1',
                nextCollectHeight: end + 1,
                lastCollectedHeight: end,
                lastCollectedEvmHash: segment.continuity.lastBlockHash,
                lastCollectedCosmosHash: segment.continuity.lastCosmosBlockHash,
                updatedAt: new Date().toISOString(),
            };
            await writeJsonAtomic(CAPTURE_CHECKPOINT, checkpoint);
            failures = 0;
            console.log(
                `Collected ${start}..${end}; ${segment.totals.canonicalTransactions} canonical txs`,
            );
        } catch (error) {
            if (isPrunedError(error)) {
                console.error(
                    `Buffered source cursor was pruned while running: ` +
                        `${error instanceof Error ? error.message : error}. ` +
                        `Restarting will create a fresh latest window.`,
                );
                stopping = true;
                currentChild?.kill('SIGTERM');
                return;
            }
            failures++;
            const delay = Math.min(30_000, 1_000 * 2 ** Math.min(failures, 5));
            console.error(
                `Continuous collection attempt ${failures} failed: ` +
                    `${error instanceof Error ? error.message : error}; retrying in ${delay}ms`,
            );
            await sleep(delay);
        }
    }
}

function isPrunedError(error: unknown): boolean {
    const message = error instanceof Error ? error.message : String(error);
    return /requested height .* pruned|earliest available/i.test(message);
}

async function runReplayBatch(throughBlock: number): Promise<void> {
    await spawnNpm('replay:run', {
        REPLAY_DIR: REPLAY_DIRECTORY,
        REPLAY_THROUGH_BLOCK: String(throughBlock),
        SKIP_FIXTURE_PREPARE: '0',
    });
}

async function runContinuousReplay(): Promise<void> {
    await spawnNpm('replay:run', {
        REPLAY_DIR: REPLAY_DIRECTORY,
        FOLLOW_SEGMENTS: '1',
        RUN_DURATION_SECONDS: String(RUN_DURATION_SECONDS),
        MIN_BUFFER_MINUTES: String(MIN_BUFFER_MINUTES),
        RESUME_BUFFER_MINUTES: String(RESUME_BUFFER_MINUTES),
        FOLLOW_POLL_MS: String(SOURCE_POLL_MS),
        LIVE_REPLAY: BUFFER_START_MODE === 'latest' ? '1' : '0',
        CLEANUP_CONSUMED_SEGMENTS: CLEANUP_CONSUMED_SEGMENTS ? '1' : '0',
    });
}

async function spawnNpm(script: string, extraEnvironment: Record<string, string>): Promise<void> {
    await new Promise<void>((resolve, reject) => {
        const child = spawn('npm', ['run', script], {
            cwd: process.cwd(),
            env: { ...process.env, ...extraEnvironment },
            stdio: 'inherit',
        });
        currentChild = child;
        child.once('error', reject);
        child.once('exit', (code, signal) => {
            currentChild = undefined;
            if (code === 0) resolve();
            else reject(new Error(`${script} exited with ${code ?? signal}`));
        });
    });
}

function availableBufferSeconds(
    segments: ReplaySegment[],
    lastCompletedBlock: number | undefined,
): number {
    const blocks = segments.flatMap(segment => segment.blocks);
    const firstPending = lastCompletedBlock
        ? blocks.find(block => block.number > lastCompletedBlock)
        : blocks[0];
    const last = blocks[blocks.length - 1];
    if (!firstPending || !last) return 0;
    return Math.max(0, last.timestamp - firstPending.timestamp + 1);
}

async function sleep(milliseconds: number): Promise<void> {
    await new Promise(resolve => setTimeout(resolve, milliseconds));
}

main().catch(error => {
    stopping = true;
    currentChild?.kill('SIGTERM');
    console.error('Fatal:', error instanceof Error ? error.message : error);
    process.exit(1);
});
