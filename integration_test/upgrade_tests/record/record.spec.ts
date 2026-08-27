/**
 * Phase one: run before the upgrade.
 *
 * Probes every module surface the upgrade retires, sends transactions at the
 * retired ones, and writes it all to an artifact. Nothing here asserts the
 * upgrade's effects — that is the verify phase's job. What it does assert is
 * that the chain is in a state worth recording: if the upgrade has already run,
 * an artifact taken now would make the comparison vacuous.
 */
import { expect } from 'chai';
import {
    adminMnemonic,
    expectedRemovedModules,
    resolveTarget,
    upgradeName,
} from '../config/targets';
import { assertExpectedChain } from '../utils/chain';
import { writeArtifact, type Artifact, type RecordedTx } from '../utils/artifact';
import {
    appliedPlanHeight,
    applicationVersion,
    currentPlan,
    latestCosmosHeight,
    moduleVersions,
} from '../utils/cosmos';
import { currentHeight, runProbes, takeArchiveReads } from '../utils/probes';
import { fundedAddress, sendProbeTransactions } from '../utils/txProbes';

describe('record pre-upgrade behaviour', function () {
    this.timeout(300 * 1000);

    const target = resolveTarget();
    const planName = upgradeName();
    const mnemonic = adminMnemonic();

    before(async () => {
        await assertExpectedChain();
    });

    it(`is on a chain that has not applied ${planName} yet`, async () => {
        const applied = await appliedPlanHeight(planName);
        expect(
            applied,
            `${target.name} already applied ${planName} at height ${applied}. Recording now ` +
                'would capture post-upgrade behaviour and leave the verify phase comparing it ' +
                'against itself. Set UPGRADE_TEST_PLAN_NAME to the next upgrade instead.',
        ).to.equal(0);
    });

    it('writes an artifact of every retired surface', async () => {
        const blockNumber = await currentHeight();
        const cosmosHeight = await latestCosmosHeight();
        const seidVersion = await applicationVersion();
        const probes = await runProbes(planName);
        const archiveReads = await takeArchiveReads(blockNumber);
        const modules = await moduleVersions();

        let transactions: RecordedTx[] = [];
        if (mnemonic) {
            const funded = await fundedAddress(mnemonic);
            expect(
                funded.balance > 0n,
                `${funded.address} has no balance, so the transaction probes cannot be sent`,
            ).to.equal(true);
            transactions = await sendProbeTransactions(mnemonic);
        }

        const artifact: Artifact = {
            schema: 1,
            meta: {
                network: target.name,
                evmChainId: target.evmChainId.toString(),
                planName,
                blockNumber,
                cosmosHeight,
                seidVersion,
                recordedAt: new Date().toISOString(),
            },
            probes,
            archiveReads,
            transactions,
            moduleVersions: modules,
        };

        const written = writeArtifact(artifact);

        // Each module the upgrade is meant to remove must still be present, or
        // there is nothing for the upgrade to remove and the verify phase would
        // pass without proving anything.
        const names = modules.map(m => m.name);
        for (const module of expectedRemovedModules()) {
            expect(names, `${module} must still hold a module version before the upgrade`).to.include(
                module,
            );
        }

        const scheduled = await currentPlan();
        console.log(`    network      ${target.name} on seid ${seidVersion}, EVM height ${blockNumber}`);
        console.log(`    modules      ${modules.length} with a version entry`);
        console.log(
            `    scheduled    ${scheduled ? `${scheduled.name} at height ${scheduled.height}` : 'no upgrade scheduled yet'}`,
        );
        console.log(`    transactions ${transactions.length}${mnemonic ? '' : ' (no UPGRADE_TEST_MNEMONIC; read-only probes only)'}`);
        console.log(`    artifact     ${written}`);
        console.log('    keep the artifact; the verify phase needs it once the upgrade lands.');
        for (const probe of probes) {
            console.log(`      ${probe.kind === 'invariant' ? '=' : '~'} ${probe.name} = ${probe.value}`);
        }
    });
});
