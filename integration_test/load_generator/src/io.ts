import fs from 'node:fs/promises';
import path from 'node:path';

export async function writeJsonAtomic(file: string, value: unknown): Promise<void> {
    await fs.mkdir(path.dirname(file), { recursive: true });
    const temporary = `${file}.tmp-${process.pid}`;
    await fs.writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, 'utf8');
    await fs.rename(temporary, file);
}

export async function readJson<T>(file: string): Promise<T> {
    return JSON.parse(await fs.readFile(file, 'utf8')) as T;
}

export async function readOptionalJson<T>(file: string): Promise<T | undefined> {
    try {
        return await readJson<T>(file);
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code === 'ENOENT') return undefined;
        throw error;
    }
}
