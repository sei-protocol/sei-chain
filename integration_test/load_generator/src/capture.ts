/**
 * Capture a durable, contiguous Pacific-1 replay corpus.
 *
 * Defaults to the finalized blocks covering the latest 20 minutes and writes
 * them as independently validated 200-block segments.
 *
 *   npm run replay:capture
 *
 * Optional:
 *   RECORD_MINUTES=20
 *   SEGMENT_BLOCKS=200
 *   TIP_LAG_BLOCKS=10
 *   START_BLOCK=123 END_BLOCK=456
 *   REPLAY_DIR=runtime/replay/pacific-1/my-capture
 */
import fs from 'fs/promises';
import path from 'path';
import { PacificSource, segmentFilename, writeSegmentAtomic } from './replay/pacificSource';
import {
    REPLAY_SCHEMA_VERSION,
    ReplayCheckpoint,
    ReplaySegment,
} from './replay/replayTypes';
import { loadCaptureConfig } from './config';
import { readOptionalJson, writeJsonAtomic } from './io';
import { SEGMENT_FILENAME } from './replay/corpus';

const captureConfig = loadCaptureConfig();
const {
    evmRpcUrl: EVM_RPC,
    cosmosRpcUrl: COSMOS_RPC,
    recordMinutes: RECORD_MINUTES,
    segmentBlocks: SEGMENT_BLOCKS,
    tipLagBlocks: TIP_LAG_BLOCKS,
    startBlock: START_BLOCK,
    endBlock: END_BLOCK,
    captureId: CAPTURE_ID,
    replayDirectory: OUTPUT_DIRECTORY,
} = captureConfig;
const CHECKPOINT_PATH = path.join(OUTPUT_DIRECTORY, 'capture-checkpoint.json');
const MANIFEST_PATH = path.join(OUTPUT_DIRECTORY, 'capture-manifest.json');

interface CaptureManifest {
    schemaVersion: typeof REPLAY_SCHEMA_VERSION;
    captureId: string;
    createdAt: string;
    complete: boolean;
    source: {
        network: 'pacific-1';
        evmRpcUrl: string;
        cosmosRpcUrl: string;
        firstBlock: number;
        lastBlock: number;
        requestedMinutes: number;
        tipLagBlocks: number;
    };
    segmentBlocks: number;
    segmentFiles: string[];
    totals?: {
        blocks: number;
        canonicalTransactions: number;
        sourceBytes: number;
        captureElapsedSeconds: number;
    };
}

