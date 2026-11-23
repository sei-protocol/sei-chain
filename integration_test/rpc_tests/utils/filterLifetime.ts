import { ethers } from 'ethers';
import { rawSei, seiRpc } from './chainUtils';
import { FILTER_LIMITS } from './constants';

/**
 * Timing of the filter_timeout sweep. cleanupLoop (evmrpc/filter.go) sweeps every timeout / 2 and
 * removes a filter idle for longer than timeout, so an unread filter is gone within 1.5 x timeout.
 */
export const FILTER_LIFETIME = {
    sweptByMs: FILTER_LIMITS.timeoutMs * 1.5 + 10_000,
    readEveryMs: FILTER_LIMITS.timeoutMs / 3,
} as const;

/** Two log filters installed when the run starts: one never read, one read every readEveryMs. */
export interface FilterLifetimeProbe {
    idle: string;
    kept: string;
    /** The criteria both filters were installed with. */
    criteria: { fromBlock: string; toBlock: string };
    startedAt: number;
    /** Reads of the kept filter that failed for any reason but load shedding. */
    failedReads: string[];
}

let probe: Promise<FilterLifetimeProbe> | undefined;
let reader: NodeJS.Timeout | undefined;

/** The probe started by the root beforeAll hook; it rejects if the filters could not be installed. */
export function filterLifetimeProbe(): Promise<FilterLifetimeProbe> {
    if (!probe) {
        throw new Error('filterLifetimeProbe: the root hooks in utils/filterLifetime.ts are not loaded');
    }
    return probe;
}

async function startProbe(): Promise<FilterLifetimeProbe> {
    const sei = seiRpc();
    const head = ethers.toQuantity(await sei.getBlockNumber());
    const criteria = { fromBlock: head, toBlock: head };
    const [idle, kept] = await Promise.all([
        sei.send('eth_newFilter', [criteria]),
        sei.send('eth_newFilter', [criteria]),
    ]);
    const started: FilterLifetimeProbe = { idle, kept, criteria, startedAt: Date.now(), failedReads: [] };
    reader = setInterval(async () => {
        const res = await rawSei('eth_getFilterLogs', [kept]).catch((e: Error) => ({ error: { code: 0, message: e.message } }));
        // A read refused for load (-32005) still refreshes the filter's access time.
        if (res.error && res.error.code !== -32005) {
            started.failedReads.push(`${new Date().toISOString()}: ${res.error.code} ${res.error.message}`);
        }
    }, FILTER_LIFETIME.readEveryMs);
    reader.unref();
    return started;
}

/**
 * Mocha root hooks, loaded through .mocharc.run.json, that install the probe filters when the run
 * starts. The wait for the sweep then overlaps the rest of the suite instead of adding to it.
 */
export const mochaHooks = {
    beforeAll(): void {
        probe = startProbe();
        // Surfaced by the lifetime test rather than here, so a failure does not abort the run.
        probe.catch(() => undefined);
    },
    afterAll(): void {
        clearInterval(reader);
    },
};
