import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError } from '../utils/chainUtils';
import { gethUnlockedAccount, assertEthSignRecovers, SIGNATURE_65 } from '../utils/walletUtils';

const MESSAGE = ethers.hexlify(ethers.toUtf8Bytes('sei rpc parity probe'));
const STRANGER = ethers.Wallet.createRandom().address;

describe('eth_sign', function () {
    this.timeout(60 * 1000);

    const { sei, geth } = bothProviders();

    describe('geth reference: signs with a node-hosted key', () => {
        it('returns a 65-byte signature that recovers the signer (EIP-191 personal sign)', async () => {
            const from = await gethUnlockedAccount(geth);
            const sig = await geth.send('eth_sign', [from, MESSAGE]);
            expect(sig, 'signature shape').to.match(SIGNATURE_65);
            assertEthSignRecovers(from, MESSAGE, sig);
        });
    });

    describe('[divergence] Sei does not host arbitrary signing keys', () => {
        // The standard signs only for accounts the node holds; Sei never hosts external keys over
        // JSON-RPC (sign client-side + eth_sendRawTransaction instead), so it refuses to sign.
        it('refuses to sign for an address it does not host', async () => {
            const res = await rawSei('eth_sign', [STRANGER, MESSAGE]);
            expect(res.error, JSON.stringify(res)).to.not.equal(undefined);
            expect(res.error!.message, 'no-hosted-key signature').to.match(/hosted key/i);
        });
    });

    describe('wrong params / error handling', () => {
        it('a malformed signer address is rejected with -32602', async () => {
            const res = await rawSei('eth_sign', ['0x1234', MESSAGE]);
            expectJsonRpcError(res, -32602, /hex string has length 4, want 40 for common\.Address/);
        });

        it('a missing data argument is rejected with -32602', async () => {
            const res = await rawSei('eth_sign', [STRANGER]);
            expectJsonRpcError(res, -32602, /missing value for required argument 1/);
        });
    });
});
