/**
 * bank precompile (0x…1001) — end-to-end semantics against a live Sei chain.
 *
 * The parity oracle is the chain's own bank module (Cosmos-side queries via
 * utils/cosmosUtils): every EVM-side effect and precompile-reported value is
 * asserted against it. Sections: happy path & state parity / error handling /
 * dispatch semantics (real CALL / STATICCALL / DELEGATECALL through the
 * PrecompileCaller fixture — the path Go unit tests cannot exercise).
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei, waitUntil } from '../utils/chainUtils';
import { EvmAccount, associateViaTx } from '../utils/evmUtils';
import { bankBalance, bankSupplyOf, generateSeiAddress } from '../utils/cosmosUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    expectExecutionReverted,
    expectTraceRevertedNotPanicked,
} from '../utils/precompileUtils';
import { readRuntimeState, claimPool, RuntimeState } from '../utils/testUtils';
import { WEI_PER_USEI } from '../utils/constants';

describe('bank precompile (0x1001)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const bankIface = precompileInterface('bank');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let bank: ethers.Contract;
    let caller: ethers.Contract;

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        bank = precompileContract('bank', admin.wallet);
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
        it('balance(admin, usei) matches the Cosmos bank balance and eth_getBalance', async () => {
            // Serial suite and nothing transacts from the admin between these reads,
            // so all three views of the same balance must agree exactly.
            const [viaPrecompile, viaCosmos, viaEvm] = await Promise.all([
                bank.balance(admin.address, 'usei') as Promise<bigint>,
                bankBalance(runtime.funded.adminSeiAddress),
                provider.getBalance(admin.address),
            ]);
            expect(viaPrecompile, 'precompile vs cosmos bank').to.equal(viaCosmos);
            expect(viaPrecompile, 'precompile (usei) vs eth_getBalance (wei, dust floored)').to.equal(
                viaEvm / WEI_PER_USEI,
            );
        });

        it('balance falls back to the cast address for an unassociated account', async () => {
            // Pool accounts are funded via EVM sends but have never signed, so their
            // funds sit at the cast sei address; the precompile must still report them.
            const [unassociated] = claimPool(runtime, provider, 1, 'bank:cast-balance');
            const [viaPrecompile, viaEvm] = await Promise.all([
                bank.balance(unassociated.address, 'usei') as Promise<bigint>,
                provider.getBalance(unassociated.address),
            ]);
            expect(viaPrecompile).to.equal(viaEvm / WEI_PER_USEI);
            expect(viaPrecompile > 0n, 'pool account must be funded').to.equal(true);
        });

        it('all_balances includes the usei coin with the same amount as balance', async () => {
            const [balances, useiBalance] = await Promise.all([
                bank.all_balances(admin.address) as Promise<Array<{ amount: bigint; denom: string }>>,
                bank.balance(admin.address, 'usei') as Promise<bigint>,
            ]);
            const usei = balances.find(c => c.denom === 'usei');
            expect(usei, 'all_balances must contain a usei entry').to.not.equal(undefined);
            expect(usei!.amount).to.equal(useiBalance);
        });

        it('supply(usei) matches the Cosmos bank total supply', async () => {
            // Supply can only grow between reads (block rewards / mint); bracket the
            // precompile read with two Cosmos reads instead of asserting instant equality.
            const before = await bankSupplyOf('usei');
            const viaPrecompile: bigint = await bank.supply('usei');
            const after = await bankSupplyOf('usei');
            expect(viaPrecompile >= before, `supply ${viaPrecompile} >= cosmos-before ${before}`).to.equal(true);
            expect(viaPrecompile <= after, `supply ${viaPrecompile} <= cosmos-after ${after}`).to.equal(true);
        });

        it('decimals(usei) is the constant 0 (native denoms are integer-based)', async () => {
            expect(await bank.decimals('usei')).to.equal(0n);
        });

        it('name and symbol behave consistently with the denom metadata state', async () => {
            // Fresh devnets may not register usei metadata; both methods must then agree
            // (both revert with "not found") — a split answer would be a real bug.
            const results = await Promise.allSettled([bank.name('usei'), bank.symbol('usei')]);
            const [name, symbol] = results;
            expect(
                name.status,
                'name and symbol must agree on whether usei metadata exists',
            ).to.equal(symbol.status);
            if (name.status === 'fulfilled') {
                expect(name.value).to.be.a('string');
                expect((symbol as PromiseFulfilledResult<unknown>).value).to.be.a('string');
            }
        });

        it('sendNative moves funds to a fresh cosmos account, verified via the bank module', async () => {
            const [sender] = claimPool(runtime, provider, 1, 'bank:sendNative');
            // sendNative resolves the caller through its association, so reveal the
            // sender's pubkey first with a 0-value self-send.
            await associateViaTx(sender);

            const recipient = await generateSeiAddress();
            expect(await bankBalance(recipient), 'recipient must start empty').to.equal(0n);

            const sendWei = ethers.parseEther('1'); // == 10^6 usei exactly, no wei dust
            const senderBefore = await sender.balance();

            const tx = await (bank.connect(sender.wallet) as ethers.Contract).sendNative(recipient, {
                value: sendWei,
            });
            const receipt = await tx.wait();
            expect(receipt!.status, 'sendNative tx must succeed').to.equal(1);

            const received = await waitUntil(
                async () => {
                    const b = await bankBalance(recipient);
                    return b > 0n ? b : null;
                },
                { timeoutMs: 30_000, label: 'recipient cosmos balance after sendNative' },
            );
            expect(received, 'recipient receives the exact usei amount').to.equal(
                sendWei / WEI_PER_USEI,
            );

            const senderAfter = await sender.balance();
            const gasCost: bigint = receipt!.fee; // gasUsed * gasPrice
            expect(senderAfter, 'sender pays value + gas exactly').to.equal(
                senderBefore - sendWei - gasCost,
            );
        });
    });

    describe('error handling', () => {
        it('send rejects non-pointer callers (EOAs cannot move arbitrary denoms)', async () => {
            // send is reserved for the denom's registered ERC20 pointer contract.
            await expectExecutionReverted(
                bank.send.staticCall(admin.address, admin.address, 'usei', 1n),
                'bank.send from an EOA',
            );
        });

        it('sendNative rejects a zero value', async () => {
            await expectExecutionReverted(
                bank.sendNative.staticCall(runtime.funded.adminSeiAddress, { value: 0n }),
                'bank.sendNative with value 0',
            );
        });

        it('sendNative rejects an unassociated caller', async () => {
            const [unassociated] = claimPool(runtime, provider, 1, 'bank:unassociated-sender');
            await expectExecutionReverted(
                (bank.connect(unassociated.wallet) as ethers.Contract).sendNative.staticCall(
                    runtime.funded.adminSeiAddress,
                    { value: WEI_PER_USEI },
                ),
                'bank.sendNative from an unassociated caller',
            );
        });

        it('sendNative rejects a malformed bech32 recipient', async () => {
            await expectExecutionReverted(
                bank.sendNative.staticCall('not-a-bech32-address', { value: WEI_PER_USEI }),
                'bank.sendNative with a malformed recipient',
            );
        });

        it('balance rejects an empty denom', async () => {
            await expectExecutionReverted(
                bank.balance.staticCall(admin.address, ''),
                'bank.balance with empty denom',
            );
        });

        it('view methods reject value (non-payable)', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.bank,
                    data: bankIface.encodeFunctionData('balance', [admin.address, 'usei']),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'balance with value must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
        });

        it('out-of-gas surfaces as "execution reverted", never as a panic (legacy guard)', async () => {
            const [sender] = claimPool(runtime, provider, 1, 'bank:out-of-gas');
            await associateViaTx(sender);

            // 40k gas covers the intrinsic cost but starves the precompile mid-execution.
            const tx = await (bank.connect(sender.wallet) as ethers.Contract).sendNative(
                runtime.funded.adminSeiAddress,
                { value: WEI_PER_USEI, gasLimit: 40_000 },
            );
            const receipt = await tx.wait().catch((e: any) => e.receipt);
            expect(receipt, 'the failing tx must still be mined').to.not.equal(undefined);
            expect(receipt.status, 'tx must fail').to.equal(0);
            await expectTraceRevertedNotPanicked(receipt.hash);
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('a real CALL from contract bytecode reaches the precompile', async () => {
            const data = bankIface.encodeFunctionData('balance', [admin.address, 'usei']);
            const [viaContract, direct] = await Promise.all([
                caller.callTarget.staticCall(PRECOMPILE_ADDRESSES.bank, data) as Promise<string>,
                bank.balance(admin.address, 'usei') as Promise<bigint>,
            ]);
            const [decoded] = bankIface.decodeFunctionResult('balance', viaContract);
            expect(decoded).to.equal(direct);
        });

        it('view methods are callable via STATICCALL', async () => {
            const data = bankIface.encodeFunctionData('balance', [admin.address, 'usei']);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.bank,
                data,
            );
            const [decoded] = bankIface.decodeFunctionResult('balance', ret);
            expect(decoded).to.equal(await bank.balance(admin.address, 'usei'));
        });

        it('send is rejected under STATICCALL (readOnly guard)', async () => {
            const data = bankIface.encodeFunctionData('send', [
                admin.address,
                admin.address,
                'usei',
                1n,
            ]);
            await expectExecutionReverted(
                caller.staticcallTarget.staticCall(PRECOMPILE_ADDRESSES.bank, data),
                'bank.send via STATICCALL',
            );
        });

        it('sendNative is rejected under DELEGATECALL', async () => {
            const data = bankIface.encodeFunctionData('sendNative', [
                runtime.funded.adminSeiAddress,
            ]);
            await expectExecutionReverted(
                caller.delegatecallTarget.staticCall(PRECOMPILE_ADDRESSES.bank, data),
                'bank.sendNative via DELEGATECALL',
            );
        });
    });
});
