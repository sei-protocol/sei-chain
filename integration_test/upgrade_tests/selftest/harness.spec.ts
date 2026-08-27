/**
 * Checks on the suite itself, runnable with no chain.
 *
 * The record and verify phases only run against a live network, days apart, and
 * a mistake in the comparison logic would surface as a false pass at the worst
 * possible moment. These cover the parts that can be exercised offline: the
 * ABIs the probes encode against, the artifact round trip, the gate, and the
 * normalisation both phases share.
 */
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { expect } from 'chai';
import { precompileInterface, ADDRESSES } from '../utils/chain';
import {
    assertSameChain,
    probeValue,
    readArtifact,
    upgradeGateRefusal,
    writeArtifact,
    type Artifact,
} from '../utils/artifact';
import {
    archiveReadLabels,
    diffProbes,
    isHistoryUnavailable,
    knowsArchiveRead,
    probes,
    reReadAtHeight,
} from '../utils/probes';
import { expectedRemovedModules, resolveTarget, upgradeName } from '../config/targets';

const sampleArtifact = (): Artifact => ({
    schema: 1,
    meta: {
        network: 'arctic-1',
        evmChainId: '713715',
        planName: 'v6.7',
        blockNumber: 1234,
        cosmosHeight: 1234,
        seidVersion: 'v6.6.1',
        recordedAt: '2026-01-01T00:00:00.000Z',
    },
    probes: [
        {
            name: 'cosmos.moduleVersion.feegrant',
            kind: 'transition',
            describes: 'x',
            value: 'v1',
        },
        {
            name: 'cosmos.moduleVersion.bank',
            kind: 'invariant',
            describes: 'x',
            value: 'v2',
        },
    ],
    archiveReads: [],
    transactions: [],
    moduleVersions: [
        { name: 'bank', version: '2' },
        { name: 'feegrant', version: '1' },
        { name: 'oracle', version: '6' },
    ],
});

