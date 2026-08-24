/**
 * gov precompile (0x…1006) — end-to-end semantics against a live Sei chain.
 *
 * Devnet gov params: min_deposit 10 SEI, voting_period 30s, max_deposit_period
 * 100s. The 30-second clock starts the moment min_deposit is reached (usually
 * inside the submit tx itself), so every test that votes creates its OWN fresh
 * proposal and votes immediately — never share a voting-period proposal across
 * tests. EOA votes tally zero power, so every proposal is ultimately rejected
 * at tally; that is what makes these submissions harmless to the devnet.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, waitUntil } from '../utils/chainUtils';
import { EvmAccount, associateViaTx, fundEvm } from '../utils/evmUtils';
import { cosmosQuery } from '../utils/cosmosUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
    expectVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, claimPool, RuntimeState } from '../utils/testUtils';

const MIN_DEPOSIT_WEI = ethers.parseEther('10'); // devnet min_deposit = 10 SEI
const MIN_DEPOSIT_USEI = 10_000_000n;
const VOTING_PERIOD = 2; // PROPOSAL_STATUS_VOTING_PERIOD
const DEPOSIT_PERIOD = 1; // PROPOSAL_STATUS_DEPOSIT_PERIOD
const EMPTY_PAGE_KEY = new Uint8Array();

const authExpiration = (): bigint => BigInt(Math.floor(Date.now() / 1000) + 86400);

/** Cosmjs Duration (`{seconds}`) or a "30s" string → seconds as bigint. */
function durationSeconds(d: unknown): bigint {
    if (d == null) return 0n;
    if (typeof d === 'object' && d !== null && 'seconds' in d) {
        return BigInt(String((d as { seconds: { toString(): string } }).seconds));
    }
    if (typeof d === 'string') {
        const m = /^(\d+)/.exec(d);
        return BigInt(m ? m[1] : 0);
    }
    return BigInt(d as number);
}

const textProposal = (title: string): string =>
    JSON.stringify({ title, description: 'precompile_tests e2e fixture', type: 'Text' });

