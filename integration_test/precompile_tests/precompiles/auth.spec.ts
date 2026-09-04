/**
 * auth precompile (0x…100D) — account queries against a live Sei chain.
 *
 * All four methods are views. The parity oracle is the auth module's own Query
 * service; the Go executor has no delegatecall guard (unlike staking/gov), so
 * CALL, STATICCALL and DELEGATECALL all succeed.
 *
 * `nextAccountNumber` queries a keeper method that increments the persisted
 * counter, which the executor neutralises by branching a CacheContext. That
 * discard is not observable from here — an `eth_call` commits nothing either
 * way — so it stays pinned by auth_test.go:TestNextAccountNumber, and this
 * spec only asserts the value the chain actually reports.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    authAccount,
    authAccounts,
    authNextAccountNumber,
    authParams,
    baseAccount,
} from '../utils/moduleQueries';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

const empty = new Uint8Array();

const uint64 = (v: string | number | bigint | undefined): bigint => BigInt(v ?? 0);

describe('auth precompile (0x100D)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const authIface = precompileInterface('auth');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let auth: ethers.Contract;
    let caller: ethers.Contract;

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        auth = precompileContract('auth', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
    });

    describe('happy path & state parity', () => {
        it('account(admin) matches the module address, account_number and sequence', async () => {
            const sei = admin.seiAddress();
            const [via, stored] = await Promise.all([
                auth.account(admin.address),
                authAccount(sei),
            ]);
            expect(stored, 'the auth module must know the admin account').to.not.equal(undefined);
            const expected = baseAccount(stored!);
            expect(via.accountAddress).to.equal(sei);
            expect(via.accountAddress).to.equal(expected.address);
            expect(via.accountNumber).to.equal(uint64(expected.account_number));
            expect(via.sequence).to.equal(uint64(expected.sequence));
        });

        it('accounts(empty) returns the same first page as the auth module', async () => {
            const [page, stored] = await Promise.all([auth.accounts(empty), authAccounts()]);
            expect(page.accounts.length, 'first page of auth accounts').to.be.greaterThan(0);
            // Both read the first page with the module's default page size, so the
            // rows must line up in order, not merely in count.
            expect(
                [...page.accounts].map((a: { accountAddress: string }) => a.accountAddress),
                'accounts() first page vs the auth module',
            ).to.deep.equal(stored.map(a => baseAccount(a).address));
        });

        it('params() matches the auth module', async () => {
            const [via, p] = await Promise.all([auth.params(), authParams()]);
            expect(p, 'the auth module must report params').to.not.equal(undefined);
            expect(via.maxMemoCharacters).to.equal(uint64(p!.max_memo_characters));
            expect(via.txSigLimit).to.equal(uint64(p!.tx_sig_limit));
            expect(via.txSizeCostPerByte).to.equal(uint64(p!.tx_size_cost_per_byte));
            expect(via.sigVerifyCostEd25519).to.equal(uint64(p!.sig_verify_cost_ed25519));
            expect(via.sigVerifyCostSecp256k1).to.equal(uint64(p!.sig_verify_cost_secp256k1));
            expect(via.disableSeqnoCheck).to.equal(Boolean(p!.disable_seqno_check));
        });

        it('nextAccountNumber() matches the auth module and is past the admin account', async () => {
            const [count, next, account] = await Promise.all([
                auth.nextAccountNumber() as Promise<bigint>,
                authNextAccountNumber(),
                auth.account(admin.address),
            ]);
            expect(count, 'nextAccountNumber vs the auth module').to.equal(uint64(next));
            expect(count > account.accountNumber, 'next number is past every issued number').to.equal(
                true,
            );
        });
    });

    describe('error handling', () => {
        it('account of a never-associated random EVM address reverts (eth_call)', async () => {
            await expectExecutionReverted(
                auth.account(EvmAccount.random(provider).address),
                'auth.account of a never-associated EVM address',
            );
        });

        it('view methods reject value (eth_call with value 0x1)', async () => {
            const cases: Array<[string, unknown[]]> = [
                ['account', [admin.address]],
                ['accounts', [empty]],
                ['params', []],
                ['nextAccountNumber', []],
            ];
            for (const [method, args] of cases) {
                const envelope = await rawSei('eth_call', [
                    {
                        from: admin.address,
                        to: PRECOMPILE_ADDRESSES.auth,
                        data: authIface.encodeFunctionData(method, args),
                        value: '0x1',
                    },
                    'latest',
                ]);
                expect(envelope.error, `${method} with value must revert`).to.not.equal(undefined);
                expect(envelope.error!.message).to.match(/execution reverted|revert/i);
            }
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('account and params respond through a real CALL from contract bytecode', async () => {
            const accountData = authIface.encodeFunctionData('account', [admin.address]);
            const paramsData = authIface.encodeFunctionData('params', []);
            const [accountRet, paramsRet, directAccount, directParams] = await Promise.all([
                caller.callTarget.staticCall(PRECOMPILE_ADDRESSES.auth, accountData) as Promise<string>,
                caller.callTarget.staticCall(PRECOMPILE_ADDRESSES.auth, paramsData) as Promise<string>,
                auth.account(admin.address),
                auth.params(),
            ]);
            const [decodedAccount] = authIface.decodeFunctionResult('account', accountRet);
            const [decodedParams] = authIface.decodeFunctionResult('params', paramsRet);
            expect(decodedAccount.accountAddress).to.equal(directAccount.accountAddress);
            expect(decodedAccount.accountNumber).to.equal(directAccount.accountNumber);
            expect(decodedAccount.sequence).to.equal(directAccount.sequence);
            expect(decodedParams.maxMemoCharacters).to.equal(directParams.maxMemoCharacters);
            expect(decodedParams.txSigLimit).to.equal(directParams.txSigLimit);
        });

        it('account and params respond under STATICCALL', async () => {
            const accountData = authIface.encodeFunctionData('account', [admin.address]);
            const paramsData = authIface.encodeFunctionData('params', []);
            const [accountRet, paramsRet, directAccount, directParams] = await Promise.all([
                caller.staticcallTarget.staticCall(
                    PRECOMPILE_ADDRESSES.auth,
                    accountData,
                ) as Promise<string>,
                caller.staticcallTarget.staticCall(
                    PRECOMPILE_ADDRESSES.auth,
                    paramsData,
                ) as Promise<string>,
                auth.account(admin.address),
                auth.params(),
            ]);
            const [decodedAccount] = authIface.decodeFunctionResult('account', accountRet);
            const [decodedParams] = authIface.decodeFunctionResult('params', paramsRet);
            expect(decodedAccount.accountAddress).to.equal(directAccount.accountAddress);
            expect(decodedParams.maxMemoCharacters).to.equal(directParams.maxMemoCharacters);
        });

        it('responds under DELEGATECALL (auth has no delegatecall guard)', async () => {
            const data = authIface.encodeFunctionData('account', [admin.address]);
            const ret: string = await caller.delegatecallTarget.staticCall(
                PRECOMPILE_ADDRESSES.auth,
                data,
            );
            const [decoded] = authIface.decodeFunctionResult('account', ret);
            expect(decoded.accountAddress).to.equal(admin.seiAddress());
        });
    });
});
