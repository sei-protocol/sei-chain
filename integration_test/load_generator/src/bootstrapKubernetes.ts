import { execFile as execFileCallback, spawn } from 'node:child_process';
import { promisify } from 'node:util';
import { coins } from '@cosmjs/amino';
import { SigningStargateClient } from '@cosmjs/stargate';
import { Encoder } from '@sei-js/cosmos/encoding';
import { ethers } from 'ethers';
import {
    loadTargetConfig,
    seiToUsei,
    verifyTargetCosmosRpc,
} from './config';
import { sendFundingBatches } from './funding';
import { cosmosWalletAt, privateKeyAt, replayRegistry } from './keys';
import { queryEvmAssociation } from './association';

const execFile = promisify(execFileCallback);

interface BootstrapAccount {
    secretName: string;
    mnemonic: string;
    address: string;
}

export async function bootstrapKubernetesMain(): Promise<void> {
    const target = loadTargetConfig();
    const namespace = (process.env.K8S_NAMESPACE ?? 'loadgen').trim();
    const secretNames = parseSecretNames(process.env.K8S_ACCOUNT_SECRETS);
    const targetUsei = seiToUsei(
        process.env.K8S_ACCOUNT_FUND_SEI ?? '',
        'K8S_ACCOUNT_FUND_SEI',
    );
    if (!target.mnemonic) {
        throw new Error('TARGET_MNEMONIC or SEI_ADMIN_MNEMONIC is required as the treasury');
    }
    if (process.env.K8S_BOOTSTRAP_EXECUTE !== '1') {
        console.log(
            `Dry-run: bootstrap ${secretNames.length} accounts in namespace ${namespace}, ` +
                `${process.env.K8S_ACCOUNT_FUND_SEI} SEI each. ` +
                'Set K8S_BOOTSTRAP_EXECUTE=1 to continue.',
        );
        return;
    }

    await applyKubernetesObject({
        apiVersion: 'v1',
        kind: 'Namespace',
        metadata: { name: namespace },
    });
    const accounts = await Promise.all(
        secretNames.map(secretName => loadOrCreateAccount(namespace, secretName)),
    );
    ensureUniqueAccounts(accounts);

    const treasuryWallet = await cosmosWalletAt(target.mnemonic, 0);
    const treasuryAddress = (await treasuryWallet.getAccounts())[0].address;
    if (accounts.some(account => account.address === treasuryAddress)) {
        throw new Error('A bootstrap account resolves to the treasury address');
    }
    const client = await SigningStargateClient.connectWithSigner(
        target.cosmosRpcUrl,
        treasuryWallet,
        {
            registry: replayRegistry(),
            broadcastPollIntervalMs: 200,
        },
    );
    try {
        await verifyTargetCosmosRpc(target, client);
        const funding = await Promise.all(
            accounts.map(async account => {
                const balance = BigInt((await client.getBalance(account.address, 'usei')).amount);
                console.log(
                    `${account.secretName}: ${account.address}, ` +
                        `${balance} / ${targetUsei} usei`,
                );
                return {
                    address: account.address,
                    amount: balance < targetUsei ? targetUsei - balance : 0n,
                };
            }),
        );
        await sendFundingBatches(
            client,
            treasuryAddress,
            funding,
            `bootstrap ${target.network} Kubernetes load runners`,
        );
        await associateAccounts(target.cosmosRpcUrl, target.network, accounts);
        console.log(`Kubernetes accounts ready in namespace ${namespace}`);
    } finally {
        client.disconnect();
    }
}

async function associateAccounts(
    cosmosRpcUrl: string,
    network: string,
    accounts: BootstrapAccount[],
): Promise<void> {
    for (const account of accounts) {
        const expectedEvmAddress = new ethers.Wallet(privateKeyAt(account.mnemonic, 0)).address;
        const mapping = await queryEvmAssociation(cosmosRpcUrl, account.address);
        if (mapping.associated) {
            if (mapping.evmAddress.toLowerCase() !== expectedEvmAddress.toLowerCase()) {
                throw new Error(
                    `${account.secretName} is associated with ${mapping.evmAddress}, ` +
                        `expected ${expectedEvmAddress}`,
                );
            }
            continue;
        }
        const wallet = await cosmosWalletAt(account.mnemonic, 0);
        const client = await SigningStargateClient.connectWithSigner(cosmosRpcUrl, wallet, {
            registry: replayRegistry(),
            broadcastPollIntervalMs: 200,
        });
        try {
            const message = Encoder.evm.MsgAssociate.fromPartial({
                sender: account.address,
                custom_message: `${network} Kubernetes load runner`,
            });
            const result = await client.signAndBroadcast(
                account.address,
                [{ typeUrl: `/${Encoder.evm.MsgAssociate.$type}`, value: message }],
                { amount: coins('21000', 'usei'), gas: '200000' },
                `associate ${network} Kubernetes load runner`,
            );
            if (result.code !== 0) {
                throw new Error(`Association failed for ${account.secretName}: ${result.rawLog}`);
            }
            console.log(`Associated account 0 for ${account.secretName}`);
        } finally {
            client.disconnect();
        }
    }
}

