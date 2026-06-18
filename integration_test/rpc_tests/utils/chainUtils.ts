import util from 'node:util';
import { ethers } from 'ethers';
import { Endpoints } from '../config/endpoints';
import {DOCKER_NODE, SEID_ENV} from "./constants";

const exec = util.promisify(require('node:child_process').exec);
const POLLING_INTERVAL_MS = Number(process.env.RPC_POLLING_INTERVAL_MS ?? 100);

const makeProvider = (url: string): ethers.JsonRpcProvider =>
    new ethers.JsonRpcProvider(url, undefined, {
        batchMaxCount: 1,
        staticNetwork: true,
        pollingInterval: POLLING_INTERVAL_MS,
    });

let seiProvider: ethers.JsonRpcProvider | undefined;
let gethProvider: ethers.JsonRpcProvider | undefined;

export function seiRpc(): ethers.JsonRpcProvider {
    if (!seiProvider) seiProvider = makeProvider(Endpoints.sei.evmRpc);
    return seiProvider;
}

/** Primary Ethereum reference: local geth --dev. */
export function gethRpc(): ethers.JsonRpcProvider {
    if (!gethProvider) gethProvider = makeProvider(Endpoints.eth.geth);
    return gethProvider;
}

/**
 * Sei + the primary geth reference. `eth` aliases the geth provider so existing
 * specs keep working after the fork→geth reference switch.
 */
export function bothProviders(): {
    sei: ethers.JsonRpcProvider;
    geth: ethers.JsonRpcProvider;
    eth: ethers.JsonRpcProvider;
} {
    const sei = seiRpc();
    const geth = gethRpc();
    return { sei, geth, eth: geth };
}

export async function isReachable(url: string, timeoutMs = 2_500): Promise<boolean> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
        const res = await fetch(url, {
            method: 'POST',
            headers: { 'content-type': 'application/json' },
            body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'eth_chainId', params: [] }),
            signal: controller.signal,
        });
        if (!res.ok) return false;
        const body = (await res.json()) as { result?: string };
        return typeof body.result === 'string';
    } catch {
        return false;
    } finally {
        clearTimeout(timer);
    }
}


export interface JsonRpcError {
    code: number;
    message: string;
    data?: unknown;
}

export interface JsonRpcEnvelope<T = unknown> {
    jsonrpc: '2.0';
    id: number | string | null;
    result?: T;
    error?: JsonRpcError;
}

/**
 * Raw JSON-RPC POST that bypasses ethers' client-side validation. Ethers v6 normalizes
 * addresses, hexlifies `data`, and re-wraps non-array `params`; negative tests need the
 * malformed bytes to reach the node untouched to verify the *node's* validation, not the
 * client's. Returns the raw envelope.
 */
export async function rawJsonRpc<T = unknown>(
    url: string,
    method: string,
    params: unknown,
    id: number | string = 1,
): Promise<JsonRpcEnvelope<T>> {
    const res = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id, method, params }),
    });
    return res.json() as Promise<JsonRpcEnvelope<T>>;
}

export const rawSei = <T = unknown>(method: string, params: unknown) =>
    rawJsonRpc<T>(Endpoints.sei.evmRpc, method, params);

/** Raw POST to the primary geth reference. */
export const rawGeth = <T = unknown>(method: string, params: unknown) =>
    rawJsonRpc<T>(Endpoints.eth.geth, method, params);

/** Raw POST to a keyless node (hosted RPC) — used to observe the empty-account case. */
export const rawAccountless = <T = unknown>(method: string, params: unknown) =>
    rawJsonRpc<T>(Endpoints.accountless, method, params);

/**
 * Resolve a promise expected to throw an ethers RPC error and return the underlying
 * JSON-RPC envelope. Unwraps both `e.info.error` (ethers v6 default) and `e.error`
 * (older shapes). Throws if the promise resolved or the error carried no RPC envelope.
 */
export async function captureRpcError(promise: Promise<unknown>): Promise<JsonRpcError> {
    try {
        await promise;
    } catch (e: any) {
        const env = e?.info?.error ?? e?.error;
        if (env && typeof env.code === 'number') {
            return env as JsonRpcError;
        }
        throw new Error(
            `captureRpcError: thrown error did not carry an RPC envelope: ${e?.message ?? e}`,
        );
    }
    throw new Error('captureRpcError: expected promise to reject but it resolved');
}

/**
 * Assert a raw JSON-RPC envelope carries an error matching `code` and (optionally)
 * `messagePattern`; returns the error. Throws a descriptive Error (not a chai assertion)
 * so the message includes the whole envelope. Use this for raw-transport negative tests
 * where you POST malformed payloads directly.
 */
export function expectJsonRpcError(
    envelope: JsonRpcEnvelope,
    code: number,
    messagePattern?: RegExp,
): JsonRpcError {
    const err = envelope.error;
    if (!err) {
        throw new Error(
            `expectJsonRpcError: expected an error but got result: ${JSON.stringify(envelope.result)}`,
        );
    }
    if (err.code !== code) {
        throw new Error(
            `expectJsonRpcError: expected code ${code} but got ${err.code} (message: ${err.message})`,
        );
    }
    if (messagePattern && !messagePattern.test(err.message)) {
        throw new Error(
            `expectJsonRpcError: message ${JSON.stringify(err.message)} did not match ${messagePattern}`,
        );
    }
    return err;
}

export const sleep = (ms: number): Promise<void> =>
    new Promise(resolve => setTimeout(resolve, ms));

/**
 * Poll `fn` until it returns truthy or the timeout elapses; returns the truthy value or
 * throws. For short, deterministic guards (wait for the next Sei block, etc.), not retries.
 */
export async function waitUntil<T>(
    fn: () => Promise<T | undefined | null>,
    opts: { timeoutMs: number; intervalMs?: number; label?: string } = { timeoutMs: 30_000 },
): Promise<T> {
    const interval = opts.intervalMs ?? 250;
    const deadline = Date.now() + opts.timeoutMs;
    let lastError: unknown;
    while (Date.now() < deadline) {
        try {
            const result = await fn();
            if (result !== undefined && result !== null && result !== false) {
                return result as T;
            }
        } catch (e) {
            lastError = e;
        }
        await sleep(interval);
    }
    throw new Error(
        `waitUntil(${opts.label ?? 'condition'}) timed out after ${opts.timeoutMs}ms` +
            (lastError ? `: ${(lastError as Error)?.message ?? lastError}` : ''),
    );
}


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
 * Read the live EIP-1559 params from the in-container `seid`. Returns null when no local
 * docker devnet is reachable so callers can degrade to structural-only checks instead of
 * failing on a hosted/remote Sei endpoint.
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

/**
 * A block's number + gas accounting + base fee as native types; the canonical reader
 * shared by the fee-market specs (eth_feeHistory / eth_gasPrice). Accepts a height or
 * the `latest` tag.
 */
export async function blockGasInfo(
    provider: ethers.JsonRpcProvider,
    n: number | 'latest',
): Promise<{ number: number; gasUsed: bigint; gasLimit: bigint; baseFee: bigint }> {
    const tag = typeof n === 'number' ? ethers.toQuantity(n) : n;
    const b = await provider.send('eth_getBlockByNumber', [tag, false]);
    return {
        number: Number(b.number),
        gasUsed: BigInt(b.gasUsed),
        gasLimit: BigInt(b.gasLimit),
        baseFee: BigInt(b.baseFeePerGas ?? '0x0'),
    };
}
