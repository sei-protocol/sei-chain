import fs from 'node:fs';
import path from 'node:path';
import { ethers } from 'ethers';
import { expect } from 'chai';
import { RuntimeStatePath } from '../config/endpoints';
import { EvmAccount } from './evmUtils';
import { JsonRpcEnvelope } from './chainUtils';
import { uint256Word } from './format';

/**
 * Runtime state captured once by _start/00_bootstrap.spec.ts and read by every other spec; the
 * typed contract turns a missing field into a TypeScript error, not a runtime undefined. Add fields
 * here, but never write back from a non-bootstrap spec — the bootstrap is the only writer.
 */
export interface RuntimeState {
    /** ISO timestamp when the state was written. */
    bootstrappedAt: string;

    /** Chain IDs as integers. */
    chainIds: {
        sei: number;
        eth: number;
    };

    /** Block numbers captured at well-defined points in the bootstrap. */
    blocks: {
        /** Sei block just before any contracts were deployed. */
        seiBeforeDeploy: number;
        /** Sei block in which TestERC20 was deployed. */
        seiErc20Deploy: number;
        /** Sei block just after the bootstrap finished. */
        seiAfterDeploy: number;
        /** geth reference head when the bootstrap ran. */
        ethAtBootstrap: number;
        /** geth block in which the mirrored TestERC20 was deployed. */
        ethErc20Deploy: number;
    };

    /** Deployed contract addresses, one per reference chain. */
    contracts: {
        /** TestERC20 on Sei. */
        erc20: string;
        /** The same TestERC20 deployed on the geth reference, for parity tests. */
        erc20Geth: string;
        /** SimpleAccount7702 delegation target on Sei (used by EIP-7702 specs). */
        simpleAccount7702: string;
        /** RealGasBurner on Sei: lets specs burn arbitrary gas to move the base fee. */
        gasBurner: string;
    };

    /**
     * CW20 + its EVM (ERC20) pointer, populated only when wasm is enabled on the chain.
     * Absent on wasm-disabled chains, so consumers must treat it as optional and skip the
     * dual-VM / pointer paths when it is undefined.
     */
    wasm?: {
        cw20: string;
        cw20Pointer: string;
        actor: { address: string; privateKey: string };
    };

    /** EVM addresses pre-funded with a small balance, ready for use by tests. */
    funded: {
        admin: string;
        adminMnemonic: string;
        /**
         * Deployer/owner of the geth-side TestERC20. Funded from geth's unlocked dev
         * account; this key is controlled client-side so specs can sign geth txs.
         */
        gethAdmin: { address: string; privateKey: string };
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
                'Run `yarn rpc:bootstrap` (or `yarn test:rpc`) before running spec files individually.',
        );
    }
    cached = JSON.parse(fs.readFileSync(abs, 'utf-8')) as RuntimeState;
    return cached;
}

/**
 * Assert two JSON-RPC envelopes carry byte-identical errors (code, message and data).
 * Used by the parity specs to prove Sei and the geth reference fail the exact same way.
 */
export function expectSameError(s: JsonRpcEnvelope, g: JsonRpcEnvelope): void {
    expect(g.error, `geth must error, got result ${JSON.stringify(g.result)}`).to.not.equal(
        undefined,
    );
    expect(s.error, `sei must error, got result ${JSON.stringify(s.result)}`).to.not.equal(
        undefined,
    );
    expect(s.error!.code, 'error.code parity').to.equal(g.error!.code);
    expect(s.error!.message, 'error.message parity').to.equal(g.error!.message);
    expect(s.error!.data, 'error.data parity').to.deep.equal(g.error!.data);
}

// The suite runs serially in a single process (see .mocharc.run.json), so a module-level
// cursor can hand every claimPool call a fresh, non-overlapping range of the pool.
let poolCursor = 0;

/**
 * Claim `count` accounts from the pre-funded pool, allocating a disjoint slice on every call so no
 * two specs ever share a key (the old salted-offset scheme could wrap and overlap, reusing accounts
 * and causing nonce collisions, balance drain and flaky heavy-block / fee-market failures). Throws
 * when exhausted rather than wrapping (which would reintroduce overlap); bump POOL_SIZE in
 * _start/00_bootstrap.spec.ts when adding hungry specs. `label` is diagnostics-only; accounts are
 * returned connected to `provider`.
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

/** Left-pad a uint into its canonical 32-byte ABI word. */
export const encodeUint = uint256Word;

/** Calldata encoders and result decoders bound to a specific ERC20 ABI. */
export class Erc20Calldata {
    constructor(private readonly iface: ethers.Interface) {}

    balanceOf(holder: string): string {
        return this.iface.encodeFunctionData('balanceOf', [holder]);
    }

    transfer(to: string, amount: bigint): string {
        return this.iface.encodeFunctionData('transfer', [to, amount]);
    }

    decodeBalance(hex: string): bigint {
        return this.iface.decodeFunctionResult('balanceOf', hex)[0] as bigint;
    }
}