function parseSecretNames(value: string | undefined): string[] {
    const names = (value ?? '')
        .split(',')
        .map(name => name.trim())
        .filter(Boolean);
    if (names.length === 0) {
        throw new Error('K8S_ACCOUNT_SECRETS must contain at least one Secret name');
    }
    if (new Set(names).size !== names.length) {
        throw new Error('K8S_ACCOUNT_SECRETS must not contain duplicates');
    }
    for (const name of names) {
        if (!/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(name) || name.length > 253) {
            throw new Error(`Invalid Kubernetes Secret name: ${name}`);
        }
    }
    return names;
}

async function loadOrCreateAccount(
    namespace: string,
    secretName: string,
): Promise<BootstrapAccount> {
    let mnemonic = await readMnemonicSecret(namespace, secretName);
    if (!mnemonic) {
        mnemonic = ethers.Wallet.createRandom().mnemonic?.phrase ?? '';
        if (!mnemonic) throw new Error(`Failed to generate mnemonic for ${secretName}`);
        await applyKubernetesObject({
            apiVersion: 'v1',
            kind: 'Secret',
            metadata: {
                name: secretName,
                namespace,
                labels: { 'app.kubernetes.io/managed-by': 'sei-load-generator-bootstrap' },
            },
            type: 'Opaque',
            data: { mnemonic: Buffer.from(mnemonic).toString('base64') },
        });
        console.log(`Created Secret ${namespace}/${secretName}`);
    } else {
        console.log(`Reusing Secret ${namespace}/${secretName}`);
    }
    const wallet = await cosmosWalletAt(mnemonic, 0);
    return {
        secretName,
        mnemonic,
        address: (await wallet.getAccounts())[0].address,
    };
}

async function readMnemonicSecret(namespace: string, secretName: string): Promise<string | undefined> {
    try {
        const { stdout } = await execFile('kubectl', [
            '-n',
            namespace,
            'get',
            'secret',
            secretName,
            '-o',
            'json',
        ]);
        const secret = JSON.parse(stdout) as { data?: { mnemonic?: string } };
        const encoded = secret.data?.mnemonic;
        if (!encoded) throw new Error(`Secret ${namespace}/${secretName} has no mnemonic key`);
        return Buffer.from(encoded, 'base64').toString('utf8').trim();
    } catch (error) {
        const stderr = (error as { stderr?: string }).stderr ?? '';
        if (/not found/i.test(stderr)) return undefined;
        throw error;
    }
}

function ensureUniqueAccounts(accounts: BootstrapAccount[]): void {
    const addresses = new Set<string>();
    for (const account of accounts) {
        if (addresses.has(account.address)) {
            throw new Error(`Multiple Secrets resolve to account ${account.address}`);
        }
        addresses.add(account.address);
    }
}

async function applyKubernetesObject(object: unknown): Promise<void> {
    await new Promise<void>((resolve, reject) => {
        const child = spawn('kubectl', ['apply', '-f', '-'], {
            stdio: ['pipe', 'ignore', 'pipe'],
        });
        let stderr = '';
        child.stderr.setEncoding('utf8');
        child.stderr.on('data', chunk => {
            stderr += chunk;
        });
        child.on('error', reject);
        child.on('close', code => {
            if (code === 0) resolve();
            else reject(new Error(stderr.trim() || `kubectl apply exited with code ${code}`));
        });
        child.stdin.end(JSON.stringify(object));
    });
}

if (require.main === module) {
    bootstrapKubernetesMain().catch(error => {
        console.error('Fatal:', error instanceof Error ? error.message : error);
        process.exitCode = 1;
    });
}
