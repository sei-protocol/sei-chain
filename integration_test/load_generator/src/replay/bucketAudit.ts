import fs from 'fs/promises';
import type { ReplayFidelity } from './evmAdapters';
import type { ReplayLane } from './replayMetrics';

export type BucketOutcome = 'included' | 'included_failed' | 'rejected' | 'skipped';

export interface BucketAuditRecord {
    recordedAt: string;
    sourceNetwork: 'pacific-1';
    targetNetwork: string;
    sourceBlock: number;
    sourceCosmosHash?: string;
    sourceEvmHash?: string;
    lane: ReplayLane;
    sequence: number;
    sourceMessageTypes?: string[];
    sourceEvmKind?: string;
    sourceEvmType?: number;
    sourceSelector?: string | null;
    sourceTransactionBytes: number;
    sourceCalldataBytes?: number;
    traceAvailability?: string;
    sourceFrames?: number;
    sourceDelegatecalls?: number;
    sourceReads?: number;
    sourceWrites?: number;
    sourceChangedAccounts?: number;
    sourceChangedStorageSlots?: number;
    sourceDeployedRuntimeBytes?: number;
    sourceCreationMethod?: 'CREATE' | 'CREATE2';
    adapter: string;
    fidelity: ReplayFidelity;
    reason?: string;
    targetHash?: string;
    targetTransactionBytes?: number;
    targetCalldataBytes?: number;
    outcome: BucketOutcome;
    error?: string;
}

export class BucketAuditWriter {
    private queue = Promise.resolve();
    private total = 0;
    private unmatched = 0;
    private readonly byAdapter: Record<string, number> = {};

    constructor(
        readonly auditPath: string,
        readonly unmatchedPath: string,
        private readonly logToConsole: boolean,
    ) {}

    async initialize(): Promise<void> {
        await Promise.all([
            fs.writeFile(this.auditPath, '', 'utf8'),
            fs.writeFile(this.unmatchedPath, '', 'utf8'),
        ]);
    }

    record(record: BucketAuditRecord): Promise<void> {
        this.total++;
        this.byAdapter[record.adapter] = (this.byAdapter[record.adapter] ?? 0) + 1;
        const isUnmatched = record.fidelity !== 'semantic';
        if (isUnmatched) this.unmatched++;
        const line = `${JSON.stringify(record)}\n`;
        this.queue = this.queue.then(async () => {
            await fs.appendFile(this.auditPath, line, 'utf8');
            if (isUnmatched) await fs.appendFile(this.unmatchedPath, line, 'utf8');
            if (this.logToConsole) {
                console.log(
                    `BUCKET block=${record.sourceBlock} lane=${record.lane} ` +
                        `source=${record.sourceEvmHash ?? record.sourceCosmosHash} ` +
                        `adapter=${record.adapter} fidelity=${record.fidelity} ` +
                        `outcome=${record.outcome} target=${record.targetHash ?? '-'}`,
                );
            }
        });
        return this.queue;
    }

    async flush(): Promise<void> {
        await this.queue;
    }

    summary(): {
        total: number;
        unmatched: number;
        semantic: number;
        byAdapter: Record<string, number>;
    } {
        return {
            total: this.total,
            unmatched: this.unmatched,
            semantic: this.total - this.unmatched,
            byAdapter: { ...this.byAdapter },
        };
    }
}
