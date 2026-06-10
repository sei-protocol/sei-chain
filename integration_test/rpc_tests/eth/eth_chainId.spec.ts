import { expect } from 'chai';
import { ethers } from 'ethers';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, expectSameError } from '../utils/testUtils';
import { HEX_QUANTITY } from '../utils/format';

const COSMOS_TO_EVM_CHAIN_ID: Readonly<Record<string, number>> = Object.freeze({
    'pacific-1': 1329,
    'atlantic-2': 1328,
    'arctic-1': 713715,
});
const DEFAULT_EVM_CHAIN_ID = 713714;

describe('eth_chainId', function () {
    this.timeout(60 * 1000);

    const { sei, geth } = bothProviders();
    let runtime: RuntimeState;

    before(() => {
        runtime = readRuntimeState();
    });

    it('returns a canonical 0x-prefixed quantity on Sei', async () => {
        const hex = await sei.send('eth_chainId', []);
        expect(hex).to.match(HEX_QUANTITY);
        expect(Number(BigInt(hex))).to.equal(runtime.chainIds.sei);
    });

    it('agrees with the Sei chain id mapping table', async () => {
        const hex = await sei.send('eth_chainId', []);
        const id = Number(BigInt(hex));
        const expected = Object.values(COSMOS_TO_EVM_CHAIN_ID).includes(id)
            ? id
            : DEFAULT_EVM_CHAIN_ID;
        expect(id).to.equal(expected);
    });

    it('ethers Provider.getNetwork() agrees with raw eth_chainId on Sei', async () => {
        const network = await sei.getNetwork();
        const hex = await sei.send('eth_chainId', []);
        expect(network.chainId).to.equal(BigInt(hex));
    });

    it('net_version returns the same chain id in decimal form on Sei', async () => {
        const [hex, netVersion] = await Promise.all([
            sei.send('eth_chainId', []),
            sei.send('net_version', []),
        ]);
        expect(netVersion).to.match(/^[0-9]+$/, 'net_version must be a decimal string');
        expect(BigInt(netVersion)).to.equal(BigInt(hex));
    });

    it('rejects extra positional parameters identically to geth (-32602 error code)', async () => {
        const [s, g] = await Promise.all([
            rawSei('eth_chainId', ['latest']),
            rawGeth('eth_chainId', ['latest']),
        ]);
        expectJsonRpcError(s, -32602, /too many arguments, want at most 0/);
        expectSameError(s, g);
    });
});
