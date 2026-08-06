import http, { Server } from 'http';
import {
    collectDefaultMetrics,
    Counter,
    Gauge,
    Histogram,
    Registry,
} from 'prom-client';
import { ReplayFidelity } from './evmAdapters';
import { ReplayEvmTransaction } from './replayTypes';
import { sourceTraceCounts } from './traceCapture';

export type ReplayLane = 'evm' | 'cosmos';
export type ReplayOutcome =
    | 'submitted'
    | 'included'
    | 'included_failed'
    | 'poll_timeout'
    | 'rejected'
    | 'skipped';
export type ReplayByteKind =
    | 'source_transaction'
    | 'produced_transaction'
    | 'source_calldata'
    | 'produced_calldata';
export type ReplayGasKind = 'source' | 'target';

export class ReplayMetrics {
    private readonly registry = new Registry();
    private readonly offered = new Counter({
        name: 'pacific_replay_offered_transactions_total',
        help: 'Canonical Pacific transactions offered to the replay adapters.',
        labelNames: ['lane'] as const,
        registers: [this.registry],
    });
    private readonly adapted = new Counter({
        name: 'pacific_replay_adapted_transactions_total',
        help: 'Transactions classified by replay adapter and fidelity.',
        labelNames: ['lane', 'adapter', 'fidelity'] as const,
        registers: [this.registry],
    });
    private readonly outcomes = new Counter({
        name: 'pacific_replay_transaction_outcomes_total',
        help: 'Replay transaction lifecycle outcomes.',
        labelNames: ['lane', 'outcome'] as const,
        registers: [this.registry],
    });
    private readonly skipped = new Counter({
        name: 'pacific_replay_skipped_transactions_total',
        help: 'Replay transactions skipped before submission.',
        labelNames: ['lane', 'reason'] as const,
        registers: [this.registry],
    });
    private readonly bytes = new Counter({
        name: 'pacific_replay_bytes_total',
        help: 'Source and produced transaction or calldata bytes.',
        labelNames: ['lane', 'kind'] as const,
        registers: [this.registry],
    });
    private readonly gas = new Counter({
        name: 'pacific_replay_gas_used_total',
        help: 'Gas consumed by source and target transactions.',
        labelNames: ['lane', 'kind'] as const,
        registers: [this.registry],
    });
    private readonly traceAvailability = new Counter({
        name: 'pacific_replay_trace_transactions_total',
        help: 'Offered EVM transactions by captured trace availability.',
        labelNames: ['availability'] as const,
        registers: [this.registry],
    });
    private readonly traceProfile = new Counter({
        name: 'pacific_replay_source_trace_operations_total',
        help: 'Bounded source trace frame and operation summary counts.',
        labelNames: ['kind'] as const,
        registers: [this.registry],
    });
    private readonly latency = new Histogram({
        name: 'pacific_replay_submission_seconds',
        help: 'Time from replay construction through target-chain result.',
        labelNames: ['lane', 'outcome'] as const,
        buckets: [0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 40],
        registers: [this.registry],
    });
    private readonly pending = new Gauge({
        name: 'pacific_replay_pending_transactions',
        help: 'Replay jobs queued or executing.',
        labelNames: ['lane'] as const,
        registers: [this.registry],
    });
    private readonly bufferSeconds = new Gauge({
        name: 'pacific_replay_buffer_seconds',
        help: 'Captured Pacific chain-time available ahead of replay.',
        registers: [this.registry],
    });
    private readonly collectedHeight = new Gauge({
        name: 'pacific_replay_collected_source_height',
        help: 'Latest fully captured Pacific source height.',
        registers: [this.registry],
    });
    private readonly replayedHeight = new Gauge({
        name: 'pacific_replay_replayed_source_height',
        help: 'Latest Pacific source height completed by replay.',
        registers: [this.registry],
    });
    private readonly paused = new Gauge({
        name: 'pacific_replay_paused',
        help: 'Whether replay is paused waiting for its source buffer (1 or 0).',
        registers: [this.registry],
    });
    private readonly runRemainingSeconds = new Gauge({
        name: 'pacific_replay_run_remaining_seconds',
        help: 'Wall-clock seconds remaining in a bounded run, or -1 if unbounded.',
        registers: [this.registry],
    });
    private readonly runInfo = new Gauge({
        name: 'pacific_replay_run_info',
        help: 'Static replay run configuration.',
        labelNames: ['source', 'target', 'time_scale', 'privileged_mode'] as const,
        registers: [this.registry],
    });
    private server?: Server;

