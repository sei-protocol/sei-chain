import {
    ReplayCallFrame,
    ReplayOperationSummary,
    ReplayStateDiffSummary,
    ReplayTraceSummary,
} from './replayTypes';

export type TraceCaptureMode = 'off' | 'calls' | 'full';

export interface TraceBounds {
    maxDepth: number;
    maxFrames: number;
}

interface CallTracerFrame {
    type?: string;
    from?: string;
    to?: string;
    input?: string;
    value?: string;
    gas?: string;
    gasUsed?: string;
    error?: string;
    revertReason?: string;
    calls?: CallTracerFrame[];
}

export interface NormalizedCallTrace {
    frames: ReplayCallFrame[];
    truncated: boolean;
    sourceFrameCount: number;
}

const COUNTED_OPS = [
    'SLOAD',
    'SSTORE',
    'CALL',
    'STATICCALL',
    'DELEGATECALL',
    'CREATE',
    'CREATE2',
    'LOG0',
    'LOG1',
    'LOG2',
    'LOG3',
    'LOG4',
    'KECCAK256',
    'SHA3',
] as const;

export function normalizeCallTrace(root: unknown, bounds: TraceBounds): NormalizedCallTrace {
    const frames: ReplayCallFrame[] = [];
    let sourceFrameCount = 0;
    let truncated = false;

    function countSubtree(value: unknown): number {
        if (!value || typeof value !== 'object') return 0;
        const children = Array.isArray((value as CallTracerFrame).calls)
            ? (value as CallTracerFrame).calls!
            : [];
        return 1 + children.reduce((sum, child) => sum + countSubtree(child), 0);
    }

    function visit(value: unknown, depth: number, parent: number | null): void {
        if (!value || typeof value !== 'object') return;
        if (depth > bounds.maxDepth || frames.length >= bounds.maxFrames) {
            truncated = true;
            sourceFrameCount += countSubtree(value);
            return;
        }
        sourceFrameCount++;
        const frame = value as CallTracerFrame;
        const input = validHex(frame.input) ? frame.input! : '0x';
        const inputBytes = Math.min(0xffff_ffff, Math.max(0, Math.floor((input.length - 2) / 2)));
        const index = frames.length;
        const children = Array.isArray(frame.calls) ? frame.calls : [];
        frames.push({
            index,
            parent,
            depth,
            type: normalizeCallType(frame.type),
            selector: inputBytes >= 4 ? input.slice(0, 10).toLowerCase() : null,
            inputBytes,
            valueNonZero: nonZeroHex(frame.value),
            gas: boundedQuantity(frame.gas),
            gasUsed: boundedQuantity(frame.gasUsed),
            error: boundedText(frame.error),
            reverted: Boolean(frame.error || frame.revertReason),
            childrenTruncated:
                depth === bounds.maxDepth && children.length > 0
                    ? true
                    : undefined,
        });
        if (depth === bounds.maxDepth && children.length > 0) truncated = true;
        for (const child of children) visit(child, depth + 1, index);
    }

    visit(root, 0, null);
    return { frames, truncated, sourceFrameCount };
}

export interface SourceTraceCounts {
    frames: number;
    delegatecalls: number;
    reads: number;
    writes: number;
    changedAccounts: number;
    changedStorageSlots: number;
}

/** Bounded source-trace counts shared by the bucket audit and live metrics. */
export function sourceTraceCounts(trace: ReplayTraceSummary | undefined): SourceTraceCounts {
    return {
        frames: trace?.calls?.sourceFrameCount ?? 0,
        delegatecalls:
            trace?.operations?.delegatecall ??
            trace?.calls?.frames.filter(frame => frame.type === 'DELEGATECALL').length ??
            0,
        reads: trace?.operations?.sload ?? 0,
        writes: trace?.operations?.sstore ?? 0,
        changedAccounts: trace?.stateDiff?.changedAccounts ?? 0,
        changedStorageSlots: trace?.stateDiff?.changedStorageSlots ?? 0,
    };
}

