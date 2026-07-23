/**
 * addr precompile (0x…1004) — end-to-end association semantics against a live Sei chain.
 *
 * Association permanently links an EVM address to its pubkey-derived sei address, so
 * every positive case uses a fresh random wallet rather than consuming a pool slot.
 * The admin signs the transactions; the precompile derives the target account from
 * the signature / pubkey argument, so the target needs no funds.
 * Sections: happy path & state parity / error handling / dispatch semantics.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    expectExecutionReverted,
    expectTraceRevertedNotPanicked,
} from '../utils/precompileUtils';
import { generateSeiAddress } from '../utils/cosmosUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';
import { SEI_ADDRESS } from '../utils/format';

/** The canonical association message users sign (mirrors the legacy hardhat suite). */
const ASSOCIATE_MESSAGE =
    'Please sign this message to link your EVM and Sei addresses. No SEI will be spent as a result of this signature.\n\n';

/** EIP-191 envelope the precompile hashes: "\x19Ethereum Signed Message:\n<len><message>". */
const eip191Envelope = (message: string): string =>
    `\x19Ethereum Signed Message:\n${Buffer.from(message, 'utf8').length}${message}`;

/** v/r/s in the precompile's expected shape: v is the 0/1 recovery id as hex. */
const signatureParts = async (wallet: EvmAccount, message: string) => {
    const sig = ethers.Signature.from(await wallet.wallet.signMessage(message));
    return { v: `0x${sig.v - 27}`, r: sig.r, s: sig.s };
};