describe('suite self-checks', () => {
    describe('precompile ABIs the probes encode against', () => {
        it('the oracle interface still has the methods the probes call', () => {
            const iface = precompileInterface('oracle');
            expect(iface.encodeFunctionData('getExchangeRates', [])).to.match(/^0x[0-9a-f]{8}$/);
            expect(iface.encodeFunctionData('getOracleTwaps', [3600n])).to.match(/^0x[0-9a-f]+$/);
        });

        it('the upgrade interface still has appliedPlan and moduleVersions', () => {
            const iface = precompileInterface('upgrade');
            expect(iface.encodeFunctionData('appliedPlan', ['v6.7'])).to.match(/^0x[0-9a-f]+$/);
            expect(iface.encodeFunctionData('moduleVersions', [''])).to.match(/^0x[0-9a-f]+$/);
        });

        it('pins the addresses the suite probes', () => {
            expect(ADDRESSES.oracle).to.equal('0x0000000000000000000000000000000000001008');
            expect(ADDRESSES.feegrant).to.equal('0x0000000000000000000000000000000000001010');
            expect(ADDRESSES.upgrade).to.equal('0x0000000000000000000000000000000000001015');
        });
    });

    describe('the probe table', () => {
        const table = probes('v6.7');

        it('names every probe uniquely, so artifact lookups cannot collide', () => {
            const names = table.map(p => p.name);
            expect(new Set(names).size, `duplicate probe name in ${names.join(', ')}`).to.equal(
                names.length,
            );
        });

        it('explains every probe, since the artifact outlives whoever ran it', () => {
            for (const probe of table) {
                expect(probe.describes, `${probe.name} has no description`).to.not.equal('');
            }
        });

        it('covers both what must hold and what must change', () => {
            expect(table.some(p => p.kind === 'invariant')).to.equal(true);
            expect(table.some(p => p.kind === 'transition')).to.equal(true);
        });

        it('probes every module the upgrade removes, plus the applied plan', () => {
            const names = table.map(p => p.name);
            for (const module of expectedRemovedModules()) {
                expect(names, `no probe for the removal of ${module}`).to.include(
                    `cosmos.moduleVersion.${module}`,
                );
            }
            expect(names).to.include('cosmos.appliedPlan.v6.7');
        });

        it('expects the four modules v6.7 retires', () => {
            expect(expectedRemovedModules()).to.deep.equal([
                'capability',
                'feegrant',
                'ibc',
                'transfer',
            ]);
        });
    });

    describe('the probe diff the verify phase reports', () => {
        it('lists only the probes whose answer moved', () => {
            const before = sampleArtifact().probes;
            const after = before.map(p =>
                p.name === 'cosmos.moduleVersion.feegrant' ? { ...p, value: 'absent' } : p,
            );
            expect(diffProbes(before, after)).to.deep.equal([
                {
                    name: 'cosmos.moduleVersion.feegrant',
                    kind: 'transition',
                    from: 'v1',
                    to: 'absent',
                },
            ]);
        });

        it('is empty when nothing moved', () => {
            const probeList = sampleArtifact().probes;
            expect(diffProbes(probeList, probeList)).to.deep.equal([]);
        });

        it('ignores a probe the artifact never recorded, which the invariant check reports instead', () => {
            const after = [
                ...sampleArtifact().probes,
                { name: 'brand.new', kind: 'invariant' as const, describes: 'x', value: 'ok' },
            ];
            expect(diffProbes(sampleArtifact().probes, after)).to.deep.equal([]);
        });
    });

    describe('the artifact', () => {
        let dir: string;
        let env: NodeJS.ProcessEnv;

        beforeEach(() => {
            dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sei-upgrade-tests-'));
            env = { UPGRADE_TEST_ARTIFACT: path.join(dir, 'pre-upgrade.json') };
        });

        afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

        it('round trips without losing anything', () => {
            const original = sampleArtifact();
            writeArtifact(original, env);
            expect(readArtifact(env)).to.deep.equal(original);
        });

        it('creates the directory it writes into', () => {
            const nested = { UPGRADE_TEST_ARTIFACT: path.join(dir, 'a', 'b', 'pre.json') };
            writeArtifact(sampleArtifact(), nested);
            expect(fs.existsSync(nested.UPGRADE_TEST_ARTIFACT!)).to.equal(true);
        });

        it('says how to produce one when it is missing', () => {
            expect(() => readArtifact(env)).to.throw(/Run the record phase before the upgrade/);
        });

        it('refuses an artifact from a different chain', () => {
            const artifact = sampleArtifact();
            expect(() => assertSameChain(artifact, 'atlantic-2', 1328n)).to.throw(
                /recorded against EVM chain 713715/,
            );
            expect(() => assertSameChain(artifact, 'arctic-1', 713715n)).to.not.throw();
        });

        it('refuses a schema it does not understand', () => {
            const artifact = { ...sampleArtifact(), schema: 99 };
            fs.mkdirSync(dir, { recursive: true });
            fs.writeFileSync(env.UPGRADE_TEST_ARTIFACT!, JSON.stringify(artifact));
            expect(() => readArtifact(env)).to.throw(/unsupported artifact schema 99/);
        });

        it('looks probes up by name', () => {
            const artifact = sampleArtifact();
            expect(probeValue(artifact, 'cosmos.moduleVersion.feegrant')).to.equal('v1');
            expect(probeValue(artifact, 'nope')).to.equal(undefined);
        });
    });

    /**
     * The gate decides whether anything the verify phase reports means
     * anything, and its failure mode is a false pass. It is pure so that it can
     * be pinned here rather than only exercised against a live chain.
     */
    describe('the gate on the verify phase', () => {
        const gate = (appliedHeight: number, artifactCosmosHeight = 1000) =>
            upgradeGateRefusal({
                planName: 'v6.7',
                network: 'arctic-1',
                appliedHeight,
                artifactCosmosHeight,
                context: 'height 1200, seid v6.6.1',
            });

        it('refuses when the upgrade has not been applied', () => {
            expect(gate(0)).to.match(/has not applied v6\.7 yet/);
        });

        it('refuses when the artifact was recorded after the upgrade', () => {
            expect(gate(900, 1000)).to.match(/at or before the artifact was recorded/);
        });

        it('refuses when the upgrade landed exactly at the recorded height', () => {
            expect(gate(1000, 1000)).to.match(/at or before the artifact was recorded/);
        });

        it('allows a run where the upgrade landed after the artifact', () => {
            expect(gate(1100, 1000)).to.equal(undefined);
        });

        it('names the network and the plan, so the message is actionable', () => {
            expect(gate(0)).to.contain('arctic-1').and.to.contain('v6.7');
        });
    });

    /**
     * The record phase and the verify phase have to reduce the same answer to
     * the same token. When they did not, a re-read of an unchanged block
     * reported `revert:oracle-retired` becoming
     * `revert:raw(execution reverted: oracle precompile is retired…)` — the same
     * answer, called a regression. Both phases now go through one lookup table,
     * and this pins that they stay in step.
     */
    describe('archive reads normalise identically in both phases', () => {
        it('re-reads every label the record phase can produce', () => {
            for (const label of archiveReadLabels()) {
                expect(knowsArchiveRead(label), `${label} cannot be re-read`).to.equal(true);
            }
        });

        it('takes at least one archive read, or the history check proves nothing', () => {
            expect(archiveReadLabels().length).to.be.greaterThan(0);
        });

        it('reports rather than compares a label it no longer defines', async () => {
            const outcome = await reReadAtHeight({
                label: 'retired.probe.from.an.older.artifact',
                blockNumber: 1,
                value: 'ok:0x',
            });
            expect(outcome).to.deep.equal({ unknownLabel: true });
        });
    });

    describe('pruned history is not a changed answer', () => {
        it('recognises the ways a node says it no longer has a height', () => {
            for (const value of [
                'revert:raw(height 100 is not available, lowest height is 900)',
                'revert:raw(failed to load state at height 5)',
                'revert:raw(-32000: header not found)',
            ]) {
                expect(isHistoryUnavailable(value), value).to.equal(true);
            }
        });

        it('does not mistake a real answer for a pruned one', () => {
            expect(isHistoryUnavailable('ok:0x')).to.equal(false);
            expect(isHistoryUnavailable('revert:oracle-retired')).to.equal(false);
            expect(isHistoryUnavailable('absent')).to.equal(false);
        });
    });

    describe('target resolution', () => {
        it('defaults to arctic-1', () => {
            const target = resolveTarget({});
            expect(target.name).to.equal('arctic-1');
            expect(target.evmChainId).to.equal(713715n);
        });

        it('honours an endpoint override without losing the expected chain id', () => {
            const target = resolveTarget({ UPGRADE_TEST_EVM_RPC: 'http://archive.example:8545' });
            expect(target.evmRpcUrl).to.equal('http://archive.example:8545');
            expect(target.evmChainId).to.equal(713715n);
        });

        it('rejects a network it has no chain id for', () => {
            expect(() => resolveTarget({ UPGRADE_TEST_NETWORK: 'mainnet' })).to.throw(
                /is not one of/,
            );
        });

        it('defaults the plan name to the upgrade under test', () => {
            expect(upgradeName({})).to.equal('v6.7');
            expect(upgradeName({ UPGRADE_TEST_PLAN_NAME: 'v6.8' })).to.equal('v6.8');
        });

        it('lets the removed-module set be overridden, and sorts it', () => {
            expect(expectedRemovedModules({ UPGRADE_TEST_REMOVED_MODULES: 'zeta,alpha' })).to.deep.equal([
                'alpha',
                'zeta',
            ]);
        });
    });
});
