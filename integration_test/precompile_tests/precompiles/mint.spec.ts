/**
 * mint precompile (0x…1012) — query surface over Sei's custom mint module.
 *
 * Two views: params() and minter(). The backing module is Sei's own
 * seiprotocol.seichain.mint, not cosmos-sdk x/mint, so the parity oracle is
 * that module's LCD routes rather than a /cosmos/... path.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { cosmosRest } from '../utils/cosmosUtils';
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

// The gRPC-gateway routes registered by x/mint (see the patterns in
// x/mint/types/query.pb.gw.go). Sei's mint module keeps the bare `seichain`
// prefix rather than the `seiprotocol.seichain` proto package path.
const PARAMS_LCD_PATH = '/seichain/mint/v1beta1/params';
const MINTER_LCD_PATH = '/seichain/mint/v1beta1/minter';

function asBigInt(v: unknown): bigint {
    if (typeof v === 'bigint') return v;
    if (typeof v === 'number') return BigInt(v);
    if (typeof v === 'string' && v !== '') return BigInt(v);
    return 0n;
}

function lcdMintDenom(body: any): string | undefined {
    const p = body?.params ?? body;
    const denom = p?.mint_denom ?? p?.mintDenom;
    return typeof denom === 'string' ? denom : undefined;
}

function lcdSchedule(body: any): any[] {
    const p = body?.params ?? body;
    const s = p?.token_release_schedule ?? p?.tokenReleaseSchedule;
    return Array.isArray(s) ? s : [];
}

/**
 * QueryMinterResponse is flat — it carries the minter's fields at the top
 * level rather than nesting them under a `minter` key the way the params
 * response nests under `params`.
 */
function lcdMinter(body: any): any {
    return body ?? {};
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
            const [params, lcd] = await Promise.all([
                mint.params() as Promise<MintParams>,
                cosmosRest<any>(PARAMS_LCD_PATH),
            ]);
            expect(params.mintDenom, 'precompile mintDenom vs LCD').to.equal(lcdMintDenom(lcd));
            expect(params.mintDenom, 'devnet genesis mints the bond denom').to.equal('usei');

            const schedule = [...params.tokenReleaseSchedule];
            const lcdRows = lcdSchedule(lcd);
            expect(schedule.length, 'tokenReleaseSchedule length vs LCD').to.equal(lcdRows.length);
            for (let i = 0; i < schedule.length; i++) {
                const row = lcdRows[i];
                expect(schedule[i].startDate).to.equal(String(row.start_date ?? row.startDate ?? ''));
                expect(schedule[i].endDate).to.equal(String(row.end_date ?? row.endDate ?? ''));
                expect(schedule[i].tokenReleaseAmount).to.equal(
                    asBigInt(row.token_release_amount ?? row.tokenReleaseAmount),
                );
            }
        });

        it('minter() matches the mint module', async () => {
            const [minter, lcd] = await Promise.all([
                mint.minter() as Promise<Minter>,
                cosmosRest<any>(MINTER_LCD_PATH),
            ]);
            const body = lcdMinter(lcd);

            expect(minter.denom).to.equal(String(body.denom ?? ''));
            expect(minter.startDate).to.equal(String(body.start_date ?? body.startDate ?? ''));
            expect(minter.endDate).to.equal(String(body.end_date ?? body.endDate ?? ''));
            expect(minter.totalMintAmount).to.equal(
                asBigInt(body.total_mint_amount ?? body.totalMintAmount),
            );
            expect(minter.remainingMintAmount).to.equal(
                asBigInt(body.remaining_mint_amount ?? body.remainingMintAmount),
            );
            expect(minter.lastMintAmount).to.equal(
                asBigInt(body.last_mint_amount ?? body.lastMintAmount),
            );
            expect(minter.lastMintDate).to.equal(
                String(body.last_mint_date ?? body.lastMintDate ?? ''),
            );
            expect(minter.lastMintHeight).to.equal(
                asBigInt(body.last_mint_height ?? body.lastMintHeight),
            );
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
