import fs from 'node:fs';
import path from 'node:path';
import { ethers } from 'ethers';
import { expect } from 'chai';
import { rawSei } from './chainUtils';

/**
 * Canonical addresses of Sei's custom precompiles, mirroring the constants in
 * precompiles/<name>/<name>.go. The suite loads each precompile's ABI from the
 * repo's own precompiles/<name>/abi.json — the single source of truth the chain
 * binary embeds — so tests can never drift from the deployed interface.
 */
export const PRECOMPILE_ADDRESSES = {
    bank: '0x0000000000000000000000000000000000001001',
    wasmd: '0x0000000000000000000000000000000000001002',
    json: '0x0000000000000000000000000000000000001003',
    addr: '0x0000000000000000000000000000000000001004',
    staking: '0x0000000000000000000000000000000000001005',
    gov: '0x0000000000000000000000000000000000001006',
    distribution: '0x0000000000000000000000000000000000001007',
    oracle: '0x0000000000000000000000000000000000001008',
    pointerview: '0x000000000000000000000000000000000000100A',
    pointer: '0x000000000000000000000000000000000000100b',
    solo: '0x000000000000000000000000000000000000100C',
} as const;

export type PrecompileName = keyof typeof PRECOMPILE_ADDRESSES;

/** Repo-root precompiles/ dir (this module lives at integration_test/precompile_tests/utils). */
const PRECOMPILES_ROOT = path.resolve(__dirname, '..', '..', '..', 'precompiles');

const abiCache = new Map<PrecompileName, any[]>();

/** The ABI of a precompile, loaded from the repo's precompiles/<name>/abi.json. */
export function precompileAbi(name: PrecompileName): any[] {
    const cached = abiCache.get(name);
    if (cached) return cached;
    const abiPath = path.join(PRECOMPILES_ROOT, name, 'abi.json');
    if (!fs.existsSync(abiPath)) {
        throw new Error(
            `precompileAbi(${name}): ${abiPath} not found — run the suite from a full sei-chain checkout.`,
        );
    }
    const abi = JSON.parse(fs.readFileSync(abiPath, 'utf-8')) as any[];
    abiCache.set(name, abi);
    return abi;
}

/** An ethers Interface for calldata encoding/decoding against a precompile. */
export function precompileInterface(name: PrecompileName): ethers.Interface {
    return new ethers.Interface(precompileAbi(name));
}

/** An ethers Contract bound to the precompile's fixed address. */
export function precompileContract(
    name: PrecompileName,
    runner: ethers.ContractRunner,
): ethers.Contract {
    return new ethers.Contract(PRECOMPILE_ADDRESSES[name], precompileAbi(name), runner);
}

/**
 * Assert `promise` (an ethers call/tx) rejects with an execution revert; returns the
 * error message so callers can additionally match the precompile's revert reason when
 * the node surfaces one.
 */
export async function expectExecutionReverted(
    promise: Promise<unknown>,
    label: string,
): Promise<string> {
    try {
        await promise;
    } catch (e: any) {
        const message: string = e?.info?.error?.message ?? e?.shortMessage ?? e?.message ?? String(e);
        expect(message, `${label}: expected an execution revert, got: ${message}`).to.match(
            /execution reverted|revert/i,
        );
        return message;
    }
    throw new Error(`${label}: expected the call to revert but it succeeded`);
}

interface CallTrace {
    error?: string;
    revertReason?: string;
    calls?: CallTrace[];
}

/** debug_traceTransaction with the callTracer; returns the top-level call frame. */
export async function traceTransaction(txHash: string): Promise<CallTrace> {
    const envelope = await rawSei<CallTrace>('debug_traceTransaction', [
        txHash,
        { tracer: 'callTracer' },
    ]);
    if (envelope.error) {
        throw new Error(
            `traceTransaction(${txHash}): ${envelope.error.code} ${envelope.error.message}`,
        );
    }
    if (!envelope.result) {
        throw new Error(`traceTransaction(${txHash}): empty result`);
    }
    return envelope.result;
}

/**
 * The load-bearing legacy assertion: a precompile that runs out of gas mid-execution
 * must surface as a plain EVM "execution reverted" in traces — never as a Go panic
 * (a "panic occurred" trace would mean a consensus-relevant unhandled error path).
 */
export async function expectTraceRevertedNotPanicked(txHash: string): Promise<void> {
    const trace = await traceTransaction(txHash);
    const error = trace.error ?? '';
    expect(error, `trace of ${txHash} must carry an error`).to.not.equal('');
    expect(error, 'precompile failure must not surface as a panic').to.not.include('panic');
    expect(error, 'precompile failure must trace as an execution revert').to.include(
        'execution reverted',
    );
}
