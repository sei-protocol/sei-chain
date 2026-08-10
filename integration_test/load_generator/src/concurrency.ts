export async function mapConcurrent<T, R>(
    values: T[],
    concurrency: number,
    callback: (value: T) => Promise<R>,
): Promise<R[]> {
    const results = new Array<R>(values.length);
    let cursor = 0;
    async function worker(): Promise<void> {
        for (;;) {
            const index = cursor++;
            if (index >= values.length) return;
            results[index] = await callback(values[index]);
        }
    }
    await Promise.all(Array.from({ length: Math.min(concurrency, values.length) }, () => worker()));
    return results;
}