async function main(): Promise<void> {
    const source = new PacificSource({
        evmRpcUrl: EVM_RPC,
        cosmosRpcUrl: COSMOS_RPC,
        evmConcurrency: captureConfig.evmConcurrency,
        cosmosConcurrency: captureConfig.cosmosConcurrency,
        blocksPerBatch: captureConfig.blocksPerBatch,
        traceCaptureMode: captureConfig.traceCaptureMode,
        traceConcurrency: captureConfig.traceConcurrency,
        traceMaxDepth: captureConfig.traceMaxDepth,
        traceMaxFrames: captureConfig.traceMaxFrames,
        traceTimeoutMs: captureConfig.traceTimeoutMs,
        traceMaxRetries: captureConfig.traceMaxRetries,
    });
    await source.verifyChain();
    await fs.mkdir(OUTPUT_DIRECTORY, { recursive: true });
    const existingManifest = await readOptionalJson<CaptureManifest>(MANIFEST_PATH);
    if (existingManifest) validateCaptureManifest(existingManifest);
    if (existingManifest?.complete) {
        console.log(
            `Capture already complete: ${existingManifest.source.firstBlock}..` +
                `${existingManifest.source.lastBlock}`,
        );
        return;
    }

    let start: number;
    let end: number;
    if (existingManifest) {
        start = existingManifest.source.firstBlock;
        end = existingManifest.source.lastBlock;
    } else {
        const latest = await source.latestHeight();
        const safeTip = latest - TIP_LAG_BLOCKS;
        if (safeTip <= 0) throw new Error(`TIP_LAG_BLOCKS=${TIP_LAG_BLOCKS} leaves no safe blocks`);
        const requestedEnd = END_BLOCK ?? safeTip;
        if (requestedEnd > safeTip && !captureConfig.allowUnlaggedTip) {
            throw new Error(
                `END_BLOCK=${requestedEnd} is above safe tip ${safeTip}; ` +
                    'lower END_BLOCK or set ALLOW_UNLAGGED_TIP=1 explicitly',
            );
        }
        end = requestedEnd;
        const endTimestamp = await source.blockTimestamp(end);
        start =
            START_BLOCK ??
            (await source.findFirstBlockAtOrAfter(
                end,
                endTimestamp - Math.round(RECORD_MINUTES * 60),
            ));
        if (start > end) throw new Error(`START_BLOCK=${start} is above END_BLOCK=${end}`);
        await writeJsonAtomic(MANIFEST_PATH, {
            schemaVersion: REPLAY_SCHEMA_VERSION,
            captureId: CAPTURE_ID,
            createdAt: new Date().toISOString(),
            complete: false,
            source: {
                network: 'pacific-1',
                evmRpcUrl: EVM_RPC,
                cosmosRpcUrl: COSMOS_RPC,
                firstBlock: start,
                lastBlock: end,
                requestedMinutes: RECORD_MINUTES,
                tipLagBlocks: TIP_LAG_BLOCKS,
            },
            segmentBlocks: SEGMENT_BLOCKS,
            segmentFiles: [],
        } satisfies CaptureManifest);
    }

    let checkpoint = await readCheckpoint();
    let cursor = checkpoint ? Math.max(start, checkpoint.nextCollectHeight) : start;
    if (cursor > end) {
        // All segments were collected in an earlier run that stopped before the
        // completion manifest was written; fall through to finalize it now.
        console.log(
            `All blocks already collected through ${checkpoint?.lastCollectedHeight}; ` +
                'finalizing the capture manifest',
        );
    } else {
        console.log(
            `Capturing Pacific-1 ${start}..${end} (${end - start + 1} blocks) ` +
                `in segments of ${SEGMENT_BLOCKS}`,
        );
        console.log(`Output: ${OUTPUT_DIRECTORY}`);
    }

    const startedAt = Date.now();
    while (cursor <= end) {
        const segmentEnd = Math.min(end, cursor + SEGMENT_BLOCKS - 1);
        const existingPath = path.join(OUTPUT_DIRECTORY, segmentFilename(cursor, segmentEnd));
        const existing = await readExistingSegment(existingPath);
        const segment = existing ?? (await source.captureSegment(cursor, segmentEnd, TIP_LAG_BLOCKS));
        validateAgainstCheckpoint(segment, checkpoint);
        if (!existing) await writeSegmentAtomic(OUTPUT_DIRECTORY, segment);
        checkpoint = {
            schemaVersion: 1,
            sourceNetwork: 'pacific-1',
            nextCollectHeight: segmentEnd + 1,
            lastCollectedHeight: segmentEnd,
            lastCollectedEvmHash: segment.continuity.lastBlockHash,
            lastCollectedCosmosHash: segment.continuity.lastCosmosBlockHash,
            updatedAt: new Date().toISOString(),
        };
        await writeJsonAtomic(CHECKPOINT_PATH, checkpoint);
        console.log(
            `  ${cursor}..${segmentEnd}: ${segment.totals.canonicalTransactions} canonical txs, ` +
                `${segment.totals.evmTransactions} EVM`,
        );
        cursor = segmentEnd + 1;
    }

    const elapsedSeconds = Math.max(0.001, (Date.now() - startedAt) / 1_000);
    const segmentFiles = (await fs.readdir(OUTPUT_DIRECTORY))
        .filter(file => SEGMENT_FILENAME.test(file))
        .sort();
    const completedSegments = await Promise.all(
        segmentFiles.map(file => readExistingSegment(path.join(OUTPUT_DIRECTORY, file))),
    );
    const capturedTransactions = completedSegments.reduce(
        (sum, segment) => sum + (segment?.totals.canonicalTransactions ?? 0),
        0,
    );
    const capturedBytes = completedSegments.reduce(
        (sum, segment) => sum + (segment?.totals.sourceBytes ?? 0),
        0,
    );
    const manifest: CaptureManifest = {
        schemaVersion: REPLAY_SCHEMA_VERSION,
        captureId: existingManifest?.captureId ?? CAPTURE_ID,
        createdAt: existingManifest?.createdAt ?? new Date().toISOString(),
        complete: true,
        source: {
            network: 'pacific-1',
            evmRpcUrl: EVM_RPC,
            cosmosRpcUrl: COSMOS_RPC,
            firstBlock: start,
            lastBlock: end,
            requestedMinutes: RECORD_MINUTES,
            tipLagBlocks: TIP_LAG_BLOCKS,
        },
        segmentBlocks: SEGMENT_BLOCKS,
        segmentFiles,
        totals: {
            blocks: end - start + 1,
            canonicalTransactions: capturedTransactions,
            sourceBytes: capturedBytes,
            captureElapsedSeconds: elapsedSeconds,
        },
    };
    await writeJsonAtomic(MANIFEST_PATH, manifest);
    console.log(
        `Captured ${end - start + 1} blocks and ${capturedTransactions} canonical txs ` +
            `in ${elapsedSeconds.toFixed(1)}s`,
    );
}

