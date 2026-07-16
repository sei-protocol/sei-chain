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

    seiAddress(): string {
        const compressed = ethers.getBytes(this.wallet.signingKey.compressedPublicKey);
        return toBech32('sei', rawSecp256k1PubkeyToRawAddress(compressed));
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
 * Fund a recipient from an account the node itself holds unlocked, letting the node sign
 * (`eth_sendTransaction`) rather than a local key. This seeds a deployer on `geth --dev`:
 * its pre-funded developer account lives auto-unlocked in the node's keyring and is
 * regenerated on every restart, so we never have its private key client-side, and the
 * funded recipient gets a key we *do* control for subsequent local-signed deploys.
 */
export async function fundFromUnlocked(
    provider: ethers.JsonRpcProvider,
    from: string,
    to: string,
    amountWei: bigint,
    timeoutMs = 60_000,
): Promise<ethers.TransactionReceipt> {
    if (!from) {
        throw new Error(
            'fundFromUnlocked: empty `from` — the geth --dev account was not available ' +
                '(eth_accounts returned nothing). Is the reference node up and unlocked?',
        );
    }
    const hash: string = await provider.send('eth_sendTransaction', [
        { from, to, value: ethers.toQuantity(amountWei) },
    ]);
    const receipt = await provider.waitForTransaction(hash, 1, timeoutMs);
    if (!receipt) {
        throw new Error(
            `fundFromUnlocked: transaction ${hash} did not confirm within ${timeoutMs}ms ` +
                '(is the geth --dev reference mining?)',
        );
    }
    return receipt;
}

/** geth --dev's auto-unlocked first account — the only key it can node-sign with. */
export async function gethUnlockedAccount(geth: ethers.JsonRpcProvider): Promise<string> {
    const accounts: string[] = await geth.send('eth_accounts', []);
    const from = accounts[0];
    if (!from) throw new Error('gethUnlockedAccount: geth --dev returned no unlocked account');
    return from;
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

/**
 * Returns the *runtime* (deployed) bytecode for a known artifact — the exact bytes
 * eth_getCode reports for a successfully deployed instance (no constructor/init code).
 */
export function deployedBytecodeOf(solFile: string, contractName?: string): string {
    return loadArtifact(solFile, contractName).deployedBytecode;
}

export const SIMPLE_ACCOUNT_ABI = [
    {
        inputs: [
            {
                components: [
                    { internalType: 'address', name: 'target', type: 'address' },
                    { internalType: 'uint256', name: 'value', type: 'uint256' },
                    { internalType: 'bytes', name: 'data', type: 'bytes' },
                ],
                internalType: 'struct BaseAccount.Call[]',
                name: 'calls',
                type: 'tuple[]',
            },
        ],
        name: 'executeBatch',
        outputs: [],
        stateMutability: 'nonpayable',
        type: 'function',
    },
] as const;

/** The 0xef0100-prefixed delegation designator geth/Sei store as an EOA's code. */
export function delegationDesignator(implementationAddress: string): string {
    return '0xef0100' + implementationAddress.replace(/^0x/, '').toLowerCase();
}

/**
 * Sign a self-authorization delegating `account` to `implementationAddress`. For a
 * self-sponsored type-4 tx the authorization nonce is the account's current nonce
 * + 1, because the outer tx consumes the current nonce first.
 */
export async function selfAuthorize(
    account: EvmAccount,
    implementationAddress: string,
): Promise<ethers.Authorization> {
    const provider = account.wallet.provider!;
    const [{ chainId }, latest] = await Promise.all([
        provider.getNetwork(),
        provider.getTransactionCount(account.address, 'latest'),
    ]);
    return account.wallet.authorize({
        address: implementationAddress,
        chainId,
        nonce: latest + 1,
    });
}

/** Broadcast a type-4 tx that installs the delegation designator on `account` itself. */
export async function setCodeForEOA(
    account: EvmAccount,
    authorizationList: ethers.Authorization[],
): Promise<ethers.TransactionReceipt | null> {
    const provider = account.wallet.provider!;
    const fee = await provider.getFeeData();
    const tx = await account.wallet.sendTransaction({
        to: account.address,
        data: '0x',
        maxFeePerGas: fee.maxFeePerGas!,
        maxPriorityFeePerGas: fee.maxPriorityFeePerGas!,
        authorizationList,
        type: 4,
    });
    return tx.wait();
}

/**
 * Encode a signed EIP-7702 authorization into the hex-quantity shape the JSON-RPC
 * `authorizationList` expects (used by eth_estimateGas / eth_call against type-4 txs).
 */
export function authToRpc(a: ethers.Authorization): Record<string, string> {
    return {
        chainId: ethers.toQuantity(a.chainId),
        address: a.address,
        nonce: ethers.toQuantity(a.nonce),
        yParity: ethers.toQuantity(a.signature.yParity),
        // r/s must be canonical hex *quantities* (no leading zero digits): geth parses
        // authorizationList.r/s as uint256 and rejects the 32-byte zero-padded form
        // (ethers' signature.r/.s) with "leading zero digits" whenever the top nibble
        // is 0 — an intermittent ~12%/sig failure. toQuantity strips the padding.
        r: ethers.toQuantity(a.signature.r),
        s: ethers.toQuantity(a.signature.s),
    };
}
