import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError } from '../utils/chainUtils';
import { HEX_DATA } from '../utils/format';
import { gethUnlockedAccount, signableTxArgs } from '../utils/walletUtils';
import { sendRaw } from '../utils/txUtils';

const STRANGER = ethers.Wallet.createRandom();
const TO = ethers.Wallet.createRandom().address;
const VALUE = ethers.parseEther('0.0001');

describe('eth_signTransaction', function () {
    this.timeout(120 * 1000);

    const { sei, geth } = bothProviders();

    describe('geth reference: signs a transaction with a node-hosted key', () => {
        it('returns {raw, tx} whose raw re-derives the requested transaction', async () => {
            const from = await gethUnlockedAccount(geth);
            const args = await signableTxArgs(geth, from, TO, VALUE);
            const result = await geth.send('eth_signTransaction', [args]);

            expect(result, 'result object').to.be.an('object');
            expect(result.raw, 'raw is hex data').to.match(HEX_DATA);

            const decoded = ethers.Transaction.from(result.raw);
            expect(decoded.from?.toLowerCase(), 'recovered sender == from').to.equal(
                from.toLowerCase(),
            );
            expect(decoded.to?.toLowerCase(), 'to').to.equal(TO.toLowerCase());
            expect(decoded.value, 'value').to.equal(VALUE);
            expect(BigInt(decoded.nonce), 'nonce').to.equal(BigInt(args.nonce));
        });

        it('the signed raw is broadcastable via eth_sendRawTransaction', async () => {
            const from = await gethUnlockedAccount(geth);
            const args = await signableTxArgs(geth, from, TO, VALUE);
            const result = await geth.send('eth_signTransaction', [args]);
            const hash = await sendRaw(geth, result.raw);
            const receipt = await geth.waitForTransaction(hash, 1, 60_000);
            expect(receipt!.status, 'signed tx mines').to.equal(1);
        });
    });

    describe('[divergence] Sei does not host arbitrary signing keys', () => {
        it('refuses to sign for an address it does not host', async () => {
            const args = await signableTxArgs(sei, STRANGER.address, TO, VALUE);
            const res = await rawSei('eth_signTransaction', [args]);
            expect(res.error, JSON.stringify(res)).to.not.equal(undefined);
            expect(res.error!.message, 'no-hosted-key signature').to.match(/hosted key/i);
        });
    });

    describe('wrong params / error handling', () => {
        it('a missing transaction argument is rejected with -32602', async () => {
            const res = await rawSei('eth_signTransaction', []);
            expectJsonRpcError(res, -32602, /missing value for required argument 0/);
        });
    });
});
