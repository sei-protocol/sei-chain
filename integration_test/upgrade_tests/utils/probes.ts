/**
 * The observations both phases make, defined once so the recording run and the
 * verifying run cannot drift apart.
 *
 * Every probe collapses its answer to a short token rather than raw output.
 * Incidental variation — heights, gas figures, hashes inside error strings —
 * would otherwise make two equivalent answers compare unequal. An unexpected
 * answer records the raw text instead, so a mismatch both fails and says what
 * came back.
 *
 * Probes are split by what the upgrade is allowed to do to them:
 *
 *   invariant  the answer must be identical before and after, and a difference
 *              fails the run. Reserved for surfaces the upgrade does not claim
 *              to touch, so that a failure is unambiguous.
 *
 *   transition the answer is expected to change. Recorded on both sides and
 *              printed as a before/after diff. The changes the upgrade is
 *              specified to make get their own named assertions in the verify
 *              phase; the rest are reported so a human can see them.
 *
 * On classification: a live network runs a released binary, not this checkout.
 * arctic-1 is on v6.6.1, where the oracle handlers still serve data and the
 * upgrade precompile is not registered. So the oracle surfaces are transitions
 * here, not invariants — the deprecation lands *with* the upgrade from that
 * network's point of view. Asserting head-of-tree behaviour as an invariant
 * would fail the run for the wrong reason.
 */
import { ethers } from 'ethers';
import { expectedRemovedModules } from '../config/targets';
import { ADDRESSES, precompileInterface, rawSei } from './chain';
import { appliedPlanHeight, moduleVersionToken, queryToken } from './cosmos';

export type ProbeKind = 'invariant' | 'transition';

export interface Probe {
    name: string;
    kind: ProbeKind;
    /** Why this probe exists, carried into the artifact for whoever reads it later. */
    describes: string;
    run: () => Promise<string>;
}

const BANK_PRECOMPILE = '0x0000000000000000000000000000000000001001';

const truncate = (text: string, max = 160): string =>
    text.length <= max ? text : `${text.slice(0, max)}…`;

/**
 * Revert reasons the suite recognises, collapsed to a stable token.
 *
 * Applied by revertToken to every call the suite makes, rather than passed in at
 * each call site. When the record phase and the verify phase normalise
 * differently the run reports a change that did not happen: recording
 * `revert:oracle-retired` and re-reading
 * `revert:raw(execution reverted: oracle precompile is retired…)` is the same
 * answer twice. One table every path goes through is what makes that
 * impossible rather than merely unlikely.
 */
const KNOWN_REVERTS: Record<string, RegExp> = {
    'oracle-retired': /oracle precompile is retired/,
};

/** A JSON-RPC error message reduced to a recognised token, or its raw text. */
function revertToken(message: string): string {
    for (const [token, pattern] of Object.entries(KNOWN_REVERTS)) {
        if (pattern.test(message)) return `revert:${token}`;
    }
    return `revert:raw(${truncate(message)})`;
}

/** An eth_call at a block tag, reduced to `ok:<hex>` or `revert:<token>`. */
async function callToken(to: string, data: string, blockTag = 'latest'): Promise<string> {
    const envelope = await rawSei<string>('eth_call', [{ to, data }, blockTag]);
    return envelope.error ? revertToken(envelope.error.message) : `ok:${envelope.result ?? ''}`;
}

/**
 * Whether an address is a registered precompile. A registered one rejects an
 * unknown selector; an address with no code answers a call with empty data.
 * Verified live on arctic-1's v6.6.1: bank (0x…1001) reverts, while 0x…1010 and
 * 0x…1015 both return 0x.
 */
async function precompileRegistrationToken(address: string, blockTag = 'latest'): Promise<string> {
    const value = await callToken(address, '0xdeadbeef', blockTag);
    return value === 'ok:0x' || value === 'ok:' ? 'unregistered' : 'registered';
}

const oracleIface = () => precompileInterface('oracle');

