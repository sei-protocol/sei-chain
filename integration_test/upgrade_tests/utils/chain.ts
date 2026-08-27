/**
 * Transport helpers. Mirrors integration_test/precompile_tests/utils/chainUtils.ts,
 * kept local so this suite has no cross-package import.
 */
import fs from 'node:fs';
import path from 'node:path';
import { ethers } from 'ethers';
import { resolveTarget } from '../config/targets';

export interface JsonRpcError {
    code: number;
    message: string;
    data?: unknown;
}

export interface JsonRpcEnvelope<T = unknown> {
    result?: T;
    error?: JsonRpcError;
}

let provider: ethers.JsonRpcProvider | undefined;

export function seiRpc(): ethers.JsonRpcProvider {
    if (!provider) {
        provider = new ethers.JsonRpcProvider(resolveTarget().evmRpcUrl, undefined, {
            batchMaxCount: 1,
            staticNetwork: true,
        });
    }
    return provider;
}

/**
 * Raw JSON-RPC POST. Precompile reverts are asserted through the raw envelope
 * because ethers rewrites the node's error message on the way out, and the exact
 * message is the thing being compared across the upgrade.
 */
export async function rawSei<T = unknown>(
    method: string,
    params: unknown,
): Promise<JsonRpcEnvelope<T>> {
    const res = await fetch(resolveTarget().evmRpcUrl, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
    });
    return res.json() as Promise<JsonRpcEnvelope<T>>;
}

/**
 * Fail before any probe runs if the endpoint is not the network the caller named.
 * Both phases call this, which is what stops a record/verify pair from spanning
 * two different chains.
 */
export async function assertExpectedChain(): Promise<void> {
    const target = resolveTarget();
    const envelope = await rawSei<string>('eth_chainId', []);
    if (envelope.error || !envelope.result) {
        throw new Error(
            `${target.evmRpcUrl} did not answer eth_chainId: ${envelope.error?.message ?? 'empty result'}`,
        );
    }
    const actual = BigInt(envelope.result);
    if (actual !== target.evmChainId) {
        throw new Error(
            `${target.evmRpcUrl} reports EVM chain id ${actual}, but ${target.name} is ${target.evmChainId}. ` +
                'Set UPGRADE_TEST_NETWORK or UPGRADE_TEST_EVM_RPC so they agree.',
        );
    }
}

/** Repo-root precompiles/ dir (this module lives at integration_test/upgrade_tests/utils). */
const PRECOMPILES_ROOT = path.resolve(__dirname, '..', '..', '..', 'precompiles');

const abiCache = new Map<string, ethers.Interface>();

/**
 * A precompile's interface, loaded from the repo's own precompiles/<name>/abi.json
 * — the same file the chain binary embeds, so the suite cannot drift from the
 * deployed interface.
 */
export function precompileInterface(name: string): ethers.Interface {
    const cached = abiCache.get(name);
    if (cached) return cached;
    const abiPath = path.join(PRECOMPILES_ROOT, name, 'abi.json');
    if (!fs.existsSync(abiPath)) {
        throw new Error(`precompileInterface(${name}): ${abiPath} not found`);
    }
    const iface = new ethers.Interface(JSON.parse(fs.readFileSync(abiPath, 'utf-8')));
    abiCache.set(name, iface);
    return iface;
}

/** Canonical precompile addresses, mirroring precompiles/<name>/<name>.go. */
export const ADDRESSES = {
    oracle: '0x0000000000000000000000000000000000001008',
    /** Removed in v6.7; there is no ABI in the tree for it any more. */
    feegrant: '0x0000000000000000000000000000000000001010',
    upgrade: '0x0000000000000000000000000000000000001015',
} as const;
