/**
 * evidence precompile (0x…100F) — end-to-end query semantics against a live Sei chain.
 *
 * Both methods are views over x/evidence. A clean chain typically stores no
 * evidence, so allEvidence(empty page) is an empty list that matches LCD
 * GET /cosmos/evidence/v1beta1/evidence, and evidence(hash) reverts for a
 * hash that is not on chain. Populated Equivocation rows are injected in the
 * Go unit tests (evidence_test.go) via the keeper and are not repeated here.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';
import { allEvidence } from '../utils/moduleQueries';

const EMPTY_PAGE = new Uint8Array();
/** Nonzero 32-byte hash that will not exist on a clean chain. */
const FAKE_HASH = new Uint8Array(32).fill(0xab);

describe('evidence precompile (0x100F)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const evidenceIface = precompileInterface('evidence');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let evidence: ethers.Contract;
    let caller: ethers.Contract;

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        evidence = precompileContract('evidence', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
    });

    describe('happy path & state parity', () => {
        // Evidence only exists after a validator equivocates, which a healthy
        // devnet never does — but asserting "empty" would turn a genuinely
        // slashed validator into a spurious failure, so the module's own list is
        // the oracle and the count has to agree either way.
        it('allEvidence matches the evidence module', async () => {
            const [resp, stored] = await Promise.all([
                evidence.allEvidence(EMPTY_PAGE),
                allEvidence(),
            ]);
            expect(resp.evidenceList.length, 'allEvidence count vs the module').to.equal(
                stored.length,
            );
            for (const row of resp.evidenceList) {
                // Each entry is the JSON encoding of the stored evidence.
                expect(() => JSON.parse(ethers.toUtf8String(row))).to.not.throw();
            }
        });
    });

    describe('error handling', () => {
        it('evidence with a nonzero fake hash reverts', async () => {
            await expectExecutionReverted(
                evidence.evidence(FAKE_HASH),
                'evidence.evidence with a hash that does not exist',
            );
        });

        it('rejects value on every method (non-payable)', async () => {
            for (const [method, args] of [
                ['allEvidence', [EMPTY_PAGE]],
                ['evidence', [FAKE_HASH]],
            ] as const) {
                const envelope = await rawSei('eth_call', [
                    {
                        from: admin.address,
                        to: PRECOMPILE_ADDRESSES.evidence,
                        data: evidenceIface.encodeFunctionData(method, args),
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
        // Compare each dispatch path against the direct call rather than against
        // a hardcoded empty list, so these stay guards on dispatch instead of
        // re-asserting that the devnet has no evidence.
        it('CALL, STATICCALL and DELEGATECALL all return the direct answer', async () => {
            const data = evidenceIface.encodeFunctionData('allEvidence', [EMPTY_PAGE]);
            const envelope = await rawSei<string>('eth_call', [
                { to: PRECOMPILE_ADDRESSES.evidence, data },
                'latest',
            ]);
            const direct = envelope.result;
            expect(direct, 'direct eth_call must answer').to.not.equal(undefined);
            for (const fn of ['callTarget', 'staticcallTarget', 'delegatecallTarget'] as const) {
                const ret: string = await caller[fn].staticCall(PRECOMPILE_ADDRESSES.evidence, data);
                const [decoded] = evidenceIface.decodeFunctionResult('allEvidence', ret);
                const [expected] = evidenceIface.decodeFunctionResult('allEvidence', direct!);
                expect(
                    decoded.evidenceList.length,
                    `${fn} must return the direct answer`,
                ).to.equal(expected.evidenceList.length);
            }
        });
    });
});
