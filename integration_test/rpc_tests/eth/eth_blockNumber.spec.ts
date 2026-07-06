import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError, sleep } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';
import { HEX_QUANTITY } from '../utils/format';

describe('eth_blockNumber Tests', function () {
    this.timeout(60 * 1000);

    const { sei, geth } = bothProviders();
    let runtime: RuntimeState;

    before(() => {
        runtime = readRuntimeState();
    });

    describe('eth_blockNumber Queries', () => {
        it('returns a canonical hex quantity > 0', async () => {
            const hex = await sei.send('eth_blockNumber', []);
            expect(hex).to.match(HEX_QUANTITY);
            expect(ethers.toNumber(hex)).to.be.greaterThan(0);
        });

        it('agrees with ethers Provider.getBlockNumber()', async () => {
            const [raw, viaProvider] = await Promise.all([
                sei.send('eth_blockNumber', []),
                sei.getBlockNumber(),
            ]);
            // Heights can advance by a block between the two calls; allow a small drift.
            expect(Math.abs(ethers.toNumber(raw) - viaProvider)).to.be.lte(2);
        });
    });

    describe('schema matching', () => {
        it('Sei and geth both return canonical hex quantities', async () => {
            const [seiHex, gethHex] = await Promise.all([
                sei.send('eth_blockNumber', []),
                geth.send('eth_blockNumber', []),
            ]);
            expect(seiHex, 'sei').to.match(HEX_QUANTITY);
            expect(gethHex, 'geth').to.match(HEX_QUANTITY);
        });

        it('the value parses to a safe positive integer', async () => {
            const hex = await sei.send('eth_blockNumber', []);
            const n = ethers.toNumber(hex);
            expect(Number.isSafeInteger(n)).to.equal(true);
            expect(n).to.be.greaterThan(0);
        });

        it('has no leading zeros in the hex encoding', async () => {
            const hex: string = await sei.send('eth_blockNumber', []);
            expect(hex === '0x0' || !/^0x0/.test(hex), `non-minimal encoding: ${hex}`).to.equal(true);
        });
    });

    describe('empty / null handling', () => {
        it('never returns null or undefined', async () => {
            const hex = await sei.send('eth_blockNumber', []);
            expect(hex).to.not.equal(null);
            expect(hex).to.not.equal(undefined);
        });

        it('always returns a non-empty hex string (raw transport)', async () => {
            const body = await rawSei<string>('eth_blockNumber', []);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            expect(body.result).to.be.a('string');
            expect(body.result).to.match(HEX_QUANTITY);
        });
    });

    describe('wrong params / error handling', () => {
        it('Sei rejects an extra positional parameter with -32602, identically to geth', async () => {
            const [seiBody, gethBody] = await Promise.all([
                rawSei('eth_blockNumber', ['latest']),
                rawGeth('eth_blockNumber', ['latest']),
            ]);
            expectJsonRpcError(seiBody, -32602, /too many arguments, want at most 0/i);
            expectJsonRpcError(gethBody, -32602, /too many arguments, want at most 0/i);
            expect(seiBody.error?.code).to.equal(gethBody.error?.code);
            expect(seiBody.error?.message).to.equal(gethBody.error?.message);
        });

        it('Sei rejects non-array params with -32602 non-array args, identically to geth', async () => {
            const [seiBody, gethBody] = await Promise.all([
                rawSei('eth_blockNumber', 'latest'),
                rawGeth('eth_blockNumber', 'latest'),
            ]);
            expectJsonRpcError(seiBody, -32602, /non-array args/i);
            expectJsonRpcError(gethBody, -32602, /non-array args/i);
            expect(seiBody.error?.code).to.equal(gethBody.error?.code);
            expect(seiBody.error?.message).to.equal(gethBody.error?.message);
        });
    });
});