export function summarizeStructLogs(value: unknown): ReplayOperationSummary {
    const counts: Record<string, number> = Object.fromEntries(COUNTED_OPS.map(op => [op, 0]));
    const logs =
        value && typeof value === 'object' && Array.isArray((value as { structLogs?: unknown[] }).structLogs)
            ? (value as { structLogs: unknown[] }).structLogs
            : [];
    for (const item of logs) {
        const op =
            item && typeof item === 'object' && typeof (item as { op?: unknown }).op === 'string'
                ? (item as { op: string }).op.toUpperCase()
                : '';
        if (op in counts) counts[op]++;
    }
    // Clients use either spelling for opcode 0x20; expose one stable aggregate.
    counts.KECCAK256 += counts.SHA3;
    delete counts.SHA3;
    return {
        steps: boundedCount(logs.length),
        sload: boundedCount(counts.SLOAD),
        sstore: boundedCount(counts.SSTORE),
        call: boundedCount(counts.CALL),
        staticcall: boundedCount(counts.STATICCALL),
        delegatecall: boundedCount(counts.DELEGATECALL),
        create: boundedCount(counts.CREATE),
        create2: boundedCount(counts.CREATE2),
        logs: boundedCount(counts.LOG0 + counts.LOG1 + counts.LOG2 + counts.LOG3 + counts.LOG4),
        log0: boundedCount(counts.LOG0),
        log1: boundedCount(counts.LOG1),
        log2: boundedCount(counts.LOG2),
        log3: boundedCount(counts.LOG3),
        log4: boundedCount(counts.LOG4),
        keccak256: boundedCount(counts.KECCAK256),
    };
}

export function summarizePrestateDiff(value: unknown): ReplayStateDiffSummary {
    const root = value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
    const pre = object(root.pre);
    const post = object(root.post);
    const addresses = new Set([...Object.keys(pre), ...Object.keys(post)]);
    let changedAccounts = 0;
    let changedStorageSlots = 0;
    let code = 0;
    let balance = 0;
    let nonce = 0;
    for (const address of addresses) {
        const before = object(pre[address]);
        const after = object(post[address]);
        if (JSON.stringify(before) === JSON.stringify(after)) continue;
        changedAccounts++;
        const beforeStorage = object(before.storage);
        const afterStorage = object(after.storage);
        for (const slot of new Set([
            ...Object.keys(beforeStorage),
            ...Object.keys(afterStorage),
        ])) {
            if (beforeStorage[slot] !== afterStorage[slot]) changedStorageSlots++;
        }
        if (before.code !== after.code) code++;
        if (before.balance !== after.balance) balance++;
        if (before.nonce !== after.nonce) nonce++;
    }
    return {
        changedAccounts: boundedCount(changedAccounts),
        changedStorageSlots: boundedCount(changedStorageSlots),
        code: boundedCount(code),
        balance: boundedCount(balance),
        nonce: boundedCount(nonce),
    };
}

function normalizeCallType(value: unknown): ReplayCallFrame['type'] {
    const type = typeof value === 'string' ? value.toUpperCase() : 'CALL';
    if (type === 'STATICCALL' || type === 'DELEGATECALL' || type === 'CREATE' || type === 'CREATE2') {
        return type;
    }
    return 'CALL';
}

function boundedQuantity(value: unknown): string | undefined {
    if (
        typeof value !== 'string' ||
        value.length > 66 ||
        !/^0x[0-9a-f]*$/i.test(value)
    ) return undefined;
    try {
        return BigInt(value || '0x0').toString();
    } catch {
        return undefined;
    }
}

function boundedText(value: unknown): string | undefined {
    return typeof value === 'string' && value ? value.slice(0, 256) : undefined;
}

function validHex(value: unknown): value is string {
    return typeof value === 'string' && /^0x(?:[0-9a-f]{2})*$/i.test(value);
}

function nonZeroHex(value: unknown): boolean {
    try {
        return typeof value === 'string' && BigInt(value) !== 0n;
    } catch {
        return false;
    }
}

function object(value: unknown): Record<string, unknown> {
    return value && typeof value === 'object' && !Array.isArray(value)
        ? (value as Record<string, unknown>)
        : {};
}

function boundedCount(value: number): number {
    return Math.min(0xffff_ffff, Math.max(0, Math.trunc(value)));
}
