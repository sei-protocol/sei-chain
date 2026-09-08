import { ethers } from 'ethers';
import { LoadOperation, LoadWorker, WorkloadContext } from './types';

export function nextWorker(context: WorkloadContext, slot: number): LoadWorker {
    return context.workers[(slot + 1) % context.workers.length];
}

export function requiredContract(context: WorkloadContext, name: string): string {
    const address = context.deployment.contracts[name];
    if (!address) throw new Error(`Deployment is missing ${name}`);
    return address;
}

export function requiredContracts<T extends string>(
    context: WorkloadContext,
    names: readonly T[],
): Record<T, string> {
    return Object.fromEntries(names.map(name => [name, requiredContract(context, name)])) as Record<
        T,
        string
    >;
}

export function evmCall(to: string, data: string, gasLimit: bigint, value = 0n) {
    return {
        lane: 'evm' as const,
        transaction: { to, data, value, gasLimit },
    };
}

export function evmOperation(
    name: string,
    weight: number,
    to: string,
    contract: ethers.Interface,
    method: string,
    gasLimit: bigint,
    args: (worker: LoadWorker, sequence: number) => unknown[],
): LoadOperation {
    return {
        name,
        lane: 'evm',
        weight,
        async build(worker, sequence) {
            return evmCall(
                to,
                contract.encodeFunctionData(method, args(worker, sequence)),
                gasLimit,
            );
        },
    };
}
