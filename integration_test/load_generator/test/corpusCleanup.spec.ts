import { expect } from 'chai';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {
    cleanupConsumedReplaySegments,
    readReplaySegments,
    removeAllReplaySegments,
} from '../src/replay/corpus';
import { ReplaySegment } from '../src/replay/replayTypes';

describe('replay corpus cleanup', () => {
    let directory: string;

    beforeEach(async () => {
        directory = await fs.mkdtemp(path.join(os.tmpdir(), 'sei-replay-cleanup-'));
    });

    afterEach(async () => {
        await fs.rm(directory, { recursive: true, force: true });
    });

    it('removes consumed segments while retaining the newest completed segment', async () => {
        await writeFiles([
            'pacific-1-0000000001-0000000200.json',
            'pacific-1-0000000201-0000000400.json',
            'pacific-1-0000000401-0000000600.json',
            'bucket-audit-arctic-1-run.jsonl',
        ]);

        const removed = await cleanupConsumedReplaySegments(directory, 400, 1);

        expect(removed).to.deep.equal(['pacific-1-0000000001-0000000200.json']);
        expect(await fs.readdir(directory)).to.have.members([
            'pacific-1-0000000201-0000000400.json',
            'pacific-1-0000000401-0000000600.json',
            'bucket-audit-arctic-1-run.jsonl',
        ]);
    });

    it('removes only segment files when retiring an archived corpus', async () => {
        await writeFiles([
            'pacific-1-0000000001-0000000200.json',
            'capture-checkpoint.json',
            'bucket-audit-arctic-1-run.jsonl',
            'replay-report-arctic-1-run.json',
        ]);

        expect(await removeAllReplaySegments(directory)).to.deep.equal([
            'pacific-1-0000000001-0000000200.json',
        ]);
        expect(await fs.readdir(directory)).to.have.members([
            'capture-checkpoint.json',
            'bucket-audit-arctic-1-run.jsonl',
            'replay-report-arctic-1-run.json',
        ]);
    });

    it('serves cached segments by filename and evicts deleted files', async () => {
        const first = 'pacific-1-0000000001-0000000200.json';
        const second = 'pacific-1-0000000201-0000000400.json';
        await writeFiles([first, second]);

        const cache = new Map<string, ReplaySegment>();
        await readReplaySegments(directory, false, cache);
        expect([...cache.keys()]).to.have.members([first, second]);

        // Cached entries are reused even if the file is rewritten (segments are
        // immutable in production), and entries for deleted files are evicted.
        const sentinel = { marker: true } as unknown as ReplaySegment;
        cache.set(second, sentinel);
        await fs.unlink(path.join(directory, first));
        const segments = await readReplaySegments(directory, false, cache);
        expect(segments).to.deep.equal([sentinel]);
        expect([...cache.keys()]).to.deep.equal([second]);
    });

    async function writeFiles(files: string[]): Promise<void> {
        await Promise.all(files.map(file => fs.writeFile(path.join(directory, file), '{}')));
    }
});
