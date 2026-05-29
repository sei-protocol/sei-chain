import { ethers, Contract, ContractFactory } from 'ethers';
import path from 'node:path';
import fs from 'node:fs';
import { EvmAccount } from './wallet';

/**
 * Minimal artifact loader that reads Hardhat-style JSON artifacts from this
 * module's own `artifacts/contracts/<File>.sol/<Contract>.json` tree, produced by
 * `npm run compile` (see ./contracts and ../hardhat.config.ts). We deliberately
 * read these via fs at runtime rather than via `import ... from '...'` so the
 * loader works regardless of which directory the spec lives in, and so the suite
 * stays self-contained — it never reaches outside this folder.
 */
const ARTIFACTS_ROOT = path.resolve(__dirname, '..', 'artifacts', 'contracts');

interface HardhatArtifact {
    contractName: string;
    abi: any[];
    bytecode: string;
}

function loadArtifact(solFile: string, contractName?: string): HardhatArtifact {
    const name = contractName ?? solFile.replace(/\.sol$/, '');
    const artifactPath = path.join(ARTIFACTS_ROOT, solFile, `${name}.json`);
    if (!fs.existsSync(artifactPath)) {
        throw new Error(
            `loadArtifact: ${artifactPath} not found. Run \`npm run compile\` first.`,
        );
    }
    return JSON.parse(fs.readFileSync(artifactPath, 'utf-8')) as HardhatArtifact;
}

/**
 * Deploy any artifact-backed contract. Returns the deployed contract instance
 * plus the deploy receipt so callers can record `blockNumber`.
 */
export async function deployContract(
    deployer: EvmAccount,
    solFile: string,
    args: unknown[] = [],
    contractName?: string,
): Promise<{ contract: Contract; address: string; receipt: ethers.TransactionReceipt }> {
    const artifact = loadArtifact(solFile, contractName);
    const factory = new ContractFactory(artifact.abi, artifact.bytecode, deployer.wallet);
    const contract = await factory.deploy(...args);
    const tx = contract.deploymentTransaction();
    if (!tx) throw new Error(`deployContract(${solFile}): no deployment transaction returned`);
    const receipt = await tx.wait();
    if (!receipt) throw new Error(`deployContract(${solFile}): deploy tx did not confirm`);
    const address = await contract.getAddress();
    return { contract: contract as Contract, address, receipt };
}

/**
 * Convenience wrapper for the canonical ERC20 used across the RPC suite.
 * Constructor: `constructor(address initialOwner)` — see contracts/TestERC20.sol.
 */
export async function deployTestErc20(
    deployer: EvmAccount,
    initialOwner = deployer.address,
) {
    return deployContract(deployer, 'TestERC20.sol', [initialOwner], 'TestERC20');
}

/**
 * Returns the parsed ABI for a known artifact. Use this when you only need to
 * encode/decode calldata against an already-deployed address.
 */
export function abiOf(solFile: string, contractName?: string): any[] {
    return loadArtifact(solFile, contractName).abi;
}

/** Returns the creation bytecode for a known artifact (for deploy-gas estimation). */
export function bytecodeOf(solFile: string, contractName?: string): string {
    return loadArtifact(solFile, contractName).bytecode;
}
