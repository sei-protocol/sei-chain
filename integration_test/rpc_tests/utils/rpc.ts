import { Endpoints } from '../config/endpoints';

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
 * Raw JSON-RPC POST that bypasses ethers' client-side validation.
 *
 * Ethers v6 normalises addresses, hexlifies `data`, and re-wraps non-array `params`
 * into an array inside JsonRpcProvider.send. For negative tests that send
 * deliberately malformed payloads, we need the bytes to reach the node untouched so
 * we can verify the *node's* validation, not the client's. Returns the raw envelope.
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

/** Raw POST to the optional anvil/Hardhat fork. */
export const rawFork = <T = unknown>(method: string, params: unknown) =>
    rawJsonRpc<T>(Endpoints.eth.fork, method, params);

/** Raw POST to a keyless node (hosted RPC) — used to observe the empty-account case. */
export const rawAccountless = <T = unknown>(method: string, params: unknown) =>
    rawJsonRpc<T>(Endpoints.accountless, method, params);

/** Back-compat alias: `eth` reference is now geth. */
export const rawEth = rawGeth;

/**
 * Resolve a promise expected to throw an ethers RPC error and return the underlying
 * JSON-RPC envelope. We unwrap both `e.info.error` (ethers v6 default) and `e.error`
 * (older shapes) so tests do not have to know which shape they got.
 *
 * Throws if the promise resolved successfully, or if the thrown error does not
 * carry an RPC envelope — both of those are test-author bugs, not test failures.
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
 * Assert that a raw JSON-RPC envelope carries an error matching `code` and
 * (optionally) `messagePattern`. Returns the error for further inspection.
 *
 * Throws a descriptive Error (not a chai assertion) so the failure message includes
 * the whole envelope — useful when a node returns an error shaped differently than
 * expected. Use this for raw-transport negative tests where you POST malformed
 * payloads directly.
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
