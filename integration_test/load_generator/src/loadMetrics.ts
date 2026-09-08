import http, { Server } from 'node:http';
import { collectDefaultMetrics, Counter, Gauge, Histogram, Registry } from 'prom-client';
import { LoadType } from './loadConfig';

export type LoadLane = 'evm' | 'cosmos';
export type LoadOutcome =
    | 'submitted'
    | 'included'
    | 'included_failed'
    | 'poll_timeout'
    | 'rejected'
    | 'skipped';

export interface LoadTransactionCount {
    lane: LoadLane;
    operation: string;
    outcome: LoadOutcome;
    count: number;
}

export interface LoadLatencyHistogram {
    buckets: { upperBound: number; count: number }[];
    count: number;
    sum: number;
}

export interface LoadMetricsSnapshot {
    transactions: LoadTransactionCount[];
    includedLatency: LoadLatencyHistogram;
}

type HistogramValues = Awaited<ReturnType<Histogram<string>['get']>>['values'];

export class LoadMetrics {
    private readonly registry = new Registry();
    private readonly transactions = new Counter({
        name: 'sei_loadgen_transactions_total',
        help: 'Load-generator transactions by operation and lifecycle outcome.',
        labelNames: ['load_type', 'lane', 'operation', 'outcome'] as const,
        registers: [this.registry],
    });
    private readonly latency = new Histogram({
        name: 'sei_loadgen_transaction_seconds',
        help: 'Transaction build, broadcast, and inclusion latency.',
        labelNames: ['load_type', 'lane', 'operation', 'outcome'] as const,
        buckets: [0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 40, 60],
        registers: [this.registry],
    });
    private readonly pending = new Gauge({
        name: 'sei_loadgen_pending_transactions',
        help: 'Transactions currently queued or awaiting a result.',
        labelNames: ['load_type', 'lane'] as const,
        registers: [this.registry],
    });
    private readonly targetTps = new Gauge({
        name: 'sei_loadgen_target_tps',
        help: 'Configured transaction rate for this process.',
        labelNames: ['load_type'] as const,
        registers: [this.registry],
    });
    private server?: Server;
    private ready = false;

    constructor(private readonly loadType: LoadType, tps: number) {
        this.registry.setDefaultLabels({ load_type: loadType });
        collectDefaultMetrics({ register: this.registry, prefix: 'sei_loadgen_' });
        this.targetTps.set({ load_type: loadType }, tps);
    }

    record(lane: LoadLane, operation: string, outcome: LoadOutcome): void {
        this.transactions.inc({
            load_type: this.loadType,
            lane,
            operation,
            outcome,
        });
    }

    observe(lane: LoadLane, operation: string, outcome: LoadOutcome, seconds: number): void {
        this.latency.observe({ load_type: this.loadType, lane, operation, outcome }, seconds);
    }

    setPending(lane: LoadLane, value: number): void {
        this.pending.set({ load_type: this.loadType, lane }, value);
    }

    setReady(ready: boolean): void {
        this.ready = ready;
    }

    // Reading the counters back keeps the end-of-run report and the Prometheus dashboards on
    // one source of truth, and it works when the metrics server is disabled.
    async snapshot(): Promise<LoadMetricsSnapshot> {
        const [transactions, latency] = await Promise.all([
            this.transactions.get(),
            this.latency.get(),
        ]);
        return {
            transactions: transactions.values.map(value => ({
                lane: String(value.labels.lane) as LoadLane,
                operation: String(value.labels.operation),
                outcome: String(value.labels.outcome) as LoadOutcome,
                count: value.value,
            })),
            includedLatency: includedLatency(latency.values),
        };
    }

    async listen(port: number, host: string): Promise<void> {
        this.server = http.createServer(async (request, response) => {
            if (request.url === '/healthz') {
                response.statusCode = 200;
                response.end('ok\n');
                return;
            }
            if (request.url === '/readyz') {
                response.statusCode = this.ready ? 200 : 503;
                response.end(this.ready ? 'ready\n' : 'not ready\n');
                return;
            }
            if (request.url !== '/metrics') {
                response.statusCode = 404;
                response.end('not found\n');
                return;
            }
            response.setHeader('content-type', this.registry.contentType);
            response.end(await this.registry.metrics());
        });
        await new Promise<void>((resolve, reject) => {
            this.server!.once('error', reject);
            this.server!.listen(port, host, () => {
                this.server!.off('error', reject);
                resolve();
            });
        });
    }

    async close(): Promise<void> {
        if (!this.server) return;
        await new Promise<void>((resolve, reject) => {
            this.server!.close(error => (error ? reject(error) : resolve()));
        });
        this.server = undefined;
    }
}

function includedLatency(values: HistogramValues): LoadLatencyHistogram {
    const buckets = new Map<number, number>();
    let count = 0;
    let sum = 0;
    for (const value of values) {
        if (value.labels.outcome !== 'included') continue;
        if (value.metricName?.endsWith('_bucket')) {
            const upperBound =
                value.labels.le === '+Inf' ? Number.POSITIVE_INFINITY : Number(value.labels.le);
            buckets.set(upperBound, (buckets.get(upperBound) ?? 0) + value.value);
        } else if (value.metricName?.endsWith('_sum')) {
            sum += value.value;
        } else if (value.metricName?.endsWith('_count')) {
            count += value.value;
        }
    }
    return {
        buckets: [...buckets]
            .sort(([left], [right]) => left - right)
            .map(([upperBound, bucketCount]) => ({ upperBound, count: bucketCount })),
        count,
        sum,
    };
}
