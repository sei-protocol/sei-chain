import { EncodeObject } from '@cosmjs/proto-signing';
import { ethers } from 'ethers';
import { LoadLane } from '../loadMetrics';
import { ReplayDeploymentManifest } from '../replay/replayTypes';

export interface LoadWorker {
    slot: number;
    index: number;
    seiAddress: string;
    evmAddress: string;
    wallet: ethers.Wallet;
    evmNonce: number;
}

export interface WorkloadContext {
    runId: string;
    deployment: ReplayDeploymentManifest;
    provider: ethers.JsonRpcProvider;
    workers: LoadWorker[];
    cw1155Contract?: string;
}

export interface EvmLoad {
    lane: 'evm';
    transaction: ethers.TransactionRequest;
}

export interface CosmosLoad {
    lane: 'cosmos';
    messages: readonly EncodeObject[];
    memo?: string;
    gas?: string;
    feeUsei?: string;
}

export type BuiltLoad = EvmLoad | CosmosLoad;

export interface LoadOperation {
    name: string;
    lane: LoadLane;
    weight: number;
    build(worker: LoadWorker, sequence: number): Promise<BuiltLoad>;
}
