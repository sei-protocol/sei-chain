/**
 * The file that carries one run's observations to the next.
 *
 * The two phases are separated by a real governance upgrade, so they are
 * separate processes minutes or days apart, possibly on different machines.
 * Everything the verify phase needs has to survive in this file, and it records
 * which chain and height it came from so a mismatched pair fails loudly instead
 * of comparing unrelated observations.
 */
import fs from 'node:fs';
import path from 'node:path';
import type { ModuleVersion } from './cosmos';
import type { ArchiveRead, ProbeResult } from './probes';

/** A transaction the record phase put on chain, to be re-read afterwards. */
export interface RecordedTx {
    label: string;
    hash: string;
    blockNumber: number;
    /** 0 for a failed transaction, 1 for a successful one. */
    status: number;
    /** eth_getVMError for a failed transaction, when the node served one. */
    vmError?: string;
}

export interface Artifact {
    schema: 1;
    meta: {
        network: string;
        evmChainId: string;
        planName: string;
        /** EVM block height the observations were taken at. */
        blockNumber: number;
        /**
         * Cosmos block height at record time. Kept separately from the EVM
         * height because applied_plan reports a Cosmos height, and the gate
         * compares the two.
         */
        cosmosHeight: number;
        /** The seid version the endpoint reported, which is what was observed. */
        seidVersion: string;
        recordedAt: string;
    };
    probes: ProbeResult[];
    archiveReads: ArchiveRead[];
    transactions: RecordedTx[];
    /** The chain's whole module version map at record time, sorted by name. */
    moduleVersions: ModuleVersion[];
}

const DEFAULT_PATH = 'artifacts/pre-upgrade.json';

export function artifactPath(env: NodeJS.ProcessEnv = process.env): string {
    return path.resolve(env.UPGRADE_TEST_ARTIFACT ?? DEFAULT_PATH);
}

export function writeArtifact(artifact: Artifact, env: NodeJS.ProcessEnv = process.env): string {
    const target = artifactPath(env);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, `${JSON.stringify(artifact, null, 2)}\n`);
    return target;
}

export function readArtifact(env: NodeJS.ProcessEnv = process.env): Artifact {
    const source = artifactPath(env);
    if (!fs.existsSync(source)) {
        throw new Error(
            `No pre-upgrade artifact at ${source}. Run the record phase before the upgrade ` +
                '(npm run record), keep the file, and point UPGRADE_TEST_ARTIFACT at it afterwards.',
        );
    }
    const artifact = JSON.parse(fs.readFileSync(source, 'utf-8')) as Artifact;
    if (artifact.schema !== 1) {
        throw new Error(`${source}: unsupported artifact schema ${artifact.schema}`);
    }
    return artifact;
}

/**
 * Refuse an artifact recorded against a different chain. Comparing observations
 * across networks would produce a confident diff about nothing.
 */
export function assertSameChain(artifact: Artifact, network: string, evmChainId: bigint): void {
    if (artifact.meta.evmChainId !== evmChainId.toString()) {
        throw new Error(
            `artifact was recorded against EVM chain ${artifact.meta.evmChainId} (${artifact.meta.network}) ` +
                `but this run targets ${evmChainId} (${network})`,
        );
    }
}

export function probeValue(artifact: Artifact, name: string): string | undefined {
    return artifact.probes.find(p => p.name === name)?.value;
}

/**
 * The gate on the verify phase: the upgrade must have run, and it must have run
 * after the artifact was taken.
 *
 * Both halves matter. Without the first, a run against a chain that has not
 * upgraded yet reports a clean pass on everything that must not change and a
 * baffling failure on everything that must — the opposite of a useful signal.
 * Without the second, an artifact recorded after the upgrade would be compared
 * against itself and pass while proving nothing.
 *
 * Returns the message explaining the refusal, or undefined when the run may
 * proceed. Pure so it can be tested without a chain.
 */
export function upgradeGateRefusal(args: {
    planName: string;
    network: string;
    appliedHeight: number;
    artifactCosmosHeight: number;
    /** Rendered into the message so the reader can see how far off the chain is. */
    context: string;
}): string | undefined {
    const { planName, network, appliedHeight, artifactCosmosHeight, context } = args;
    if (appliedHeight === 0) {
        return (
            `${network} has not applied ${planName} yet (applied_plan reports 0 at ${context}). ` +
            'Wait for the upgrade to land on this network, then re-run.'
        );
    }
    if (appliedHeight <= artifactCosmosHeight) {
        return (
            `${planName} was applied at height ${appliedHeight}, at or before the artifact was ` +
            `recorded at height ${artifactCosmosHeight}. The artifact already describes an ` +
            'upgraded chain, so there is no pre-upgrade state to compare against.'
        );
    }
    return undefined;
}
