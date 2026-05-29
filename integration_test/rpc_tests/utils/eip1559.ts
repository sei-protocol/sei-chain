import util from 'node:util';

const exec = util.promisify(require('node:child_process').exec);

const DOCKER_NODE = 'sei-node-0';
const SEID_ENV = 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin';

/** EIP-1559 fee-market parameters as the chain applies them. */
export interface Eip1559Params {
    blockGasLimit: number;
    targetGasUsedPerBlock: number;
    maxUpwardAdjustment: number;
    maxDownwardAdjustment: number;
    minFeePerGas: number;
    maxFeePerGas: number;
}

/**
 * Read the live EIP-1559 params from the in-container `seid`. Returns null when no
 * local docker devnet is reachable so callers can degrade to structural-only checks
 * instead of failing on a hosted/remote Sei endpoint.
 */
export async function queryEip1559Params(): Promise<Eip1559Params | null> {
    try {
        const param = async (key: string): Promise<string> => {
            const { stdout } = await exec(
                `docker exec ${DOCKER_NODE} /bin/bash -c '${SEID_ENV} && seid query params subspace evm ${key} --output json'`,
            );
            return JSON.parse(stdout).value.replace(/"/g, '');
        };
        const { stdout: blockParams } = await exec(
            `docker exec ${DOCKER_NODE} /bin/bash -c '${SEID_ENV} && seid query params blockparams --output json'`,
        );
        const [minFee, maxFee, upward, downward, target] = await Promise.all([
            param('KeyMinFeePerGas'),
            param('KeyMaximumFeePerGas'),
            param('KeyMaxDynamicBaseFeeUpwardAdjustment'),
            param('KeyMaxDynamicBaseFeeDownwardAdjustment'),
            param('KeyTargetGasUsedPerBlock'),
        ]);
        return {
            blockGasLimit: Number(JSON.parse(blockParams).max_gas),
            targetGasUsedPerBlock: Number(target),
            maxUpwardAdjustment: parseFloat(upward),
            maxDownwardAdjustment: parseFloat(downward),
            minFeePerGas: parseFloat(minFee),
            maxFeePerGas: parseFloat(maxFee),
        };
    } catch {
        return null;
    }
}

/**
 * Sei's dynamic base fee for the next block. Sei does not use geth's 1/8 rule: it
 * nudges by up to `maxUpwardAdjustment` when a block is over `targetGasUsedPerBlock`
 * (scaled by how full the block is relative to the gas limit) and down by
 * `maxDownwardAdjustment` when under target (scaled by how empty it is), then clamps
 * to [minFeePerGas, maxFeePerGas]. Mirrors x/evm's CalculateNextBaseFee.
 */
export function nextBaseFeeSei(
    prevBaseFee: number,
    blockGasUsed: number,
    p: Eip1559Params,
): number {
    let next: number;
    if (blockGasUsed > p.targetGasUsedPerBlock) {
        const fullness = (blockGasUsed - p.targetGasUsedPerBlock) / (p.blockGasLimit - p.targetGasUsedPerBlock);
        next = prevBaseFee * (1 + p.maxUpwardAdjustment * fullness);
    } else {
        const emptiness = (p.targetGasUsedPerBlock - blockGasUsed) / p.targetGasUsedPerBlock;
        next = prevBaseFee * (1 - p.maxDownwardAdjustment * emptiness);
    }
    next = Math.floor(next);
    if (next < p.minFeePerGas) return p.minFeePerGas;
    if (next > p.maxFeePerGas) return p.maxFeePerGas;
    return next;
}

const GETH_ELASTICITY = 2n;
const GETH_BASE_FEE_CHANGE_DENOMINATOR = 8n;

/**
 * go-ethereum's London CalcBaseFee (all integer arithmetic): target = gasLimit/2,
 * base fee moves by at most 1/8 toward fullness each block, with a minimum delta of
 * 1 wei when over target. Exact, so feeHistory's predicted next base fee can be
 * matched byte-for-byte.
 */
export function nextBaseFeeGeth(prevBaseFee: bigint, gasUsed: bigint, gasLimit: bigint): bigint {
    const target = gasLimit / GETH_ELASTICITY;
    if (gasUsed === target) return prevBaseFee;
    if (gasUsed > target) {
        const delta = (prevBaseFee * (gasUsed - target)) / target / GETH_BASE_FEE_CHANGE_DENOMINATOR;
        return prevBaseFee + (delta > 0n ? delta : 1n);
    }
    const delta = (prevBaseFee * (target - gasUsed)) / target / GETH_BASE_FEE_CHANGE_DENOMINATOR;
    const next = prevBaseFee - delta;
    return next > 0n ? next : 0n;
}