describe('gov precompile (0x1006)', function () {
    this.timeout(180 * 1000);

    const provider = seiRpc();
    const govIface = precompileInterface('gov');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let gov: ethers.Contract;
    let caller: ethers.Contract;
    let adminSeiAddress: string;

    /**
     * Submit a proposal from the admin and return its id. With value >=
     * min_deposit the proposal enters VOTING_PERIOD inside this very tx. The id
     * is predicted with a staticCall immediately before the send — race-free
     * because the suite is serial and nothing else submits proposals.
     */
    async function submitProposal(json: string, valueWei: bigint): Promise<bigint> {
        const id: bigint = await gov.submitProposal.staticCall(json, { value: valueWei });
        const tx = await gov.submitProposal(json, { value: valueWei, gasLimit: 1_000_000 });
        const receipt = await tx.wait();
        expect(receipt!.status, 'submitProposal tx must succeed').to.equal(1);
        return id;
    }

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        adminSeiAddress = runtime.funded.adminSeiAddress;
        gov = precompileContract('gov', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
    });

    describe('happy path & state parity', () => {
        it('submitProposal with min_deposit enters voting period; vote is recorded by the gov module', async () => {
            const id = await submitProposal(textProposal('e2e vote'), MIN_DEPOSIT_WEI);

            const qc = await cosmosQuery();
            await waitUntil(
                async () => {
                    const { proposal } = await qc.gov.proposal(id.toString());
                    return proposal?.status === VOTING_PERIOD ? proposal : null;
                },
                { timeoutMs: 15_000, label: 'proposal in voting period' },
            );

            // Vote immediately — the 30s voting clock is already running.
            const voteTx = await gov.vote(id, 1, { gasLimit: 500_000 });
            expect((await voteTx.wait())!.status).to.equal(1);

            const recorded = await waitUntil(
                async () => (await qc.gov.vote(id.toString(), adminSeiAddress)).vote ?? null,
                { timeoutMs: 15_000, label: 'vote recorded in gov module' },
            );
            expect(recorded.options.length).to.equal(1);
            expect(recorded.options[0].option, 'VOTE_OPTION_YES').to.equal(1);
        });

        it('voteWeighted splits a vote across options with 18-decimal weights', async () => {
            const id = await submitProposal(textProposal('e2e weighted vote'), MIN_DEPOSIT_WEI);

            const voteTx = await gov.voteWeighted(
                id,
                [
                    { option: 1, weight: '0.6' },
                    { option: 3, weight: '0.4' },
                ],
                { gasLimit: 500_000 },
            );
            expect((await voteTx.wait())!.status).to.equal(1);

            const qc = await cosmosQuery();
            const recorded = await waitUntil(
                async () => (await qc.gov.vote(id.toString(), adminSeiAddress)).vote ?? null,
                { timeoutMs: 15_000, label: 'weighted vote recorded' },
            );
            expect(recorded.options.length).to.equal(2);
            // Weights round-trip as 18-decimal fixed-point strings.
            expect(recorded.options[0].weight).to.equal('600000000000000000');
            expect(recorded.options[1].weight).to.equal('400000000000000000');
        });

        it('deposit tops up a deposit-period proposal into voting period', async () => {
            const id = await submitProposal(textProposal('e2e deposit'), ethers.parseEther('2'));

            // Poll: the gov query can race the EVM receipt on CI (cosmjs throws
            // on a not-yet-visible proposal, so an unguarded read hard-fails).
            const qc = await cosmosQuery();
            const before = await waitUntil(
                async () => (await qc.gov.proposal(id.toString())).proposal ?? null,
                { timeoutMs: 15_000, label: 'proposal visible in deposit period' },
            );
            expect(before.status, 'starts in deposit period').to.equal(DEPOSIT_PERIOD);

            const depositTx = await gov.deposit(id, {
                value: ethers.parseEther('8'),
                gasLimit: 500_000,
            });
            expect((await depositTx.wait())!.status).to.equal(1);

            const after = await waitUntil(
                async () => {
                    const { proposal } = await qc.gov.proposal(id.toString());
                    return proposal?.status === VOTING_PERIOD ? proposal : null;
                },
                { timeoutMs: 15_000, label: 'proposal activated by deposit' },
            );
            const total = after.totalDeposit.find(c => c.denom === 'usei');
            expect(total?.amount, 'total deposit reaches min_deposit').to.equal('10000000');
        });

        it('params() matches cosmosQuery().gov.params()', async () => {
            const qc = await cosmosQuery();
            const [pre, voting, deposit] = await Promise.all([
                gov.params(),
                qc.gov.params('voting'),
                qc.gov.params('deposit'),
            ]);
            expect(pre.votingPeriod).to.equal(durationSeconds(voting.votingParams?.votingPeriod));
            expect(pre.maxDepositPeriod).to.equal(
                durationSeconds(deposit.depositParams?.maxDepositPeriod),
            );
            const min = deposit.depositParams?.minDeposit?.[0];
            expect(min, 'cosmos min_deposit is set').to.not.equal(undefined);
            expect(pre.minDeposit[0].denom).to.equal(min!.denom);
            expect(pre.minDeposit[0].amount).to.equal(BigInt(min!.amount));
        });

        it('proposal/deposits/vote/tally queries reflect a freshly submitted and voted proposal', async () => {
            const id = await submitProposal(textProposal('e2e queries'), MIN_DEPOSIT_WEI);

            const prop = await waitUntil(
                async () => {
                    const p = await gov.proposal(id);
                    return Number(p.status) === VOTING_PERIOD ? p : null;
                },
                { timeoutMs: 15_000, label: 'proposal() in voting period' },
            );
            expect(prop.id).to.equal(id);

            const dep = await gov.getDeposit(id, admin.address);
            expect(dep.proposalId).to.equal(id);
            expect(dep.depositor).to.equal(adminSeiAddress);
            expect(dep.amount[0].denom).to.equal('usei');
            expect(dep.amount[0].amount).to.equal(MIN_DEPOSIT_USEI);

            const [allDeposits] = await gov.deposits(id, EMPTY_PAGE_KEY);
            expect(
                allDeposits.some((d: { depositor: string }) => d.depositor === adminSeiAddress),
            ).to.equal(true);

            const voteTx = await gov.vote(id, 1, { gasLimit: 500_000 });
            expect((await voteTx.wait())!.status).to.equal(1);

            const recorded = await waitUntil(
                async () => {
                    try {
                        const v = await gov.getVote(id, admin.address);
                        return v.options?.length ? v : null;
                    } catch {
                        return null;
                    }
                },
                { timeoutMs: 15_000, label: 'getVote after vote()' },
            );
            expect(recorded.proposalId).to.equal(id);
            expect(recorded.voter).to.equal(adminSeiAddress);
            expect(Number(recorded.options[0].option)).to.equal(1);

            const [allVotes] = await gov.votes(id, EMPTY_PAGE_KEY);
            expect(allVotes.some((v: { voter: string }) => v.voter === adminSeiAddress)).to.equal(
                true,
            );

            // Every tally field is a decimal power string. The admin is an EOA
            // with no delegation, so its Yes carries no weight — assert the
            // format rather than a total, which would drift with the devnet's
            // stake distribution.
            const tally = await gov.tallyResult(id);
            for (const field of ['yes', 'abstain', 'no', 'noWithVeto'] as const) {
                expect(tally[field], `tallyResult.${field}`).to.match(/^\d+$/);
            }

            let pageKey: Uint8Array = EMPTY_PAGE_KEY;
            let found = false;
            for (let i = 0; i < 20 && !found; i++) {
                const [page, nextKey] = (await gov.proposals(
                    0,
                    ethers.ZeroAddress,
                    ethers.ZeroAddress,
                    pageKey,
                )) as [Array<{ id: bigint }>, string];
                found = page.some(p => p.id === id);
                // nextKey arrives as a hex string, so an exhausted page is '0x',
                // not a zero-length value — decode before testing it or the loop
                // re-reads page one until the counter runs out.
                const next = ethers.getBytes(nextKey);
                if (next.length === 0) break;
                pageKey = next;
            }
            expect(found, 'proposals(0, zero, zero, empty) includes the new id').to.equal(true);
        });

        it('grantVoteAuthorization lets the grantee vote as the admin; revoke then blocks them', async () => {
            const [grantee] = claimPool(runtime, provider, 1, 'gov:vote-authz');
            await associateViaTx(grantee);

            const grantTx = await gov.grantVoteAuthorization(grantee.address, authExpiration(), {
                gasLimit: 500_000,
            });
            expect((await grantTx.wait())!.status).to.equal(1);

            const id = await submitProposal(textProposal('e2e authz vote'), MIN_DEPOSIT_WEI);
            const govAsGrantee = gov.connect(grantee.wallet) as ethers.Contract;
            const voteTx = await govAsGrantee.voteWithAuthorization(admin.address, id, 1, {
                gasLimit: 500_000,
            });
            expect((await voteTx.wait())!.status).to.equal(1);

            const recorded = await waitUntil(
                async () => {
                    try {
                        const v = await gov.getVote(id, admin.address);
                        return v.options?.length ? v : null;
                    } catch {
                        return null;
                    }
                },
                { timeoutMs: 15_000, label: 'authorized vote visible via getVote' },
            );
            expect(recorded.voter).to.equal(adminSeiAddress);
            expect(Number(recorded.options[0].option)).to.equal(1);

            const revokeTx = await gov.revokeVoteAuthorization(grantee.address, {
                gasLimit: 500_000,
            });
            expect((await revokeTx.wait())!.status).to.equal(1);

            const id2 = await submitProposal(textProposal('e2e authz vote revoked'), MIN_DEPOSIT_WEI);
            await expectVmError(
                govAsGrantee.voteWithAuthorization(admin.address, id2, 1, {
                    gasLimit: 500_000,
                }),
                'authorization not found',
            );
        });

        it('grantProposalAuthorization lets the grantee submit on behalf of the admin', async () => {
            const [grantee] = claimPool(runtime, provider, 1, 'gov:proposal-authz');
            await associateViaTx(grantee);
            await fundEvm(admin, grantee.address, MIN_DEPOSIT_WEI);

            const grantTx = await gov.grantProposalAuthorization(grantee.address, authExpiration(), {
                gasLimit: 500_000,
            });
            expect((await grantTx.wait())!.status).to.equal(1);

            const govAsGrantee = gov.connect(grantee.wallet) as ethers.Contract;
            const json = textProposal('e2e authz submit');
            const id: bigint = await govAsGrantee.submitProposalWithAuthorization.staticCall(
                admin.address,
                json,
                { value: MIN_DEPOSIT_WEI },
            );
            const tx = await govAsGrantee.submitProposalWithAuthorization(admin.address, json, {
                value: MIN_DEPOSIT_WEI,
                gasLimit: 1_000_000,
            });
            expect((await tx.wait())!.status).to.equal(1);

            const prop = await waitUntil(
                async () => {
                    try {
                        const p = await gov.proposal(id);
                        return p.id === id ? p : null;
                    } catch {
                        return null;
                    }
                },
                { timeoutMs: 15_000, label: 'authorized proposal visible' },
            );
            expect(prop.id).to.equal(id);

            const revokeTx = await gov.revokeProposalAuthorization(grantee.address, {
                gasLimit: 500_000,
            });
            expect((await revokeTx.wait())!.status).to.equal(1);
        });
    });

    describe('error handling', () => {
        it('vote on an unknown proposal fails with the gov module error', async () => {
            await expectVmError(
                gov.vote(999_999n, 1, { gasLimit: 500_000 }),
                'unknown proposal',
            );
        });

        it('vote with an invalid option is rejected', async () => {
            const id = await submitProposal(textProposal('e2e bad option'), MIN_DEPOSIT_WEI);
            await expectVmError(
                gov.vote(id, 0, { gasLimit: 500_000 }),
                'invalid vote option',
            );
        });

        it('vote from an unassociated caller reverts (via eth_call)', async () => {
            // Mining a tx auto-associates its sender, so the association error
            // can never surface from a real tx — only eth_call (which does not
            // associate) reaches the precompile with an unassociated caller.
            const [unassociated] = claimPool(runtime, provider, 1, 'gov:unassociated');
            await expectExecutionReverted(
                (gov.connect(unassociated.wallet) as ethers.Contract).vote.staticCall(1n, 1),
                'gov.vote from an unassociated caller',
            );
        });

        it('voteWeighted rejects more than 4 options', async () => {
            const options = [1, 2, 3, 4, 1].map(option => ({ option, weight: '0.2' }));
            await expectVmError(
                gov.voteWeighted(1n, options, { gasLimit: 500_000 }),
                'too many vote options provided',
            );
        });

        it('voteWeighted rejects an unparseable weight', async () => {
            await expectVmError(
                gov.voteWeighted(1n, [{ option: 1, weight: 'not-a-decimal' }], {
                    gasLimit: 500_000,
                }),
                'invalid weight format',
            );
        });

        it('submitProposal rejects malformed JSON', async () => {
            await expectExecutionReverted(
                gov.submitProposal.staticCall('{not json', { value: 0n }),
                'gov.submitProposal with malformed JSON',
            );
        });

        it('deposit rejects a zero value', async () => {
            const id = await submitProposal(textProposal('e2e zero deposit'), 0n);
            await expectExecutionReverted(
                gov.deposit.staticCall(id, { value: 0n }),
                'gov.deposit with value 0',
            );
        });

        it('getVote on an unknown proposal reverts', async () => {
            await expectExecutionReverted(
                gov.getVote(999_999n, admin.address),
                'gov.getVote on an unknown proposal',
            );
        });

        it('voteWithAuthorization without a grant reverts', async () => {
            const [grantee] = claimPool(runtime, provider, 1, 'gov:no-vote-grant');
            await associateViaTx(grantee);
            const id = await submitProposal(textProposal('e2e no grant'), MIN_DEPOSIT_WEI);
            await expectVmError(
                (gov.connect(grantee.wallet) as ethers.Contract).voteWithAuthorization(
                    admin.address,
                    id,
                    1,
                    { gasLimit: 500_000 },
                ),
                'authorization not found',
            );
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        // The executor dispatches its query methods before the readOnly check,
        // so gov views answer under STATICCALL and only the transaction methods
        // are refused.
        it('transaction methods are rejected under STATICCALL (readOnly guard)', async () => {
            const data = govIface.encodeFunctionData('vote', [1n, 1]);
            await expectVmError(
                caller.getFunction('staticcallTarget').send(PRECOMPILE_ADDRESSES.gov, data, {
                    gasLimit: 500_000,
                }),
                'cannot call gov precompile from staticcall',
            );
        });

        it('all methods are rejected under DELEGATECALL', async () => {
            const data = govIface.encodeFunctionData('vote', [1n, 1]);
            await expectVmError(
                caller.getFunction('delegatecallTarget').send(PRECOMPILE_ADDRESSES.gov, data, {
                    gasLimit: 500_000,
                }),
                'cannot delegatecall gov',
            );
        });

        it('proposal() is callable via STATICCALL', async () => {
            const id = await submitProposal(textProposal('e2e staticcall proposal'), MIN_DEPOSIT_WEI);
            const data = govIface.encodeFunctionData('proposal', [id]);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.gov,
                data,
            );
            const [decoded] = govIface.decodeFunctionResult('proposal', ret);
            expect(decoded.id).to.equal(id);
        });
    });
});
