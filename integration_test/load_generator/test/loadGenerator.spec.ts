import { expect } from 'chai';
import { ethers } from 'ethers';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { loadProvisionConfig } from '../src/config';
import { LoadAuditWriter } from '../src/loadAudit';
import { loadGeneratorConfig } from '../src/loadConfig';
import {
    applyOperationWeights,
    chooseOperation,
    nextScheduleAt,
    seededRandom,
} from '../src/workloads/scheduler';
import { defiOperations } from '../src/workloads/defi';
import { nativeTransferOperations } from '../src/workloads/nativetransfers';
import { tokenOperations } from '../src/workloads/tokenops';
import { LoadOperation, WorkloadContext } from '../src/workloads/types';
import {
    REPLAY_DEPLOYMENT_SCHEMA_VERSION,
    ReplayDeploymentManifest,
} from '../src/replay/replayTypes';
import { SUSHI_V2_PROVENANCE } from '../src/sushiV2';

describe('multi-mode load generator', () => {
    it('parses CLI arguments and enforces run identity for execution', () => {
        const config = loadGeneratorConfig(
            ['run', '--type', 'defi', '--tps=12.5', '--duration', '30', '--run-id', 'defi-a'],
            { EXECUTE: '1', WORKER_COUNT: '4', FIXTURE_PREPARE_GAS_LIMIT: '3000000' },
        );
        expect(config.type).to.equal('defi');
        expect(config.tps).to.equal(12.5);
        expect(config.maxTps).to.equal(100);
        expect(config.durationSeconds).to.equal(30);
        expect(config.runId).to.equal('defi-a');
        expect(config.workerCount).to.equal(4);
        expect(config.usersPerTps).to.equal(2);
        expect(config.maxWorkerCount).to.equal(200);
        expect(config.fixturePrepareGasLimit).to.equal(3_000_000n);
        expect(() => loadGeneratorConfig(['--type', 'tokenops'], { EXECUTE: '1' })).to.throw(
            'RUN_ID',
        );
        expect(() => loadGeneratorConfig(['--type', 'unknown'], {})).to.throw(
            'type must be one of',
        );
        expect(() => loadGeneratorConfig(['--type', 'defi', '--tps', '101'], {})).to.throw(
            'exceeds MAX_SYNTHETIC_TPS 100',
        );
        expect(
            loadGeneratorConfig(['--type', 'defi', '--tps', '1000'], {
                MAX_SYNTHETIC_TPS: '1000',
                MAX_WORKER_COUNT: '2000',
            }).tps,
        ).to.equal(1000);
        expect(loadGeneratorConfig(['--type', 'defi', '--tps', '20'], {}).workerCount).to.equal(40);
        expect(() =>
            loadGeneratorConfig(['--type', 'defi', '--tps', '101'], {
                MAX_SYNTHETIC_TPS: '101',
            }),
        ).to.throw('worker count 202 exceeds MAX_WORKER_COUNT 200');
    });

    it('parses large funding targets without number precision loss', () => {
        const config = loadProvisionConfig({ FUND_SEI: '1000000000000.123456' });
        expect(config.fundSei).to.equal('1000000000000.123456');
        expect(config.targetUsei).to.equal(1_000_000_000_000_123_456n);
        expect(() => loadProvisionConfig({ FUND_SEI: '1.0000001' })).to.throw(
            'at most 6 decimal places',
        );
    });

    it('rotates synthetic audit files at the configured bound', async () => {
        const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'load-audit-'));
        try {
            const file = path.join(directory, 'transactions.jsonl');
            const writer = new LoadAuditWriter(file, 200, 2);
            await writer.initialize();
            for (let sequence = 0; sequence < 6; sequence++) {
                await writer.record({
                    timestamp: new Date(0).toISOString(),
                    runId: 'rotation-test',
                    loadType: 'defi',
                    sequence,
                    worker: 1,
                    operation: 'swap_a_to_b',
                    lane: 'evm',
                    outcome: 'included',
                });
            }
            await writer.flush();
            expect(await fs.readdir(directory)).to.include.members([
                'transactions.jsonl',
                'transactions.jsonl.1',
                'transactions.jsonl.2',
            ]);
        } finally {
            await fs.rm(directory, { recursive: true, force: true });
        }
    });

    it('selects weighted operations deterministically and validates overrides', () => {
        const operations = [operation('one', 1), operation('two', 3)];
        expect(chooseOperation(operations, 0).name).to.equal('one');
        expect(chooseOperation(operations, 0.5).name).to.equal('two');
        const first = seededRandom('run-a');
        const second = seededRandom('run-a');
        expect(Array.from({ length: 5 }, first)).to.deep.equal(Array.from({ length: 5 }, second));
        expect(applyOperationWeights(operations, { one: 9 })).to.deep.equal([
            { ...operations[0], weight: 9 },
        ]);
        expect(() => applyOperationWeights(operations, { missing: 1 })).to.throw(
            'unknown operation',
        );
    });

    it('paces from the current time instead of bursting after a stall', () => {
        expect(nextScheduleAt(1_000, 10, 1_000)).to.equal(1_100);
        expect(nextScheduleAt(1_000, 10, 2_000)).to.equal(2_100);
    });

    it('exposes distinct defi, token, and native-transfer operation sets', () => {
        const context = workloadContext();
        expect(defiOperations(context).map(item => item.name)).to.include.members([
            'swap_a_to_b',
            'lend_borrow',
            'vault_deposit',
        ]);
        expect(tokenOperations(context).map(item => item.name)).to.include.members([
            'erc20_mint',
            'erc721_round_trip',
            'erc1155_batch_mint',
            'cw1155_send',
        ]);
        const nativeTransfers = nativeTransferOperations(context);
        expect(nativeTransfers.map(item => item.name)).to.have.members([
            'cosmos_bank_send',
            'evm_native_transfer',
            'bank_precompile_send',
        ]);
    });

    it('uses the run id to avoid ERC721 mint collisions across reruns', async () => {
        const first = workloadContext();
        const second = { ...first, runId: 'second-run' };
        const firstMint = tokenOperations(first).find(item => item.name === 'erc721_mint')!;
        const secondMint = tokenOperations(second).find(item => item.name === 'erc721_mint')!;
        const [firstLoad, secondLoad] = await Promise.all([
            firstMint.build(first.workers[0], 0),
            secondMint.build(second.workers[0], 0),
        ]);
        expect(firstLoad).to.have.property('lane', 'evm');
        expect(secondLoad).to.have.property('lane', 'evm');
        expect(
            (firstLoad as { transaction: ethers.TransactionRequest }).transaction.data,
        ).not.to.equal((secondLoad as { transaction: ethers.TransactionRequest }).transaction.data);
    });
});

