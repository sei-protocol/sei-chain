import {
    LoadLatencyHistogram,
    LoadMetricsSnapshot,
    LoadOutcome,
    LoadTransactionCount,
} from './loadMetrics';

// Outcomes that close a transaction's lifecycle. `submitted` is an intermediate step and would
// double-count against these.
const FINAL_OUTCOMES: LoadOutcome[] = [
    'included',
    'included_failed',
    'poll_timeout',
    'rejected',
    'skipped',
];
// Final outcomes that reached the chain, so queue-full skips do not deflate the success rate.
const ATTEMPTED_OUTCOMES: LoadOutcome[] = [
    'included',
    'included_failed',
    'poll_timeout',
    'rejected',
];

export type LoadStopReason = 'completed' | 'signal' | 'error';

export interface LoadRunIdentity {
    runId: string;
    executionId: string;
    loadType: string;
    targetTps: number;
    workerCount: number;
    partitionIndex: number;
}

export interface LoadRunWindow {
    startedAt: string;
    completedAt: string;
    durationSeconds: number;
}

export interface LoadRunSummary extends LoadRunIdentity {
    schemaVersion: 1;
    window: LoadRunWindow;
    stopReason: LoadStopReason;
    throughput: {
        targetTps: number;
        offeredTps: number;
        submittedTps: number;
        includedTps: number;
    };
    transactions: {
        offered: number;
        submitted: number;
        included: number;
        successRatePercent: number;
        outcomes: { outcome: LoadOutcome; count: number }[];
    };
    includedLatencySeconds: {
        p50: number;
        p90: number;
        p95: number;
        p99: number;
        mean: number;
    };
    operations: LoadTransactionCount[];
}

/**
 * Aggregates one process's collected metrics into the report written at the end of a run. Counts
 * and percentiles use the same definitions as the Prometheus report so per-pod and fleet-wide
 * numbers are comparable.
 */
export function summarizeRun(
    identity: LoadRunIdentity,
    window: LoadRunWindow,
    stopReason: LoadStopReason,
    snapshot: LoadMetricsSnapshot,
): LoadRunSummary {
    const outcomes = tallyOutcomes(snapshot.transactions);
    const total = (names: LoadOutcome[]) =>
        names.reduce((sum, name) => sum + (outcomes.get(name) ?? 0), 0);
    const offered = total(FINAL_OUTCOMES);
    const attempted = total(ATTEMPTED_OUTCOMES);
    const submitted = outcomes.get('submitted') ?? 0;
    const included = outcomes.get('included') ?? 0;
    const seconds = Math.max(window.durationSeconds, 0.001);
    const latency = snapshot.includedLatency;
    return {
        schemaVersion: 1,
        ...identity,
        window,
        stopReason,
        throughput: {
            targetTps: identity.targetTps,
            offeredTps: round(offered / seconds, 3),
            submittedTps: round(submitted / seconds, 3),
            includedTps: round(included / seconds, 3),
        },
        transactions: {
            offered,
            submitted,
            included,
            successRatePercent: attempted > 0 ? round((included / attempted) * 100, 2) : 0,
            // Only final outcomes, so the breakdown sums to `offered`. `submitted` is reported
            // above as its own field.
            outcomes: FINAL_OUTCOMES.filter(outcome => outcomes.has(outcome)).map(outcome => ({
                outcome,
                count: outcomes.get(outcome) ?? 0,
            })),
        },
        includedLatencySeconds: {
            p50: round(histogramQuantile(latency, 0.5), 4),
            p90: round(histogramQuantile(latency, 0.9), 4),
            p95: round(histogramQuantile(latency, 0.95), 4),
            p99: round(histogramQuantile(latency, 0.99), 4),
            mean: latency.count > 0 ? round(latency.sum / latency.count, 4) : 0,
        },
        operations: snapshot.transactions
            .filter(item => item.outcome !== 'submitted')
            .sort(
                (left, right) =>
                    left.lane.localeCompare(right.lane) ||
                    left.operation.localeCompare(right.operation) ||
                    left.outcome.localeCompare(right.outcome),
            ),
    };
}

