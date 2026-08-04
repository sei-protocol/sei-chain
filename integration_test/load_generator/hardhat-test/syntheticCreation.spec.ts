import { expect } from 'chai';
import { ethers, network } from 'hardhat';
import type { Log, LogDescription } from 'ethers';

type OpcodeTrace = { structLogs?: Array<{ op?: string }> };

describe('synthetic contract creation', () => {
    it('deploys bounded runtime code through CREATE with constructor pressure', async () => {
        const harness = await ethers.deployContract('SyntheticCreationHarness');
        const transaction = await harness.deploy(
            320,
            3,
            10_000,
            600,
            false,
            ethers.id('create'),
        );
        const receipt = await transaction.wait();
        const event = receipt?.logs
            .map((log: Log) => {
                try {
                    return harness.interface.parseLog(log);
                } catch {
                    return null;
                }
            })
            .find((log: LogDescription | null) => log?.name === 'SyntheticContractCreated');
        const created = event?.args.created as string;

        expect(ethers.getBytes(await ethers.provider.getCode(created))).to.have.length(320);
        const trace = (await network.provider.send('debug_traceTransaction', [
            transaction.hash,
            { disableMemory: true, disableStack: true, disableStorage: true },
        ])) as OpcodeTrace;
        const operations = trace.structLogs?.map(step => step.op) ?? [];
        expect(operations).to.include('CREATE');
        expect(operations.filter(operation => operation === 'SSTORE').length).to.be.at.least(3);
    });

    it('supports CREATE2 and rejects values above its runtime bound', async () => {
        const harness = await ethers.deployContract('SyntheticCreationHarness');
        const transaction = await harness.deploy(
            64,
            0,
            0,
            128,
            true,
            ethers.id('create2'),
        );
        const trace = (await network.provider.send('debug_traceTransaction', [
            transaction.hash,
            { disableMemory: true, disableStack: true, disableStorage: true },
        ])) as OpcodeTrace;
        expect(trace.structLogs?.map(step => step.op)).to.include('CREATE2');

        await expect(
            harness.deploy(24_577, 0, 0, 128, false, ethers.id('too-large')),
        ).to.be.revertedWithCustomError(harness, 'InvalidCreationShape');
    });
});
