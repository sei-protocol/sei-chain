/**
 * pointer precompile (0x…100b) — wasm-free subset (addNativePointer).
 *
 * Fixture: a tokenfactory denom created from the admin's cosmos key —
 * x/tokenfactory sets bank denom metadata automatically on creation, which is
 * exactly what addNativePointer requires. The CW-pointer methods (addCW20/721/
 * 1155Pointer) need live CosmWasm contracts and belong to the future wasm-gated
 * phase; only their cheap wasm-free negative paths are probed here.
 *
 * addNativePointer has upsert semantics: re-registering the same denom
 * SUCCEEDS and keeps the pointer address stable — pinned below; do not add a
 * "duplicate registration reverts" test.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, waitUntil } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import { createTokenfactoryDenom } from '../utils/cosmosUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
    expectVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

const ERC20_ABI = [
    'function name() view returns (string)',
    'function symbol() view returns (string)',
    'function decimals() view returns (uint8)',
];

describe('pointer precompile (0x100b)', function () {
    this.timeout(180 * 1000);

    const provider = seiRpc();
    const pointerIface = precompileInterface('pointer');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let pointer: ethers.Contract;
    let pointerview: ethers.Contract;
    let caller: ethers.Contract;
    let denom: string;

    before(async () => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        pointer = precompileContract('pointer', admin.wallet);
        pointerview = precompileContract('pointerview', provider);
        caller = callerContract(runtime, admin.wallet);
        // Unique subdenom so reruns against a long-lived devnet never collide.
        denom = await createTokenfactoryDenom(
            runtime.funded.adminMnemonic,
            `ptr${Date.now().toString(36)}`,
        );
    });

    describe('happy path & state parity', () => {
        let pointerAddress: string;

        it('addNativePointer deploys an ERC20 pointer for a metadata-backed denom', async () => {
            // eth_call runs readOnly=false at top level, so staticCall predicts
            // the pointer address without persisting anything.
            pointerAddress = await pointer.addNativePointer.staticCall(denom);
            expect(pointerAddress).to.match(/^0x[0-9a-fA-F]{40}$/);

            const tx = await pointer.addNativePointer(denom, { gasLimit: 5_000_000 });
            const receipt = await tx.wait();
            expect(receipt!.status, 'addNativePointer tx must succeed').to.equal(1);

            const registered = await waitUntil(
                async () => {
                    const [addr, , exists] = await pointerview.getNativePointer(denom);
                    return exists ? addr : null;
                },
                { timeoutMs: 15_000, label: 'native pointer registered' },
            );
            expect(registered.toLowerCase()).to.equal(pointerAddress.toLowerCase());
        });

        it('the deployed pointer is a live ERC20 whose metadata mirrors the denom', async () => {
            const erc20 = new ethers.Contract(pointerAddress, ERC20_ABI, provider);
            // Tokenfactory metadata: name/symbol are the FULL factory/… denom
            // string and the single denom unit has exponent 0.
            expect(await erc20.name()).to.equal(denom);
            expect(await erc20.symbol()).to.equal(denom);
            expect(await erc20.decimals()).to.equal(0n);
        });

        it('re-registering the same denom upserts and keeps the address stable', async () => {
            const again: string = await pointer.addNativePointer.staticCall(denom);
            expect(again.toLowerCase()).to.equal(pointerAddress.toLowerCase());

            const tx = await pointer.addNativePointer(denom, { gasLimit: 5_000_000 });
            expect((await tx.wait())!.status, 'upsert must succeed').to.equal(1);

            const [addr, , exists] = await pointerview.getNativePointer(denom);
            expect(exists).to.equal(true);
            expect(addr.toLowerCase()).to.equal(pointerAddress.toLowerCase());
        });
    });

    describe('error handling', () => {
        it('addNativePointer rejects a denom without bank metadata (usei)', async () => {
            await expectVmError(
                pointer.addNativePointer('usei', { gasLimit: 5_000_000 }),
                'does not have metadata stored',
            );
        });

        it('addCW20Pointer rejects a malformed bech32 contract address', async () => {
            await expectExecutionReverted(
                pointer.addCW20Pointer.staticCall('not-a-contract'),
                'pointer.addCW20Pointer with a malformed address',
            );
        });

        it('addCW20Pointer rejects a valid sei address that is not a contract', async () => {
            await expectExecutionReverted(
                pointer.addCW20Pointer.staticCall(runtime.funded.adminSeiAddress),
                'pointer.addCW20Pointer with a non-contract address',
            );
        });

        it('rejects value despite the payable ABI declaration', async () => {
            // The ABI marks add* methods payable but the Go handler rejects any
            // value — the mismatch itself is the behavior under test.
            await expectExecutionReverted(
                pointer.addNativePointer.staticCall(denom, { value: 10n ** 12n }),
                'pointer.addNativePointer with value',
            );
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('is rejected under STATICCALL (readOnly guard)', async () => {
            const data = pointerIface.encodeFunctionData('addNativePointer', [denom]);
            await expectVmError(
                caller.getFunction('staticcallTarget').send(PRECOMPILE_ADDRESSES.pointer, data, {
                    gasLimit: 1_000_000,
                }),
                'cannot call pointer precompile from staticcall',
            );
        });

        it('is rejected under DELEGATECALL', async () => {
            const data = pointerIface.encodeFunctionData('addNativePointer', [denom]);
            await expectVmError(
                caller.getFunction('delegatecallTarget').send(PRECOMPILE_ADDRESSES.pointer, data, {
                    gasLimit: 1_000_000,
                }),
                'cannot delegatecall pointer',
            );
        });
    });
});