function operation(name: string, weight: number): LoadOperation {
    return {
        name,
        weight,
        lane: 'evm',
        async build() {
            return { lane: 'evm', transaction: {} };
        },
    };
}

function workloadContext(): WorkloadContext {
    const address = (index: number) =>
        ethers.getAddress(`0x${index.toString(16).padStart(40, '0')}`);
    const contracts = {
        router: address(1),
        tokenA: address(2),
        tokenB: address(3),
        lendingPoolProxy: address(4),
        masterChef: address(5),
        liquidStakingProxy: address(6),
        strategyVaultProxy: address(7),
        nft: address(8),
        erc1155: address(9),
    };
    const deployment: ReplayDeploymentManifest = {
        schemaVersion: REPLAY_DEPLOYMENT_SCHEMA_VERSION,
        network: 'arctic-1',
        chainId: 713715,
        sushiV2: SUSHI_V2_PROVENANCE,
        contracts,
    };
    const walletA = new ethers.Wallet(ethers.id('load-a'));
    const walletB = new ethers.Wallet(ethers.id('load-b'));
    return {
        runId: 'test-run',
        deployment,
        provider: {} as ethers.JsonRpcProvider,
        workers: [
            {
                slot: 0,
                index: 1,
                seiAddress: 'sei1a',
                evmAddress: walletA.address,
                wallet: walletA,
                evmNonce: 0,
            },
            {
                slot: 1,
                index: 2,
                seiAddress: 'sei1b',
                evmAddress: walletB.address,
                wallet: walletB,
                evmNonce: 0,
            },
        ],
        cw1155Contract: 'sei1cw1155',
    };
}
