import { ethers } from 'ethers';
import { Endpoints } from '../config/endpoints';

const POLLING_INTERVAL_MS = Number(process.env.RPC_POLLING_INTERVAL_MS ?? 100);

const makeProvider = (url: string): ethers.JsonRpcProvider =>
    new ethers.JsonRpcProvider(url, undefined, {
        batchMaxCount: 1, // RPC tests assert per-request behavior; batching would mask it.
        staticNetwork: true,
        pollingInterval: POLLING_INTERVAL_MS,
    });

let seiProvider: ethers.JsonRpcProvider | undefined;
let gethProvider: ethers.JsonRpcProvider | undefined;
let forkProvider: ethers.JsonRpcProvider | undefined;

export function seiRpc(): ethers.JsonRpcProvider {
    if (!seiProvider) seiProvider = makeProvider(Endpoints.sei.evmRpc);
    return seiProvider;
}

/** Primary Ethereum reference: local geth --dev. */
export function gethRpc(): ethers.JsonRpcProvider {
    if (!gethProvider) gethProvider = makeProvider(Endpoints.eth.geth);
    return gethProvider;
}

/** Optional secondary reference: anvil/Hardhat mainnet fork. */
export function forkRpc(): ethers.JsonRpcProvider {
    if (!forkProvider) forkProvider = makeProvider(Endpoints.eth.fork);
    return forkProvider;
}

/**
 * Sei + the primary geth reference. Most parity specs want exactly these two.
 * `eth` aliases the geth provider so existing specs keep working after the
 * fork→geth reference switch.
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
