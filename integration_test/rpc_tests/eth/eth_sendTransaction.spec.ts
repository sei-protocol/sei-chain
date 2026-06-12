import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError } from '../utils/chainUtils';
import { HASH32 } from '../utils/format';
import { gethUnlockedAccount, transferArgs } from '../utils/walletUtils';

const STRANGER = ethers.Wallet.createRandom().address;
const TO = ethers.Wallet.createRandom().address;
const VALUE = ethers.parseEther('0.0001');

describe('eth_sendTransaction', function () {
    this.timeout(120 * 1000);

    const { sei, geth } = bothProviders();

    describe('geth reference: node-signs and broadcasts from a hosted account', () => {
        it('returns the tx hash and the transfer mines from the hosted account', async () => {
            const from = await gethUnlockedAccount(geth);
            const hash = await geth.send('eth_sendTransaction', [transferArgs(from, TO, VALUE)]);
            expect(hash, 'tx hash').to.match(HASH32);

            const receipt = await geth.waitForTransaction(hash, 1, 60_000);
            expect(receipt!.status, 'mines successfully').to.equal(1);

            const txObject = await geth.send('eth_getTransactionByHash', [hash]);
            expect(txObject.from.toLowerCase(), 'from is the hosted account').to.equal(
                from.toLowerCase(),
            );
            expect(BigInt(txObject.value), 'value').to.equal(VALUE);
        });
    });

    describe('[divergence] Sei does not host arbitrary signing keys', () => {
        // The standard node-signs only for hosted accounts; Sei hosts none over JSON-RPC, so the
        // documented path is to sign client-side and use eth_sendRawTransaction.
        it('refuses to send from an address it does not host', async () => {
            const res = await rawSei('eth_sendTransaction', [transferArgs(STRANGER, TO, VALUE)]);
            expect(res.error, JSON.stringify(res)).to.not.equal(undefined);
            expect(res.error!.message, 'no-hosted-key signature').to.match(/hosted key/i);
        });
    });

    describe('wrong params / error handling', () => {
        it('a missing transaction argument is rejected with -32602', async () => {
            const res = await rawSei('eth_sendTransaction', []);
            expectJsonRpcError(res, -32602, /missing value for required argument 0/);
        });

        it('a malformed from address is rejected with -32602', async () => {
            const res = await rawSei('eth_sendTransaction', [{ from: '0x1234', to: TO }]);
            expectJsonRpcError(res, -32602, /hex string has length 4, want 40 for common\.Address/);
        });
    });
});
