import { expect } from 'chai';
import { sleep } from './chainUtils';
import { ADDRESS, HASH32, HEX_QUANTITY, HEX_DATA, BLOOM256, NONCE8 } from './format';

export class WsClient {
    private nextId = 1;
    private readonly pending = new Map<number, { resolve: (v: any) => void; reject: (e: any) => void }>();
    private readonly buffers = new Map<string, any[]>();

    private constructor(private readonly socket: any) {
        socket.onmessage = (ev: any) => this.onMessage(String(ev.data));
    }

    static open(url: string, timeoutMs = 10_000): Promise<WsClient> {
        const Impl: any = (globalThis as any).WebSocket;
        if (!Impl) throw new Error('global WebSocket unavailable (Node >= 21 required)');
        return new Promise<WsClient>((resolve, reject) => {
            const socket = new Impl(url);
            const client = new WsClient(socket);
            const timer = setTimeout(() => {
                try { socket.close(); } catch { /* noop */ }
                reject(new Error(`WsClient.open: timed out connecting to ${url}`));
            }, timeoutMs);
            socket.onopen = () => { clearTimeout(timer); resolve(client); };
            socket.onerror = () => { clearTimeout(timer); reject(new Error(`WsClient.open: cannot reach ${url}`)); };
        });
    }

    private onMessage(raw: string): void {
        let msg: any;
        try { msg = JSON.parse(raw); } catch { return; }
        if (msg.id !== undefined && this.pending.has(msg.id)) {
            const p = this.pending.get(msg.id)!;
            this.pending.delete(msg.id);
            msg.error ? p.reject(msg.error) : p.resolve(msg.result);
            return;
        }
        if (msg.method === 'eth_subscription' && msg.params) {
            const buf = this.buffers.get(msg.params.subscription);
            if (buf) buf.push(msg.params.result);
        }
    }

    private call(method: string, params: unknown[]): Promise<any> {
        const id = this.nextId++;
        return new Promise((resolve, reject) => {
            this.pending.set(id, { resolve, reject });
            this.socket.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }));
        });
    }

    /** eth_subscribe; returns the opaque subscription id and starts buffering its notifications. */
    async subscribe(params: unknown[]): Promise<string> {
        const id: string = await this.call('eth_subscribe', params);
        this.buffers.set(id, []);
        return id;
    }

    /** Block until `want` notifications have arrived for `subId`, then return the first `want`. */
    async waitFor(subId: string, want: number, timeoutMs = 30_000): Promise<any[]> {
        const buf = this.buffers.get(subId);
        if (!buf) throw new Error(`WsClient.waitFor: unknown subscription ${subId}`);
        const deadline = Date.now() + timeoutMs;
        while (buf.length < want && Date.now() < deadline) await sleep(100);
        if (buf.length < want) {
            throw new Error(`WsClient.waitFor(${subId}): got ${buf.length}/${want} notifications before timeout`);
        }
        return buf.slice(0, want);
    }

    /** eth_unsubscribe; returns the node's boolean result. */
    unsubscribe(subId: string): Promise<boolean> {
        return this.call('eth_unsubscribe', [subId]);
    }

    close(): void {
        try { this.socket.close(); } catch { /* noop */ }
    }
}

const ZERO_HASH = '0x' + '00'.repeat(32);
const ZERO_NONCE = '0x' + '00'.repeat(8);

/** Subscription ids are opaque random hex, not minimally-encoded quantities. */
export const SUBSCRIPTION_ID = /^0x[0-9a-f]+$/;

/** Every field Sei pushes in an eth_newHeads notification (evmrpc/subscribe.go encodeCommittedBlock). */
export const NEW_HEAD_FIELDS = [
    'baseFeePerGas',
    'blobGasUsed',
    'difficulty',
    'excessBlobGas',
    'extraData',
    'gasLimit',
    'gasUsed',
    'hash',
    'logsBloom',
    'miner',
    'mixHash',
    'nonce',
    'number',
    'parentBeaconBlockRoot',
    'parentHash',
    'receiptsRoot',
    'sha3Uncles',
    'stateRoot',
    'timestamp',
    'transactionsRoot',
    'withdrawalsRoot',
] as const;

const HEAD_QUANTITY_FIELDS = [
    'number',
    'gasLimit',
    'gasUsed',
    'timestamp',
    'difficulty',
    'baseFeePerGas',
    'blobGasUsed',
    'excessBlobGas',
] as const;

const HEAD_HASH_FIELDS = [
    'hash',
    'parentHash',
    'receiptsRoot',
    'transactionsRoot',
    'stateRoot',
    'sha3Uncles',
    'mixHash',
    'parentBeaconBlockRoot',
    'withdrawalsRoot',
] as const;

/** Assert a newHeads payload carries exactly the documented fields, each canonically encoded. */
export function assertNewHeadSchema(head: any): void {
    NEW_HEAD_FIELDS.forEach(f => expect(head, `newHead has ${f}`).to.have.property(f));
    HEAD_QUANTITY_FIELDS.forEach(f =>
        expect(head[f], `newHead.${f} is a canonical quantity`).to.match(HEX_QUANTITY),
    );
    HEAD_HASH_FIELDS.forEach(f => expect(head[f], `newHead.${f} is a 32-byte hash`).to.match(HASH32));
    expect(head.miner, 'newHead.miner').to.match(ADDRESS);
    expect(head.logsBloom, 'newHead.logsBloom').to.match(BLOOM256);
    expect(head.nonce, 'newHead.nonce').to.match(NONCE8);
    expect(head.extraData, 'newHead.extraData').to.match(HEX_DATA);
}

/** The PoW/PoS-only header fields Sei cannot populate must be their canonical zero values. */
export function assertNewHeadInapplicableZeros(head: any): void {
    expect(BigInt(head.difficulty), 'difficulty is 0 (inapplicable)').to.equal(0n);
    expect(head.sha3Uncles, 'sha3Uncles is the zero hash').to.equal(ZERO_HASH);
    expect(head.mixHash, 'mixHash is the zero hash').to.equal(ZERO_HASH);
    expect(head.nonce, 'nonce is the zero block nonce').to.equal(ZERO_NONCE);
    expect(head.extraData, 'extraData is empty').to.equal('0x');
}

/**
 * Cross-check a pushed head against the canonical block the node reports for the same height:
 * the deterministic fields (identity, proposer, fee market, time) must agree exactly. gasUsed is
 * intentionally excluded — newHeads sums TxResult gas while eth_getBlockByNumber sums receipt gas.
 */
export function assertNewHeadMatchesBlock(head: any, block: any): void {
    expect(BigInt(head.number), 'number matches the canonical block').to.equal(BigInt(block.number));
    expect(head.hash, 'hash matches the canonical block').to.equal(block.hash);
    expect(head.miner.toLowerCase(), 'miner matches the canonical block').to.equal(
        block.miner.toLowerCase(),
    );
    expect(BigInt(head.timestamp), 'timestamp matches the canonical block').to.equal(
        BigInt(block.timestamp),
    );
    expect(BigInt(head.gasLimit), 'gasLimit matches the canonical block').to.equal(
        BigInt(block.gasLimit),
    );
    expect(BigInt(head.baseFeePerGas), 'baseFeePerGas matches the canonical block').to.equal(
        BigInt(block.baseFeePerGas),
    );
}
