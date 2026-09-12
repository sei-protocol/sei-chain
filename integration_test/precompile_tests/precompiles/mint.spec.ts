/**
 * mint precompile (0x…1012) — query surface over Sei's custom mint module.
 *
 * Two views: params() and minter(). The backing module is Sei's own
 * seiprotocol.seichain.mint, not cosmos-sdk x/mint, so the parity oracle is
 * that module's Query service rather than a cosmos.* one.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { mintMinter, mintParams } from '../utils/moduleQueries';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

interface TokenRelease {
    startDate: string;
    endDate: string;
    tokenReleaseAmount: bigint;
}

interface MintParams {
    mintDenom: string;
    tokenReleaseSchedule: TokenRelease[];
}

interface Minter {
    startDate: string;
    endDate: string;
    denom: string;
    totalMintAmount: bigint;
    remainingMintAmount: bigint;
    lastMintAmount: bigint;
    lastMintDate: string;
    lastMintHeight: bigint;
}

function asBigInt(v: unknown): bigint {
    if (typeof v === 'bigint') return v;
    if (typeof v === 'number') return BigInt(v);
    if (typeof v === 'string' && v !== '') return BigInt(v);
    return 0n;
}

describe('mint precompile (0x1012)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const mintIface = precompileInterface('mint');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let mint: ethers.Contract;
    let caller: ethers.Contract;

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        mint = precompileContract('mint', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
    });

    describe('happy path & state parity', () => {
        it('params() matches the mint module', async () => {
            const [params, moduleParams] = await Promise.all([
                mint.params() as Promise<MintParams>,
                mintParams(),
            ]);
            expect(params.mintDenom, 'precompile mintDenom vs the mint module').to.equal(
                moduleParams?.mint_denom,
            );
            expect(params.mintDenom, 'devnet genesis mints the bond denom').to.equal('usei');

            const schedule = [...params.tokenReleaseSchedule];
            const moduleRows = moduleParams?.token_release_schedule ?? [];
            expect(schedule.length, 'tokenReleaseSchedule length vs the mint module').to.equal(
                moduleRows.length,
            );
            for (let i = 0; i < schedule.length; i++) {
                const row = moduleRows[i];
                expect(schedule[i].startDate).to.equal(row.start_date);
                expect(schedule[i].endDate).to.equal(row.end_date);
                expect(schedule[i].tokenReleaseAmount).to.equal(asBigInt(row.token_release_amount));
            }
        });

        it('minter() matches the mint module', async () => {
            // QueryMinterResponse is flat — it carries the minter's fields at the
            // top level rather than nesting them under a `minter` key the way the
            // params response nests under `params`.
            const [minter, body] = await Promise.all([mint.minter() as Promise<Minter>, mintMinter()]);

            expect(minter.denom).to.equal(body.denom);
            expect(minter.startDate).to.equal(body.start_date);
            expect(minter.endDate).to.equal(body.end_date);
            expect(minter.totalMintAmount).to.equal(asBigInt(body.total_mint_amount));
            expect(minter.remainingMintAmount).to.equal(asBigInt(body.remaining_mint_amount));
            expect(minter.lastMintAmount).to.equal(asBigInt(body.last_mint_amount));
            expect(minter.lastMintDate).to.equal(body.last_mint_date);
            expect(minter.lastMintHeight).to.equal(asBigInt(body.last_mint_height));
            // Genesis may leave the minter unset; when it is set it mints the bond denom.
            if (minter.denom !== '') {
                expect(minter.denom).to.equal('usei');
            }
        });
    });

    describe('error handling', () => {
        it('rejects value on view methods (non-payable)', async () => {
            for (const method of ['params', 'minter'] as const) {
                const envelope = await rawSei('eth_call', [
                    {
                        from: admin.address,
                        to: PRECOMPILE_ADDRESSES.mint,
                        data: mintIface.encodeFunctionData(method, []),
                        value: '0x1',
                    },
                    'latest',
                ]);
                expect(envelope.error, `${method}: value-bearing call must revert`).to.not.equal(
                    undefined,
                );
                expect(envelope.error!.message).to.match(/execution reverted|revert/i);
            }
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('responds under STATICCALL', async () => {
            const data = mintIface.encodeFunctionData('params', []);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.mint,
                data,
            );
            const [decoded] = mintIface.decodeFunctionResult('params', ret);
            const direct: MintParams = await mint.params();
            expect(decoded.mintDenom).to.equal(direct.mintDenom);
            expect(decoded.mintDenom).to.equal('usei');
        });
    });
});
