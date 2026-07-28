/**
 * oracle precompile (0x…1008) — retirement assertion.
 *
 * The oracle precompile is retired: both methods unconditionally revert with
 * "oracle precompile is retired; oracle data queries are disabled". Uniquely
 * among Sei precompiles it returns REAL Error(string) revert data, so the
 * reason is visible directly in eth_call errors — this spec pins that too.
 *
 * One ordering nuance: the non-payable check runs BEFORE the retirement
 * revert, so a value-bearing call reverts WITHOUT the retirement reason.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    PRECOMPILE_CALLER_ABI,
    precompileInterface,
    getVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

const RETIRED = /oracle precompile is retired/;

describe('oracle precompile (0x1008)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const oracleIface = precompileInterface('oracle');

    let runtime: RuntimeState;
    let admin: EvmAccount;

    const expectRetired = async (data: string, to: string = PRECOMPILE_ADDRESSES.oracle) => {
        const envelope = await rawSei('eth_call', [{ to, data }, 'latest']);
        expect(envelope.error, 'retired method must revert').to.not.equal(undefined);
        expect(envelope.error!.message, 'retirement reason surfaces in eth_call').to.match(RETIRED);
    };

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
    });

    describe('error handling (retirement)', () => {
        it('getExchangeRates reverts with the retirement reason', async () => {
            await expectRetired(oracleIface.encodeFunctionData('getExchangeRates', []));
        });

        it('getOracleTwaps reverts with the retirement reason', async () => {
            await expectRetired(oracleIface.encodeFunctionData('getOracleTwaps', [3600n]));
        });

        it('a value-bearing call reverts on the non-payable check, without the retirement reason', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.oracle,
                    data: oracleIface.encodeFunctionData('getExchangeRates', []),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'value-bearing call must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
            expect(
                envelope.error!.message,
                'the non-payable check fires before the retirement revert',
            ).to.not.match(RETIRED);
        });

        it('a mined tx fails with the retirement reason in its VmError', async () => {
            const tx = await admin.wallet.sendTransaction({
                to: PRECOMPILE_ADDRESSES.oracle,
                data: oracleIface.encodeFunctionData('getExchangeRates', []),
                gasLimit: 200_000,
            });
            const receipt = await tx.wait().catch((e: any) => e.receipt);
            expect(receipt.status, 'tx must fail').to.equal(0);
            expect(await getVmError(receipt.hash)).to.match(RETIRED);
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('the retirement reason bubbles through CALL, STATICCALL and DELEGATECALL', async () => {
            const inner = oracleIface.encodeFunctionData('getExchangeRates', []);
            const callerIface = new ethers.Interface(PRECOMPILE_CALLER_ABI as unknown as string[]);
            for (const fn of ['callTarget', 'staticcallTarget', 'delegatecallTarget']) {
                await expectRetired(
                    callerIface.encodeFunctionData(fn, [PRECOMPILE_ADDRESSES.oracle, inner]),
                    runtime.contracts.precompileCaller,
                );
            }
        });
    });
});
