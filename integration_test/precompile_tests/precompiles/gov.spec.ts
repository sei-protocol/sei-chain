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
import { EvmAccount } from '../utils/evmUtils';
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
const VOTING_PERIOD = 2; // PROPOSAL_STATUS_VOTING_PERIOD
const DEPOSIT_PERIOD = 1; // PROPOSAL_STATUS_DEPOSIT_PERIOD

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
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('all methods are rejected under STATICCALL (gov has no view methods)', async () => {
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
    });
});