describe('addr precompile (0x1004)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const addrIface = precompileInterface('addr');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let addr: ethers.Contract;
    let caller: ethers.Contract;

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        addr = precompileContract('addr', admin.wallet);
        caller = new ethers.Contract(
            runtime.contracts.precompileCaller,
            [
                'function callTarget(address target, bytes data) payable returns (bytes)',
                'function staticcallTarget(address target, bytes data) view returns (bytes)',
                'function delegatecallTarget(address target, bytes data) returns (bytes)',
            ],
            admin.wallet,
        );
    });

    describe('happy path & state parity', () => {
        it('getSeiAddr returns the admin’s pubkey-derived sei address', async () => {
            const seiAddr: string = await addr.getSeiAddr(admin.address);
            expect(seiAddr).to.match(SEI_ADDRESS);
            expect(seiAddr, 'must match the client-side derivation from the same pubkey').to.equal(
                runtime.funded.adminSeiAddress,
            );
        });

        it('getEvmAddr round-trips back to the admin’s EVM address', async () => {
            const evmAddr: string = await addr.getEvmAddr(runtime.funded.adminSeiAddress);
            expect(ethers.getAddress(evmAddr)).to.equal(ethers.getAddress(admin.address));
        });

        it('associate links a fresh wallet via a v/r/s signature', async () => {
            const fresh = EvmAccount.random(provider);
            await expectExecutionReverted(
                addr.getSeiAddr(fresh.address),
                'getSeiAddr before association',
            );

            const { v, r, s } = await signatureParts(fresh, ASSOCIATE_MESSAGE);
            const tx = await addr.associate(v, r, s, eip191Envelope(ASSOCIATE_MESSAGE));
            const receipt = await tx.wait();
            expect(receipt!.status, 'associate tx must succeed').to.equal(1);

            const seiAddr: string = await addr.getSeiAddr(fresh.address);
            expect(seiAddr, 'on-chain association must match the pubkey derivation').to.equal(
                fresh.seiAddress(),
            );
        });

        it('associatePubKey links a fresh wallet from its compressed pubkey', async () => {
            const fresh = EvmAccount.random(provider);
            await expectExecutionReverted(
                addr.getSeiAddr(fresh.address),
                'getSeiAddr before association',
            );

            const tx = await addr.associatePubKey(fresh.compressedPubKeyHex());
            const receipt = await tx.wait();
            expect(receipt!.status, 'associatePubKey tx must succeed').to.equal(1);

            const seiAddr: string = await addr.getSeiAddr(fresh.address);
            expect(seiAddr).to.equal(fresh.seiAddress());

            const evmAddr: string = await addr.getEvmAddr(seiAddr);
            expect(ethers.getAddress(evmAddr), 'reverse lookup must round-trip').to.equal(
                ethers.getAddress(fresh.address),
            );
        });
    });

    describe('error handling', () => {
        it('getSeiAddr rejects an unassociated EVM address', async () => {
            const message = await expectExecutionReverted(
                addr.getSeiAddr(EvmAccount.random(provider).address),
                'getSeiAddr for an unassociated address',
            );
            // Surface the precompile's reason when the node carries it through.
            if (/not associated/i.test(message)) {
                expect(message).to.match(/is not associated/);
            }
        });

        it('getEvmAddr rejects a valid but unassociated sei address', async () => {
            const neverAssociated = await generateSeiAddress();
            await expectExecutionReverted(
                addr.getEvmAddr(neverAssociated),
                'getEvmAddr for a never-associated sei address',
            );
        });

        it('getEvmAddr rejects a malformed bech32', async () => {
            await expectExecutionReverted(
                addr.getEvmAddr('sei1not-a-real-address'),
                'getEvmAddr for a malformed bech32',
            );
        });

        it('associate rejects an already-associated account', async () => {
            const { v, r, s } = await signatureParts(admin, ASSOCIATE_MESSAGE);
            await expectExecutionReverted(
                addr.associate.staticCall(v, r, s, eip191Envelope(ASSOCIATE_MESSAGE)),
                'associate for the already-associated admin',
            );
        });

        it('associate rejects non-hex signature components', async () => {
            await expectExecutionReverted(
                addr.associate.staticCall('0x0', 'zz-not-hex', '0x1234', 'message'),
                'associate with a non-hex r component',
            );
        });

        it('view methods reject value (non-payable)', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.addr,
                    data: addrIface.encodeFunctionData('getSeiAddr', [admin.address]),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'getSeiAddr with value must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
        });

        it('out-of-gas surfaces as "execution reverted", never as a panic (legacy guard)', async () => {
            const fresh = EvmAccount.random(provider);
            // 52k gas covers the intrinsic tx cost but leaves too little for the
            // association's state writes, so the precompile runs out of gas mid-call.
            const tx = await addr.associatePubKey(fresh.compressedPubKeyHex(), {
                gasLimit: 52_000,
            });
            const receipt = await tx.wait().catch((e: any) => e.receipt);
            expect(receipt, 'the failing tx must still be mined').to.not.equal(undefined);
            expect(receipt.status, 'tx must fail').to.equal(0);
            await expectTraceRevertedNotPanicked(receipt.hash);
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('a real CALL from contract bytecode reaches the precompile', async () => {
            const data = addrIface.encodeFunctionData('getSeiAddr', [admin.address]);
            const ret: string = await caller.callTarget.staticCall(
                PRECOMPILE_ADDRESSES.addr,
                data,
            );
            const [decoded] = addrIface.decodeFunctionResult('getSeiAddr', ret);
            expect(decoded).to.equal(runtime.funded.adminSeiAddress);
        });

        it('view methods are callable via STATICCALL', async () => {
            const data = addrIface.encodeFunctionData('getEvmAddr', [
                runtime.funded.adminSeiAddress,
            ]);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.addr,
                data,
            );
            const [decoded] = addrIface.decodeFunctionResult('getEvmAddr', ret);
            expect(ethers.getAddress(decoded)).to.equal(ethers.getAddress(admin.address));
        });

        it('associate methods are rejected under STATICCALL (readOnly guard)', async () => {
            const fresh = EvmAccount.random(provider);
            const data = addrIface.encodeFunctionData('associatePubKey', [
                fresh.compressedPubKeyHex(),
            ]);
            await expectExecutionReverted(
                caller.staticcallTarget.staticCall(PRECOMPILE_ADDRESSES.addr, data),
                'addr.associatePubKey via STATICCALL',
            );
        });
    });
});
