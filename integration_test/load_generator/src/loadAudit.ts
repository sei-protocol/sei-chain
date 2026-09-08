import fs from 'node:fs/promises';
import path from 'node:path';
import { LoadLane, LoadOutcome } from './loadMetrics';

export interface LoadAuditRecord {
    timestamp: string;
    runId: string;
    loadType: string;
    sequence: number;
    worker: number;
    operation: string;
    lane: LoadLane;
    outcome: LoadOutcome;
    hash?: string;
    error?: string;
}

export class LoadAuditWriter {
    private queue = Promise.resolve();
    private size = 0;

    constructor(
        readonly file: string,
        private readonly maxBytes = 100 * 1024 * 1024,
        private readonly retainFiles = 5,
    ) {}

    async initialize(): Promise<void> {
        await fs.mkdir(path.dirname(this.file), { recursive: true });
        await fs.writeFile(this.file, '', { flag: 'a' });
        this.size = (await fs.stat(this.file)).size;
    }

    record(record: LoadAuditRecord): Promise<void> {
        const write = this.queue.then(() => this.append(`${JSON.stringify(record)}\n`));
        this.queue = write.catch(() => undefined);
        return write;
    }

    flush(): Promise<void> {
        return this.queue;
    }

    private async append(line: string): Promise<void> {
        const bytes = Buffer.byteLength(line);
        if (this.size > 0 && this.size + bytes > this.maxBytes) await this.rotate();
        await fs.appendFile(this.file, line);
        this.size += bytes;
    }

    private async rotate(): Promise<void> {
        await fs.rm(`${this.file}.${this.retainFiles}`, { force: true });
        for (let index = this.retainFiles - 1; index >= 1; index--) {
            await fs.rename(`${this.file}.${index}`, `${this.file}.${index + 1}`).catch(error => {
                if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
            });
        }
        await fs.rename(this.file, `${this.file}.1`);
        await fs.writeFile(this.file, '');
        this.size = 0;
    }
}