function validateCaptureManifest(manifest: CaptureManifest): void {
    if (
        manifest.schemaVersion !== REPLAY_SCHEMA_VERSION ||
        manifest.source.network !== 'pacific-1' ||
        manifest.source.evmRpcUrl !== EVM_RPC ||
        manifest.source.cosmosRpcUrl !== COSMOS_RPC ||
        manifest.segmentBlocks !== SEGMENT_BLOCKS ||
        (START_BLOCK !== undefined && manifest.source.firstBlock !== START_BLOCK) ||
        (END_BLOCK !== undefined && manifest.source.lastBlock !== END_BLOCK)
    ) {
        throw new Error(
            `Existing capture manifest does not match the configured source, range, or segment size: ` +
                MANIFEST_PATH,
        );
    }
}

async function readCheckpoint(): Promise<ReplayCheckpoint | undefined> {
    const parsed = await readOptionalJson<ReplayCheckpoint>(CHECKPOINT_PATH);
    if (parsed && (parsed.schemaVersion !== 1 || parsed.sourceNetwork !== 'pacific-1')) {
        throw new Error(`Unsupported checkpoint ${CHECKPOINT_PATH}`);
    }
    return parsed;
}

async function readExistingSegment(file: string): Promise<ReplaySegment | undefined> {
    const segment = await readOptionalJson<ReplaySegment>(file);
    if (segment && segment.schemaVersion !== REPLAY_SCHEMA_VERSION) {
        throw new Error(`Unsupported replay segment ${file}`);
    }
    return segment;
}

function validateAgainstCheckpoint(
    segment: ReplaySegment,
    checkpoint: ReplayCheckpoint | undefined,
): void {
    if (!checkpoint) return;
    if (segment.source.firstBlock !== checkpoint.nextCollectHeight) {
        throw new Error(
            `Segment starts at ${segment.source.firstBlock}; checkpoint expects ` +
                `${checkpoint.nextCollectHeight}`,
        );
    }
    if (
        segment.continuity.firstParentHash.toLowerCase() !==
        checkpoint.lastCollectedEvmHash.toLowerCase()
    ) {
        throw new Error(`EVM continuity mismatch before block ${segment.source.firstBlock}`);
    }
    if (
        segment.continuity.firstCosmosParentHash &&
        segment.continuity.firstCosmosParentHash.toLowerCase() !==
            checkpoint.lastCollectedCosmosHash.toLowerCase()
    ) {
        throw new Error(`Cosmos continuity mismatch before block ${segment.source.firstBlock}`);
    }
}

main().catch(error => {
    console.error('Fatal:', error instanceof Error ? error.message : error);
    process.exit(1);
});
