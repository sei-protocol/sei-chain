import fs from 'node:fs/promises';
import path from 'node:path';
import { REPLAY_SCHEMA_VERSION, ReplaySegment } from './replayTypes';

export const SEGMENT_FILENAME = /^pacific-1-(\d{10})-(\d{10})\.json$/;

/**
 * Segment files are immutable once atomically renamed into place, so pollers
 * can pass a cache keyed by filename to avoid re-parsing the whole corpus on
 * every poll. Entries for deleted files are evicted.
 */
export async function readReplaySegments(
    directory: string,
    allowMissing = false,
    cache?: Map<string, ReplaySegment>,
): Promise<ReplaySegment[]> {
    let files: string[];
    try {
        files = (await fs.readdir(directory)).filter(file => SEGMENT_FILENAME.test(file)).sort();
    } catch (error) {
        if (allowMissing && (error as NodeJS.ErrnoException).code === 'ENOENT') return [];
        throw error;
    }
    const loaded = await Promise.all(
        files.map(async file => {
            const cached = cache?.get(file);
            if (cached) return { file, segment: cached };
            try {
                const parsed = JSON.parse(
                    await fs.readFile(path.join(directory, file), 'utf8'),
                ) as ReplaySegment;
                cache?.set(file, parsed);
                return { file, segment: parsed };
            } catch (error) {
                if (allowMissing && (error as NodeJS.ErrnoException).code === 'ENOENT') {
                    cache?.delete(file);
                    return undefined;
                }
                throw error;
            }
        }),
    );
    const present = new Set(loaded.flatMap(item => item?.file ?? []));
    if (cache) {
        for (const key of [...cache.keys()]) {
            if (!present.has(key)) cache.delete(key);
        }
    }
    return loaded.flatMap(item => item?.segment ?? []);
}

export async function cleanupConsumedReplaySegments(
    directory: string,
    completedThrough: number,
    retainCompletedSegments = 1,
): Promise<string[]> {
    const files = await listSegmentFiles(directory);
    const completed = files.filter(file => file.lastBlock <= completedThrough);
    const removable = completed.slice(0, Math.max(0, completed.length - retainCompletedSegments));
    await Promise.all(removable.map(file => unlinkIfPresent(path.join(directory, file.name))));
    return removable.map(file => file.name);
}

export async function removeAllReplaySegments(directory: string): Promise<string[]> {
    const files = await listSegmentFiles(directory);
    await Promise.all(files.map(file => unlinkIfPresent(path.join(directory, file.name))));
    return files.map(file => file.name);
}

async function listSegmentFiles(
    directory: string,
): Promise<Array<{ name: string; firstBlock: number; lastBlock: number }>> {
    const entries = (await fs.readdir(directory)).sort();
    return entries.flatMap(name => {
        const match = SEGMENT_FILENAME.exec(name);
        if (!match) return [];
        return [
            {
                name,
                firstBlock: Number(match[1]),
                lastBlock: Number(match[2]),
            },
        ];
    });
}

async function unlinkIfPresent(file: string): Promise<void> {
    try {
        await fs.unlink(file);
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
    }
}

export function validateReplaySegments(segments: ReplaySegment[]): void {
    let previous: ReplaySegment | undefined;
    for (const segment of segments) {
        if (
            segment.schemaVersion !== REPLAY_SCHEMA_VERSION ||
            segment.source.network !== 'pacific-1'
        ) {
            throw new Error(`Unsupported replay segment at block ${segment.source.firstBlock}`);
        }
        if (segment.blocks.length !== segment.source.blockCount) {
            throw new Error(`Segment ${segment.source.firstBlock} has incomplete block data`);
        }
        if (previous) {
            if (segment.source.firstBlock !== previous.source.lastBlock + 1) {
                throw new Error(`Gap before segment ${segment.source.firstBlock}`);
            }
            if (
                segment.continuity.firstParentHash.toLowerCase() !==
                previous.continuity.lastBlockHash.toLowerCase()
            ) {
                throw new Error(`EVM continuity mismatch before ${segment.source.firstBlock}`);
            }
            if (
                segment.continuity.firstCosmosParentHash &&
                segment.continuity.firstCosmosParentHash.toLowerCase() !==
                    previous.continuity.lastCosmosBlockHash.toLowerCase()
            ) {
                throw new Error(`Cosmos continuity mismatch before ${segment.source.firstBlock}`);
            }
        }
        previous = segment;
    }
}
