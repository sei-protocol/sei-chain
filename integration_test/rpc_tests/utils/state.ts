import fs from 'node:fs';
import path from 'node:path';
import { RuntimeStatePath } from '../config/endpoints';

/**
 * Runtime state captured once by _start/00_bootstrap.spec.ts and read by every
 * other spec file. Keeping the contract here means a missing field is a TypeScript
 * error in the spec, not a runtime undefined.
 *
 * Add a field when you need a new precomputed value across specs — never write
 * back to this file from a non-bootstrap spec, or parallel workers will race.
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

    /** EVM addresses pre-funded with a small balance, ready for use by tests. */
    funded: {
        admin: string;
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
