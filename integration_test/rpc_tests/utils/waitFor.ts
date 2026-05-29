export const sleep = (ms: number): Promise<void> =>
    new Promise(resolve => setTimeout(resolve, ms));

/**
 * Poll `fn` until it returns truthy or the timeout elapses. Returns the truthy value
 * or throws. Intended for "wait for the next Sei block to land", "wait until the
 * Hardhat fork is reachable", etc. — short, deterministic guards, not retries.
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
