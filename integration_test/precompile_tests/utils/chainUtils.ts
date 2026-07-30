import { ethers } from 'ethers';
import { Endpoints } from '../config/endpoints';

const POLLING_INTERVAL_MS = Number(process.env.PRECOMPILE_POLLING_INTERVAL_MS ?? 100);

const makeProvider = (url: string): ethers.JsonRpcProvider =>
    new ethers.JsonRpcProvider(url, undefined, {
        batchMaxCount: 1,
        staticNetwork: true,
        pollingInterval: POLLING_INTERVAL_MS,
    });

let seiProvider: ethers.JsonRpcProvider | undefined;

export function seiRpc(): ethers.JsonRpcProvider {
    if (!seiProvider) seiProvider = makeProvider(Endpoints.sei.evmRpc);
    return seiProvider;
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
