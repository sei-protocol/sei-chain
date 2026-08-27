/**
 * Phase two: run after the upgrade has landed on the network.
 *
 * Reads the pre-upgrade artifact and asks three questions of the live chain:
 * did the surfaces the upgrade does not touch keep answering identically, did
 * the module removal actually happen, and can the chain still serve the history
 * it committed before its state changed.
 */
import { expect } from 'chai';
import { expectedRemovedModules, resolveTarget, upgradeName } from '../config/targets';
import { assertExpectedChain, rawSei } from '../utils/chain';
import {
    assertSameChain,
    probeValue,
    readArtifact,
    upgradeGateRefusal,
    type Artifact,
} from '../utils/artifact';
import {
    appliedPlanHeight,
    applicationVersion,
    latestCosmosHeight,
    moduleVersions,
} from '../utils/cosmos';
import {
    diffProbes,
    isHistoryUnavailable,
    reReadAtHeight,
    runProbes,
    type ProbeResult,
} from '../utils/probes';
import { vmError } from '../utils/txProbes';

const EXPECTED_REMOVED_MODULES = expectedRemovedModules();

describe('verify post-upgrade behaviour', function () {
    this.timeout(300 * 1000);

    const target = resolveTarget();
    const planName = upgradeName();

    let artifact: Artifact;
    let current: ProbeResult[];
    let appliedHeight: number;

    /**
     * The gate. Every assertion below is about the difference the upgrade made,
     * so a run against a chain that has not upgraded yet would report a clean
     * pass on the invariants and a confusing failure on the transitions.
     * Throwing from a hook stops the suite with one message saying what to do.
     */
    before(async () => {
        await assertExpectedChain();
        artifact = readArtifact();
        assertSameChain(artifact, target.name, target.evmChainId);

        appliedHeight = await appliedPlanHeight(planName);
        const refusal = upgradeGateRefusal({
            planName,
            network: target.name,
            appliedHeight,
            artifactCosmosHeight: artifact.meta.cosmosHeight,
            context: `height ${await latestCosmosHeight()}, seid ${await applicationVersion()}`,
        });
        if (refusal) throw new Error(refusal);

        current = await runProbes(planName);

        const changed = diffProbes(artifact.probes, current);
        console.log(
            `    ${planName} applied at height ${appliedHeight}; artifact recorded at EVM height ` +
                `${artifact.meta.blockNumber} on seid ${artifact.meta.seidVersion}, now ${await applicationVersion()}`,
        );
        console.log(`    ${changed.length} probe(s) changed:`);
        for (const entry of changed) {
            console.log(`      ~ ${entry.name}: ${entry.from} -> ${entry.to}`);
        }
    });

    describe('surfaces the upgrade does not touch', () => {
        it('every invariant probe answers exactly as it did before', () => {
            const mismatches: string[] = [];
            for (const probe of current.filter(p => p.kind === 'invariant')) {
                const before = probeValue(artifact, probe.name);
                if (before === undefined) {
                    mismatches.push(`${probe.name}: absent from the artifact`);
                } else if (before !== probe.value) {
                    mismatches.push(`${probe.name}: was ${before}, now ${probe.value}`);
                }
            }
            expect(
                mismatches,
                'a surface the upgrade does not claim to touch answered differently',
            ).to.deep.equal([]);
        });
    });

    describe('the module removal the upgrade is for', () => {
        for (const removed of EXPECTED_REMOVED_MODULES) {
            it(`${removed} no longer holds a module version`, () => {
                const probe = `cosmos.moduleVersion.${removed}`;
                expect(
                    probeValue(artifact, probe),
                    `${removed} must have held a version before the upgrade`,
                ).to.match(/^v\d+$/);
                expect(
                    current.find(p => p.name === probe)?.value,
                    `the upgrade handler calls DeleteModuleVersion for ${removed}, so the ` +
                        'lookup must now answer not-found',
                ).to.equal('absent');
            });
        }

        it('removes exactly the modules it was supposed to and no others', async () => {
            const after = (await moduleVersions()).map(m => m.name);
            const before = artifact.moduleVersions.map(m => m.name);
            const removed = before.filter(n => !after.includes(n)).sort();
            const added = after.filter(n => !before.includes(n)).sort();

            expect(removed, 'unexpected module(s) lost their version entry').to.deep.equal(
                EXPECTED_REMOVED_MODULES,
            );
            expect(added, 'the upgrade added a module version this test did not expect').to.deep.equal(
                [],
            );
        });

        it('the feegrant precompile address is not a registered precompile', () => {
            expect(
                current.find(p => p.name === 'feegrant.precompileRegistered')?.value,
                'a revert would mean a precompile is still mounted at 0x…1010',
            ).to.equal('unregistered');
        });

        /**
         * Module versions are only ever written, never cleared, so a module
         * dropped from the manager without a DeleteModuleVersion call keeps its
         * entry for the life of the chain. arctic-1 carries dex and accesscontrol
         * entries whose stores were deleted at v5.8.0 and v6.3.0. This reports
         * them rather than failing: they are pre-existing, and the assertion that
         * this upgrade does not add to the pile is the one above.
         */
        it('reports module versions with no module behind them', async () => {
            const known = ['dex', 'accesscontrol', 'crisis', ...EXPECTED_REMOVED_MODULES];
            const orphans = (await moduleVersions()).map(m => m.name).filter(n => known.includes(n));
            if (orphans.length > 0) {
                console.log(`    orphaned module version entries still on chain: ${orphans.join(', ')}`);
            }
            for (const removed of EXPECTED_REMOVED_MODULES) {
                expect(orphans, `${planName} must not leave ${removed} on the orphan pile`).to.not.include(
                    removed,
                );
            }
        });
    });

    describe('history committed before the upgrade', () => {
        it('still answers the same at the heights it was read at', async () => {
            const mismatches: string[] = [];
            const skipped: string[] = [];

            for (const before of artifact.archiveReads) {
                const outcome = await reReadAtHeight(before);
                if ('unknownLabel' in outcome) {
                    skipped.push(`${before.label}: this suite version no longer defines that read`);
                    continue;
                }
                if (isHistoryUnavailable(outcome.value)) {
                    skipped.push(`${before.label}@${before.blockNumber}: ${outcome.value}`);
                    continue;
                }
                if (outcome.value !== before.value) {
                    mismatches.push(
                        `${before.label}@${before.blockNumber}: was ${before.value}, now ${outcome.value}`,
                    );
                }
            }

            if (skipped.length > 0) {
                // Pruning is a property of the endpoint, not of the upgrade. Say
                // so rather than passing quietly or failing on it.
                console.log(`    ${skipped.length} historical read(s) not compared:`);
                for (const entry of skipped) console.log(`      ${entry}`);
            }
            expect(
                mismatches,
                'a historical read changed answer; removing state must not rewrite what ' +
                    'already-committed blocks return',
            ).to.deep.equal([]);
        });

        it('still serves the receipts of transactions mined before it', async () => {
            if (artifact.transactions.length === 0) {
                console.log('    artifact has no transactions (recorded without a funded key)');
                return;
            }

            const mismatches: string[] = [];
            for (const before of artifact.transactions) {
                const envelope = await rawSei<{ status: string; blockNumber: string }>(
                    'eth_getTransactionReceipt',
                    [before.hash],
                );
                if (envelope.error || !envelope.result) {
                    mismatches.push(
                        `${before.label} (${before.hash}): receipt no longer resolves: ` +
                            `${envelope.error?.message ?? 'null result'}`,
                    );
                    continue;
                }
                const status = Number(BigInt(envelope.result.status));
                const blockNumber = Number(BigInt(envelope.result.blockNumber));
                if (status !== before.status) {
                    mismatches.push(`${before.label}: status was ${before.status}, now ${status}`);
                }
                if (blockNumber !== before.blockNumber) {
                    mismatches.push(
                        `${before.label}: block was ${before.blockNumber}, now ${blockNumber}`,
                    );
                }
            }
            expect(mismatches, 'pre-upgrade receipts must survive the upgrade unchanged').to.deep.equal(
                [],
            );
        });

        it('still reports the same failure reason for transactions that failed before it', async () => {
            const withReason = artifact.transactions.filter(t => t.vmError);
            if (withReason.length === 0) {
                console.log('    artifact recorded no failed transaction with a VM error');
                return;
            }

            const mismatches: string[] = [];
            for (const before of withReason) {
                const now = await vmError(before.hash);
                if (now === undefined) {
                    mismatches.push(`${before.label}: no VM error served any more`);
                } else if (now !== before.vmError) {
                    mismatches.push(
                        `${before.label}: was ${JSON.stringify(before.vmError)}, now ${JSON.stringify(now)}`,
                    );
                }
            }
            expect(
                mismatches,
                'the reason a pre-upgrade transaction failed must not change after the upgrade',
            ).to.deep.equal([]);
        });
    });
});