    constructor(
        source: string,
        target: string,
        timeScale: number,
        privilegedMode: string,
    ) {
        collectDefaultMetrics({
            prefix: 'pacific_replay_process_',
            register: this.registry,
        });
        this.runInfo.set(
            {
                source,
                target,
                time_scale: String(timeScale),
                privileged_mode: privilegedMode,
            },
            1,
        );
        this.pending.set({ lane: 'evm' }, 0);
        this.pending.set({ lane: 'cosmos' }, 0);
        this.paused.set(0);
        this.runRemainingSeconds.set(-1);
    }

    async listen(port: number, host: string): Promise<void> {
        this.server = http.createServer(async (request, response) => {
            if (request.url === '/healthz') {
                response.writeHead(200, { 'content-type': 'application/json' });
                response.end('{"status":"ok"}\n');
                return;
            }
            if (request.url !== '/metrics') {
                response.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' });
                response.end('Not found\n');
                return;
            }
            response.writeHead(200, { 'content-type': this.registry.contentType });
            response.end(await this.registry.metrics());
        });
        await new Promise<void>((resolve, reject) => {
            this.server!.once('error', reject);
            this.server!.listen(port, host, resolve);
        });
    }

    async close(): Promise<void> {
        if (!this.server) return;
        const server = this.server;
        await new Promise<void>((resolve, reject) => {
            server.close(error => (error ? reject(error) : resolve()));
            server.closeAllConnections();
        });
        this.server = undefined;
    }

    recordOffered(lane: ReplayLane): void {
        this.offered.inc({ lane });
    }

    recordAdapted(lane: ReplayLane, adapter: string, fidelity: ReplayFidelity): void {
        this.adapted.inc({ lane, adapter, fidelity });
    }

    recordOutcome(lane: ReplayLane, outcome: ReplayOutcome): void {
        this.outcomes.inc({ lane, outcome });
    }

    recordSkip(lane: ReplayLane, reason: string): void {
        this.outcomes.inc({ lane, outcome: 'skipped' });
        this.skipped.inc({ lane, reason });
    }

    recordBytes(lane: ReplayLane, kind: ReplayByteKind, value: number): void {
        if (value > 0) this.bytes.inc({ lane, kind }, value);
    }

    recordGas(lane: ReplayLane, kind: ReplayGasKind, value: bigint): void {
        if (value > 0n) this.gas.inc({ lane, kind }, Number(value));
    }

    recordTraceProfile(source: ReplayEvmTransaction): void {
        this.traceAvailability.inc({ availability: source.trace?.availability ?? 'not_captured' });
        const counts = sourceTraceCounts(source.trace);
        const values: Record<string, number> = {
            frames: counts.frames,
            delegatecalls: counts.delegatecalls,
            reads: counts.reads,
            writes: counts.writes,
            changed_accounts: counts.changedAccounts,
            changed_storage_slots: counts.changedStorageSlots,
        };
        for (const [kind, count] of Object.entries(values)) {
            if (count > 0) this.traceProfile.inc({ kind }, count);
        }
    }

    observeSubmission(lane: ReplayLane, outcome: ReplayOutcome, seconds: number): void {
        this.latency.observe({ lane, outcome }, seconds);
    }

    setPending(lane: ReplayLane, value: number): void {
        this.pending.set({ lane }, value);
    }

    setProgress(
        bufferSeconds: number,
        collectedHeight: number,
        replayedHeight: number | undefined,
        paused: boolean,
    ): void {
        this.bufferSeconds.set(bufferSeconds);
        this.collectedHeight.set(collectedHeight);
        if (replayedHeight !== undefined) this.replayedHeight.set(replayedHeight);
        this.paused.set(paused ? 1 : 0);
    }

    setRunRemaining(seconds: number | undefined): void {
        this.runRemainingSeconds.set(seconds === undefined ? -1 : Math.max(0, seconds));
    }
}
