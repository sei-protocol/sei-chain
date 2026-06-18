import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import {
    seedStorageToken,
    StorageScene,
    mappingSlot,
    nestedMappingSlot,
    storageWord,
    addressWord,
    STORAGE_WORD,
    SLOT_TOTAL_SUPPLY,
    SLOT_OWNER,
    SLOT_BALANCEOF,
    SLOT_ALLOWANCE,
} from '../utils/storageUtils';

// An unused, far-out slot guaranteed to be empty.
const UNUSED_SLOT = '0x' + 'f'.repeat(63) + 'e';
const ZERO_WORD = '0x' + '0'.repeat(64);

describe('eth_getStorageAt', function () {
    this.timeout(180 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let scene: StorageScene;
    let seiAdmin: string;
    let gethAdmin: string;

    before(async () => {
        runtime = readRuntimeState();
        seiAdmin = runtime.funded.admin;
        gethAdmin = runtime.funded.gethAdmin.address;
        const [deployer] = claimPool(runtime, sei, 1, 'eth_getStorageAt');
        scene = await seedStorageToken(deployer);
    });

    const at = (addr: string, slot: string, tag: any = 'latest') =>
        sei.send('eth_getStorageAt', [addr, slot, tag]);

    describe('happy path / schema (deterministic seeded contract)', () => {
        it('reads a scalar slot: the owner address (left-padded into a 32-byte word)', async () => {
            const word = await at(scene.erc20, ethers.toQuantity(SLOT_OWNER));
            expect(word, 'canonical 32-byte word').to.match(STORAGE_WORD);
            expect(word).to.equal(addressWord(scene.owner.address));
        });

        it('reads a scalar slot: totalSupply', async () => {
            const word = await at(scene.erc20, ethers.toQuantity(SLOT_TOTAL_SUPPLY));
            expect(word).to.equal(storageWord(scene.totalSupply));
        });

        it('reads a mapping slot: balanceOf[holder]', async () => {
            const slot = mappingSlot(scene.holder, SLOT_BALANCEOF);
            const word = await at(scene.erc20, slot);
            expect(word).to.equal(storageWord(scene.holderBalance));
        });

        it('reads a nested mapping slot: allowance[owner][spender]', async () => {
            const slot = nestedMappingSlot(scene.owner.address, scene.spender, SLOT_ALLOWANCE);
            const word = await at(scene.erc20, slot);
            expect(word).to.equal(storageWord(scene.allowanceAmount));
        });

        it('always returns a full 32-byte data word, even for small values', async () => {
            const word = await at(scene.erc20, ethers.toQuantity(SLOT_TOTAL_SUPPLY));
            expect(word.length, '0x + 64 nibbles').to.equal(66);
        });
    });

    describe('empty / non-existent storage', () => {
        it('returns a zero word for an unused slot of a real contract', async () => {
            expect(await at(scene.erc20, UNUSED_SLOT)).to.equal(ZERO_WORD);
        });

        it('returns a zero word for any slot of an EOA', async () => {
            expect(await at(seiAdmin, '0x0')).to.equal(ZERO_WORD);
            expect(await at(seiAdmin, UNUSED_SLOT)).to.equal(ZERO_WORD);
        });

        it('returns a zero word for an address with no code or storage', async () => {
            const fresh = ethers.Wallet.createRandom().address;
            expect(await at(fresh, '0x0')).to.equal(ZERO_WORD);
        });
    });

    describe('historical state', () => {
        it('reflects the pre-seed (empty) value before the mint and the seeded value after', async () => {
            const slot = mappingSlot(scene.holder, SLOT_BALANCEOF);
            const [before, after] = await Promise.all([
                at(scene.erc20, slot, ethers.toQuantity(scene.preSeedBlock)),
                at(scene.erc20, slot, ethers.toQuantity(scene.seededBlock)),
            ]);
            expect(before, 'balance was zero before the mint').to.equal(ZERO_WORD);
            expect(after, 'seeded balance afterwards').to.equal(storageWord(scene.holderBalance));
        });

        it('agrees across latest / pending / safe / finalized for a settled slot', async () => {
            const slot = ethers.toQuantity(SLOT_OWNER);
            const tags = ['latest', 'pending', 'safe', 'finalized'] as const;
            const words = await Promise.all(
                tags.map(t => rawSei<string>('eth_getStorageAt', [scene.erc20, slot, t])),
            );
            words.forEach((res, i) =>
                expect(res.error, `${tags[i]}: ${JSON.stringify(res.error)}`).to.equal(undefined),
            );
            const latest = words[0].result;
            words.forEach((res, i) =>
                expect(res.result, `${tags[i]} == latest`).to.equal(latest),
            );
        });
    });

    describe('block specifiers (EIP-1898)', () => {
        it('a blockNumber object matches the numeric tag', async () => {
            const slot = mappingSlot(scene.holder, SLOT_BALANCEOF);
            const tag = ethers.toQuantity(scene.seededBlock);
            const [viaTag, viaObject] = await Promise.all([
                rawSei<string>('eth_getStorageAt', [scene.erc20, slot, tag]),
                rawSei<string>('eth_getStorageAt', [scene.erc20, slot, { blockNumber: tag }]),
            ]);
            expect(viaObject.result, 'object == numeric tag').to.equal(viaTag.result);
            expect(viaObject.result).to.equal(storageWord(scene.holderBalance));
        });

        it('a blockHash object matches the numeric tag', async () => {
            const block = await sei.getBlock(scene.seededBlock);
            expect(block, 'seeded block exists').to.not.equal(null);
            const slot = mappingSlot(scene.holder, SLOT_BALANCEOF);
            const [viaNumber, viaHash] = await Promise.all([
                rawSei<string>('eth_getStorageAt', [
                    scene.erc20,
                    slot,
                    ethers.toQuantity(scene.seededBlock),
                ]),
                rawSei<string>('eth_getStorageAt', [scene.erc20, slot, { blockHash: block!.hash! }]),
            ]);
            expect(viaHash.result, 'blockHash object == numeric tag').to.equal(viaNumber.result);
        });
    });

    describe('geth compatibility (canonical 32-byte data)', () => {
        it('an empty slot returns the same 32-byte zero word on Sei and geth', async () => {
            const [s, g] = await Promise.all([
                rawSei<string>('eth_getStorageAt', [runtime.contracts.erc20, UNUSED_SLOT, 'latest']),
                rawGeth<string>('eth_getStorageAt', [
                    runtime.contracts.erc20Geth,
                    UNUSED_SLOT,
                    'latest',
                ]),
            ]);
            expect(s.result, 'Sei: empty slot is a zero word').to.equal(ZERO_WORD);
            expect(g.result, 'geth: empty slot is a zero word').to.equal(ZERO_WORD);
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {

        it('empty params fail identically (-32602, missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getStorageAt', []),
                rawGeth('eth_getStorageAt', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('omitting the storage key fails identically (-32602, missing required argument 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getStorageAt', [seiAdmin]),
                rawGeth('eth_getStorageAt', [gethAdmin]),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 1/);
            expectSameError(s, g);
        });

        it('omitting the block fails identically (-32602, missing required argument 2)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getStorageAt', [seiAdmin, '0x0']),
                rawGeth('eth_getStorageAt', [gethAdmin, '0x0']),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 2/);
            expectSameError(s, g);
        });

        it('too many positional args fail identically (-32602, want at most 3)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getStorageAt', [seiAdmin, '0x0', 'latest', {}]),
                rawGeth('eth_getStorageAt', [gethAdmin, '0x0', 'latest', {}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 3/);
            expectSameError(s, g);
        });

        it('a malformed (too short) address fails identically (-32602, exact length message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getStorageAt', ['0x1234', '0x0', 'latest']),
                rawGeth('eth_getStorageAt', ['0x1234', '0x0', 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /hex string has length 4, want 40 for common\.Address/);
            expectSameError(s, g);
        });

        it('non-array params fail identically (-32602, non-array args)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getStorageAt', { address: seiAdmin }),
                rawGeth('eth_getStorageAt', { address: gethAdmin }),
            ]);
            expectJsonRpcError(s, -32602, /^non-array args$/);
            expectSameError(s, g);
        });
    });
});
