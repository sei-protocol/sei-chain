import { LoadOperation } from './types';

export function applyOperationWeights(
    operations: LoadOperation[],
    overrides: Record<string, number>,
): LoadOperation[] {
    const names = new Set(operations.map(operation => operation.name));
    for (const name of Object.keys(overrides)) {
        if (!names.has(name)) throw new Error(`LOAD_MIX references unknown operation ${name}`);
    }
    if (Object.keys(overrides).length === 0) return operations;
    return operations.flatMap(operation => {
        const weight = overrides[operation.name];
        return weight === undefined ? [] : [{ ...operation, weight }];
    });
}

export function chooseOperation(operations: LoadOperation[], random: number): LoadOperation {
    const total = operations.reduce((sum, operation) => sum + operation.weight, 0);
    if (operations.length === 0 || total <= 0)
        throw new Error('Workload has no enabled operations');
    let cursor = Math.min(Math.max(random, 0), 1 - Number.EPSILON) * total;
    for (const operation of operations) {
        cursor -= operation.weight;
        if (cursor < 0) return operation;
    }
    return operations[operations.length - 1];
}

export function seededRandom(seed: string): () => number {
    let state = 2166136261;
    for (const character of seed) {
        state ^= character.charCodeAt(0);
        state = Math.imul(state, 16777619);
    }
    return () => {
        state += 0x6d2b79f5;
        let value = state;
        value = Math.imul(value ^ (value >>> 15), value | 1);
        value ^= value + Math.imul(value ^ (value >>> 7), value | 61);
        return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
    };
}

export async function paceUntil(scheduledAt: number, signal: AbortSignal): Promise<boolean> {
    const waitMs = scheduledAt - Date.now();
    if (waitMs <= 0) return !signal.aborted;
    return new Promise(resolve => {
        const timer = setTimeout(() => {
            signal.removeEventListener('abort', stop);
            resolve(true);
        }, waitMs);
        const stop = () => {
            clearTimeout(timer);
            resolve(false);
        };
        signal.addEventListener('abort', stop, { once: true });
    });
}

export function nextScheduleAt(scheduledAt: number, tps: number, now = Date.now()): number {
    const intervalMs = 1_000 / tps;
    return Math.max(scheduledAt + intervalMs, now + intervalMs);
}
