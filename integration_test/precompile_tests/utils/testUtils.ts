import fs from 'node:fs';
import path from 'node:path';
import { ethers } from 'ethers';
import { RuntimeStatePath } from '../config/endpoints';
import { EvmAccount } from './evmUtils';

/**
 * Runtime state captured once by _start/00_bootstrap.spec.ts and read by every other spec; the
 * typed contract turns a missing field into a TypeScript error, not a runtime undefined. Add fields
 * here, but never write back from a non-bootstrap spec — the bootstrap is the only writer.
 */
export interface RuntimeState {
    /** ISO timestamp when the state was written. */
    bootstrappedAt: string;

    /** Sei EVM chain id as an integer. */
    chainId: number;

    /** Block numbers captured at well-defined points in the bootstrap. */
    blocks: {
        /** Sei block just before any contracts were deployed. */
        beforeDeploy: number;
        /** Sei block in which PrecompileCaller was deployed. */
        callerDeploy: number;
        /** Sei block just after the bootstrap finished. */
        afterDeploy: number;
    };

    /** Deployed fixture contract addresses. */
    contracts: {
        /** PrecompileCaller: forwards calldata via CALL / STATICCALL / DELEGATECALL. */
        precompileCaller: string;
    };

    /** Accounts pre-funded by the bootstrap, ready for use by specs. */
    funded: {
        /** The admin's EVM address (funded and associated by the bootstrap). */
        admin: string;
        adminMnemonic: string;
        /** The admin's pubkey-derived `sei1…` address (associated, so live on-chain). */
        adminSeiAddress: string;
        /** Pool of fresh accounts the bootstrap funded but has not transacted from. */
        pool: { address: string; privateKey: string }[];
    };
}

const stateAbsPath = (): string => path.resolve(process.cwd(), RuntimeStatePath);

export function writeRuntimeState(state: RuntimeState): void {
    const abs = stateAbsPath();
    fs.mkdirSync(path.dirname(abs), { recursive: true });
    fs.writeFileSync(abs, JSON.stringify(state, null, 2), 'utf-8');
}

let cached: RuntimeState | undefined;

export function readRuntimeState(): RuntimeState {
    if (cached) return cached;
    const abs = stateAbsPath();
    if (!fs.existsSync(abs)) {
        throw new Error(
            `readRuntimeState: ${abs} not found. ` +
                'Run `npm run precompile:bootstrap` (or `npm run test:precompile`) before running spec files individually.',
        );
    }
    cached = JSON.parse(fs.readFileSync(abs, 'utf-8')) as RuntimeState;
    return cached;
}

// The suite runs serially in a single process (see .mocharc.run.json), so a module-level
// cursor can hand every claimPool call a fresh, non-overlapping range of the pool.
let poolCursor = 0;

/**
 * Claim `count` accounts from the pre-funded pool, allocating a disjoint slice on every call so no
 * two specs ever share a key. Throws when exhausted rather than wrapping (which would reintroduce
 * overlap); bump POOL_SIZE in _start/00_bootstrap.spec.ts when adding hungry specs. `label` is
 * diagnostics-only; accounts are returned connected to `provider`.
 */
export function claimPool(
    runtime: RuntimeState,
    provider: ethers.JsonRpcProvider,
    count: number,
    label: string,
): EvmAccount[] {
    const pool = runtime.funded.pool;
    if (poolCursor + count > pool.length) {
        throw new Error(
            `claimPool('${label}', count=${count}): pre-funded pool exhausted ` +
                `(used ${poolCursor}/${pool.length}). Increase POOL_SIZE in _start/00_bootstrap.spec.ts.`,
        );
    }
    const start = poolCursor;
    poolCursor += count;
    return Array.from({ length: count }, (_, i) =>
        EvmAccount.fromPrivateKey(pool[start + i].privateKey, provider),
    );
}
