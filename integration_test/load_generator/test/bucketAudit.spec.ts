import { expect } from 'chai';
import fs from 'fs/promises';
import os from 'os';
import path from 'path';
import { BucketAuditRecord, BucketAuditWriter } from '../src/replay/bucketAudit';

const record: BucketAuditRecord = {
    recordedAt: new Date(0).toISOString(),
    sourceNetwork: 'pacific-1',
    targetNetwork: 'arctic-1',
    sourceBlock: 1,
    lane: 'cosmos',
    sequence: 0,
    sourceTransactionBytes: 1,
    adapter: 'test',
    fidelity: 'semantic',
    outcome: 'included',
};

describe('bucket audit writer', () => {
    it('creates parent directories and recovers its queue after a failed write', async () => {
        const root = await fs.mkdtemp(path.join(os.tmpdir(), 'bucket-audit-'));
        const auditPath = path.join(root, 'nested', 'audit.jsonl');
        const unmatchedPath = path.join(root, 'other', 'unmatched.jsonl');
        const writer = new BucketAuditWriter(auditPath, unmatchedPath, false);
        try {
            await writer.initialize();
            await fs.unlink(auditPath);
            await fs.mkdir(auditPath);
            let failed = false;
            try {
                await writer.record(record);
            } catch {
                failed = true;
            }
            expect(failed).to.equal(true);
            await fs.rm(auditPath, { recursive: true });
            await fs.writeFile(auditPath, '');
            await writer.record({ ...record, sequence: 1 });
            await writer.flush();
            expect((await fs.readFile(auditPath, 'utf8')).trim()).to.contain('"sequence":1');
        } finally {
            await fs.rm(root, { recursive: true, force: true });
        }
    });
});