/**
 * Interpolates a quantile from cumulative histogram buckets the way Prometheus `histogram_quantile`
 * does. Resolution is bounded by the configured bucket edges, and values above the highest finite
 * edge report as that edge.
 */
export function histogramQuantile(histogram: LoadLatencyHistogram, quantile: number): number {
    const { buckets, count } = histogram;
    if (count <= 0 || buckets.length === 0) return 0;
    const highestFinite = buckets.filter(bucket => Number.isFinite(bucket.upperBound)).at(-1);
    if (!highestFinite) return 0;
    const rank = quantile * count;
    let lowerBound = 0;
    let lowerCount = 0;
    for (const bucket of buckets) {
        if (bucket.count < rank) {
            lowerBound = bucket.upperBound;
            lowerCount = bucket.count;
            continue;
        }
        if (!Number.isFinite(bucket.upperBound)) return highestFinite.upperBound;
        const share = bucket.count - lowerCount;
        if (share <= 0) return bucket.upperBound;
        return lowerBound + ((bucket.upperBound - lowerBound) * (rank - lowerCount)) / share;
    }
    return highestFinite.upperBound;
}

export function formatRunSummary(summary: LoadRunSummary): string {
    const { throughput, transactions, includedLatencySeconds: latency } = summary;
    const lines = [
        `Run summary ${summary.loadType} run=${summary.runId} ` +
            `partition=${summary.partitionIndex} stop=${summary.stopReason}`,
        `  window       ${summary.window.startedAt} -> ${summary.window.completedAt} ` +
            `(${summary.window.durationSeconds.toFixed(1)}s)`,
        `  throughput   target ${throughput.targetTps} tx/s | offered ${throughput.offeredTps} | ` +
            `submitted ${throughput.submittedTps} | included ${throughput.includedTps}`,
        `  transactions offered ${transactions.offered} | submitted ${transactions.submitted} | ` +
            `included ${transactions.included} | success ${transactions.successRatePercent}%`,
        `  latency      p50 ${latency.p50}s | p90 ${latency.p90}s | p95 ${latency.p95}s | ` +
            `p99 ${latency.p99}s | mean ${latency.mean}s`,
        `  outcomes     ${
            transactions.outcomes.map(item => `${item.outcome} ${item.count}`).join(', ') || 'none'
        }`,
    ];
    if (summary.operations.length > 0) lines.push('  operations');
    for (const item of groupOperations(summary.operations)) {
        lines.push(`    ${item.lane.padEnd(6)} ${item.operation.padEnd(22)} ${item.counts}`);
    }
    return lines.join('\n');
}

function groupOperations(
    operations: LoadTransactionCount[],
): { lane: string; operation: string; counts: string }[] {
    const grouped = new Map<string, { lane: string; operation: string; counts: string[] }>();
    for (const item of operations) {
        const key = `${item.lane} ${item.operation}`;
        const entry = grouped.get(key) ?? {
            lane: item.lane,
            operation: item.operation,
            counts: [],
        };
        entry.counts.push(`${item.outcome} ${item.count}`);
        grouped.set(key, entry);
    }
    return [...grouped.values()].map(entry => ({ ...entry, counts: entry.counts.join(', ') }));
}

function tallyOutcomes(transactions: LoadTransactionCount[]): Map<LoadOutcome, number> {
    const outcomes = new Map<LoadOutcome, number>();
    for (const item of transactions) {
        outcomes.set(item.outcome, (outcomes.get(item.outcome) ?? 0) + item.count);
    }
    return outcomes;
}

function round(value: number, digits: number): number {
    if (!Number.isFinite(value)) return 0;
    const factor = 10 ** digits;
    return Math.round(value * factor) / factor;
}
