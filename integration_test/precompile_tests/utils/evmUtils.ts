import { ethers, HDNodeWallet, Wallet, Contract, ContractFactory } from 'ethers';
import path from 'node:path';
import fs from 'node:fs';
import { rawSecp256k1PubkeyToRawAddress } from '@cosmjs/amino';
import { toBech32 } from '@cosmjs/encoding';
import { seiRpc } from './chainUtils';
import { SEI_HD_PATH } from './constants';

export class EvmAccount {
    readonly wallet: HDNodeWallet | Wallet;
    readonly address: string;

    private constructor(wallet: HDNodeWallet | Wallet) {
        this.wallet = wallet;
        this.address = wallet.address;
    }

    static fromMnemonic(mnemonic: string, provider = seiRpc()): EvmAccount {
        const wallet = ethers.HDNodeWallet.fromPhrase(mnemonic, '', SEI_HD_PATH).connect(provider);
        return new EvmAccount(wallet);
    }

    static fromPrivateKey(privateKey: string, provider = seiRpc()): EvmAccount {
        const wallet = new ethers.Wallet(privateKey, provider);
        return new EvmAccount(wallet);
    }

    static random(provider = seiRpc()): EvmAccount {
        const wallet = ethers.Wallet.createRandom().connect(provider);
        return new EvmAccount(wallet);
    }

    nonce(blockTag: ethers.BlockTag = 'latest'): Promise<number> {
        return this.wallet.provider!.getTransactionCount(this.address, blockTag);
    }

    balance(blockTag: ethers.BlockTag = 'latest'): Promise<bigint> {
        return this.wallet.provider!.getBalance(this.address, blockTag);
    }

    /** The account's pubkey-derived `sei1…` address (its cosmos address once associated). */
    seiAddress(): string {
        const compressed = ethers.getBytes(this.wallet.signingKey.compressedPublicKey);
        return toBech32('sei', rawSecp256k1PubkeyToRawAddress(compressed));
    }

    /** The compressed secp256k1 pubkey as bare hex (no 0x) — the addr precompile's associatePubKey input. */
    compressedPubKeyHex(): string {
        return this.wallet.signingKey.compressedPublicKey.slice(2);
    }
}

/**
 * Send native sei (in wei) from `from` to `to` and wait for inclusion. Used by the
 * bootstrap to seed fresh EVM accounts; returns the receipt so callers can record the
 * block number it landed in.
 */
export async function fundEvm(
    from: EvmAccount,
    to: string,
    amountWei: bigint,
): Promise<ethers.TransactionReceipt> {
    const tx = await from.wallet.sendTransaction({ to, value: amountWei });
    const receipt = await tx.wait();
    if (!receipt) {
        throw new Error(`fundEvm: transaction ${tx.hash} did not confirm`);
    }
    return receipt;
}

/**
 * Reveal an account's pubkey on-chain (associating it) via a 0-value self-send.
 * Until an account is associated Sei cannot map its EVM address to its
 * pubkey-derived sei address, which precompiles like bank.sendNative require.
 */
export async function associateViaTx(account: EvmAccount): Promise<ethers.TransactionReceipt> {
    return fundEvm(account, account.address, 0n);
}

/**
 * Fund many recipients in parallel from a single funder: assign nonces sequentially but
 * broadcast concurrently — Sei's mempool accepts gapless nonces from the same sender, so
 * this is the fastest correct pattern.
 */
export async function fundManyEvm(
    from: EvmAccount,
    recipients: string[],
    amountWei: bigint,
): Promise<ethers.TransactionReceipt[]> {
    if (recipients.length === 0) return [];
    const startNonce = await from.nonce('pending');
    const txs = await Promise.all(
        recipients.map((to, i) =>
            from.wallet.sendTransaction({ to, value: amountWei, nonce: startNonce + i }),
        ),
    );
    const receipts = await Promise.all(txs.map(t => t.wait()));
    receipts.forEach((r, i) => {
        if (!r) throw new Error(`fundManyEvm: tx ${txs[i].hash} did not confirm`);
        if (r.status !== 1) {
            throw new Error(
                `fundManyEvm: funding ${recipients[i]} reverted (status ${r.status}) in tx ${txs[i].hash}`,
            );
        }
    });
    return receipts as ethers.TransactionReceipt[];
}

/**
 * Minimal artifact loader reading Hardhat-style JSON artifacts from this module's own
 * `artifacts/contracts/<File>.sol/<Contract>.json` tree, produced by `npm run compile`.
 * Read via fs at runtime rather than `import` so the loader works regardless of which
 * directory the spec lives in and the suite stays self-contained.
 */
const ARTIFACTS_ROOT = path.resolve(__dirname, '..', 'artifacts', 'contracts');

interface HardhatArtifact {
    contractName: string;
    abi: any[];
    bytecode: string;
    deployedBytecode: string;
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
 * Returns the parsed ABI for a known artifact. Use this when you only need to
 * encode/decode calldata against an already-deployed address.
 */
export function abiOf(solFile: string, contractName?: string): any[] {
    return loadArtifact(solFile, contractName).abi;
}