export function probes(planName: string): Probe[] {
    const removed = expectedRemovedModules();
    return [
        // --- surfaces the upgrade does not claim to touch ---
        {
            name: 'bank.precompileRegistered',
            kind: 'invariant',
            describes:
                'a precompile the upgrade does not touch, so a failure here means the probe ' +
                'broke rather than the chain changing',
            run: () => precompileRegistrationToken(BANK_PRECOMPILE),
        },
        {
            name: 'cosmos.moduleVersion.bank',
            kind: 'invariant',
            describes: 'bank keeps its module version across the upgrade',
            run: () => moduleVersionToken('bank'),
        },
        {
            name: 'cosmos.moduleVersion.oracle',
            kind: 'invariant',
            describes:
                'oracle is deprecated but not removed, so it must keep its module version; ' +
                'losing one would mean the deprecation went further than intended',
            run: () => moduleVersionToken('oracle'),
        },

        // --- what the upgrade is for: one probe per module it removes ---
        ...removed.map(
            (module): Probe => ({
                name: `cosmos.moduleVersion.${module}`,
                kind: 'transition',
                describes: `${module} holds a module version until the upgrade deletes it`,
                run: () => moduleVersionToken(module),
            }),
        ),
        {
            name: `cosmos.appliedPlan.${planName}`,
            kind: 'transition',
            describes: 'zero until the upgrade runs, then the height it ran at',
            run: async () => String(await appliedPlanHeight(planName)),
        },

        // --- oracle deprecation, which reaches a released network with this upgrade ---
        {
            name: 'cosmos.query.oracleParams',
            kind: 'transition',
            describes:
                'the oracle query handlers serve data until they are deprecated, after which ' +
                'they answer "oracle module is deprecated"',
            run: () => queryToken('/sei-protocol/sei-chain/oracle/params'),
        },
        {
            name: 'cosmos.query.oracleExchangeRates',
            kind: 'transition',
            describes: 'the same, for the exchange rate query',
            run: () => queryToken('/sei-protocol/sei-chain/oracle/denoms/exchange_rates'),
        },
        {
            name: 'oracle.precompile.getExchangeRates',
            kind: 'transition',
            describes:
                'the oracle precompile’s revert reason. Recorded rather than asserted because ' +
                'public endpoints do not always forward revert data',
            run: () =>
                callToken(ADDRESSES.oracle, oracleIface().encodeFunctionData('getExchangeRates', [])),
        },
        {
            name: 'feegrant.precompileRegistered',
            kind: 'transition',
            describes:
                'the feegrant precompile is removed, so this must read unregistered afterwards',
            run: () => precompileRegistrationToken(ADDRESSES.feegrant),
        },
        {
            name: 'upgrade.precompileRegistered',
            kind: 'transition',
            describes:
                'whether the upgrade precompile (0x…1015) is mounted. Informational: the suite ' +
                'reads the upgrade module over Cosmos REST precisely because this is not ' +
                'registered on every released binary',
            run: () => precompileRegistrationToken(ADDRESSES.upgrade),
        },
    ];
}

export interface ProbeResult {
    name: string;
    kind: ProbeKind;
    describes: string;
    value: string;
}

export async function runProbes(planName: string): Promise<ProbeResult[]> {
    const results: ProbeResult[] = [];
    for (const probe of probes(planName)) {
        results.push({
            name: probe.name,
            kind: probe.kind,
            describes: probe.describes,
            value: await probe.run(),
        });
    }
    return results;
}

/** The EVM block height the observation was taken at. */
export async function currentHeight(): Promise<number> {
    const envelope = await rawSei<string>('eth_blockNumber', []);
    if (envelope.error || !envelope.result) {
        throw new Error(`eth_blockNumber failed: ${envelope.error?.message ?? 'empty'}`);
    }
    return Number(BigInt(envelope.result));
}

/**
 * A read pinned to the height it was made at, so the verify phase can ask the
 * same question of the same block after the upgrade. If removing state broke
 * historical reads, this is where it shows.
 */
export interface ArchiveRead {
    label: string;
    blockNumber: number;
    /** `ok:<hex>`, `revert:<token>`, or a registration token. */
    value: string;
}

/**
 * The archive reads the suite takes, keyed by label.
 *
 * The verify phase re-runs a recorded read by looking its label up here rather
 * than replaying calldata out of the artifact, so both phases encode and
 * normalise through the same code. An artifact naming a label this version no
 * longer defines is reported instead of compared.
 */
const ARCHIVE_READS: Record<string, (blockTag: string) => Promise<string>> = {
    'oracle.precompile.getExchangeRates': blockTag =>
        callToken(ADDRESSES.oracle, oracleIface().encodeFunctionData('getExchangeRates', []), blockTag),
    'bank.precompileRegistered': blockTag => precompileRegistrationToken(BANK_PRECOMPILE, blockTag),
};

/** The labels takeArchiveReads produces, which reReadAtHeight must all know. */
export function archiveReadLabels(): string[] {
    return Object.keys(ARCHIVE_READS);
}

export function knowsArchiveRead(label: string): boolean {
    return label in ARCHIVE_READS;
}

export async function takeArchiveReads(blockNumber: number): Promise<ArchiveRead[]> {
    const blockTag = ethers.toQuantity(blockNumber);
    const reads: ArchiveRead[] = [];
    for (const [label, read] of Object.entries(ARCHIVE_READS)) {
        reads.push({ label, blockNumber, value: await read(blockTag) });
    }
    return reads;
}

export type ReReadOutcome = { value: string } | { unknownLabel: true };

export async function reReadAtHeight(recorded: ArchiveRead): Promise<ReReadOutcome> {
    const read = ARCHIVE_READS[recorded.label];
    if (!read) return { unknownLabel: true };
    return { value: await read(ethers.toQuantity(recorded.blockNumber)) };
}

/**
 * A pruned or unavailable historical block, which is a different outcome from a
 * changed answer. Shared endpoints prune, and treating that as a failure would
 * make the suite unusable against them.
 */
export function isHistoryUnavailable(value: string): boolean {
    return /pruned|not available|failed to load state|is not available|header not found/i.test(
        value,
    );
}

/** A before/after line for every probe whose answer moved. */
export function diffProbes(
    before: ProbeResult[],
    after: ProbeResult[],
): Array<{ name: string; kind: ProbeKind; from: string; to: string }> {
    const byName = new Map(before.map(p => [p.name, p]));
    const changed: Array<{ name: string; kind: ProbeKind; from: string; to: string }> = [];
    for (const probe of after) {
        const previous = byName.get(probe.name);
        if (previous && previous.value !== probe.value) {
            changed.push({
                name: probe.name,
                kind: probe.kind,
                from: previous.value,
                to: probe.value,
            });
        }
    }
    return changed;
}
