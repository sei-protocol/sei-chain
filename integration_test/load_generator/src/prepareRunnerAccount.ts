import fs from 'node:fs/promises';
import path from 'node:path';
import { coins } from '@cosmjs/amino';
import { SigningStargateClient } from '@cosmjs/stargate';
import { Encoder } from '@sei-js/cosmos/encoding';
import { ethers } from 'ethers';
import { queryEvmAssociation } from './association';
import { loadTargetConfig, seiToUsei, verifyTargetCosmosRpc } from './config';
import { sendFundingBatches } from './funding';
import { cosmosWalletAt, privateKeyAt, replayRegistry } from './keys';

const ASSOCIATION_BUFFER_USEI = 100_000n;

export async function prepareRunnerAccountMain(): Promise<void> {
    const target = loadTargetConfig();
    const mnemonicPathValue = (
        process.env.RUNNER_MNEMONIC_PATH ?? 'runtime/runner.mnemonic'
    ).trim();
    if (!mnemonicPathValue) throw new Error('RUNNER_MNEMONIC_PATH must not be empty');
    const mnemonicPath = path.resolve(mnemonicPathValue);
    const targetUsei = seiToUsei(
        process.env.RUNNER_ACCOUNT_FUND_SEI ?? '',
        'RUNNER_ACCOUNT_FUND_SEI',
    );
    if (targetUsei < ASSOCIATION_BUFFER_USEI) {
        throw new Error(
            `RUNNER_ACCOUNT_FUND_SEI must provide at least ${ASSOCIATION_BUFFER_USEI} usei`,
        );
    }
    if (process.env.EXECUTE !== '1') {
        console.log(
            `Dry-run: prepare a runner account at ${mnemonicPath} with target balance ` +
                `${targetUsei} usei. Set EXECUTE=1 to continue.`,
        );
        return;
    }
    if (!target.mnemonic) {
        throw new Error('TARGET_MNEMONIC or SEI_ADMIN_MNEMONIC is required as the treasury');
    }

    const runnerMnemonic = await loadOrCreateMnemonic(mnemonicPath);
    const treasuryWallet = await cosmosWalletAt(target.mnemonic, 0);
    const runnerWallet = await cosmosWalletAt(runnerMnemonic, 0);
    const treasuryAddress = (await treasuryWallet.getAccounts())[0].address;
    const runnerAddress = (await runnerWallet.getAccounts())[0].address;
    if (treasuryAddress === runnerAddress) {
        throw new Error('Runner account must not be the treasury account');
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
        const balance = BigInt((await client.getBalance(runnerAddress, 'usei')).amount);
        await sendFundingBatches(
            client,
            treasuryAddress,
            [
                {
                    address: runnerAddress,
                    amount: balance < targetUsei ? targetUsei - balance : 0n,
                },
            ],
            `fund ${target.network} load runner`,
        );
    } finally {
        client.disconnect();
    }

    await associateRunner(target.cosmosRpcUrl, target.network, runnerMnemonic, runnerAddress);
    console.log(`Runner account ready: ${runnerAddress}`);
    console.log(`Mnemonic file: ${mnemonicPath}`);
}

async function loadOrCreateMnemonic(mnemonicPath: string): Promise<string> {
    try {
        const existing = (await fs.readFile(mnemonicPath, 'utf8')).trim();
        if (!existing) throw new Error(`Mnemonic file is empty: ${mnemonicPath}`);
        return existing;
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
    }

    const mnemonic = ethers.Wallet.createRandom().mnemonic?.phrase;
    if (!mnemonic) throw new Error('Failed to generate runner mnemonic');
    await fs.mkdir(path.dirname(mnemonicPath), { recursive: true });
    const handle = await fs.open(mnemonicPath, 'wx', 0o600);
    try {
        await handle.writeFile(`${mnemonic}\n`, 'utf8');
    } finally {
        await handle.close();
    }
    console.log(`Created runner mnemonic file: ${mnemonicPath}`);
    return mnemonic;
}

async function associateRunner(
    cosmosRpcUrl: string,
    network: string,
    mnemonic: string,
    seiAddress: string,
): Promise<void> {
    const expectedEvmAddress = new ethers.Wallet(privateKeyAt(mnemonic, 0)).address;
    const mapping = await queryEvmAssociation(cosmosRpcUrl, seiAddress);
    if (mapping.associated) {
        if (mapping.evmAddress.toLowerCase() !== expectedEvmAddress.toLowerCase()) {
            throw new Error(
                `Runner is associated with ${mapping.evmAddress}, expected ${expectedEvmAddress}`,
            );
        }
        return;
    }

    const wallet = await cosmosWalletAt(mnemonic, 0);
    const client = await SigningStargateClient.connectWithSigner(cosmosRpcUrl, wallet, {
        registry: replayRegistry(),
        broadcastPollIntervalMs: 200,
    });
    try {
        const message = Encoder.evm.MsgAssociate.fromPartial({
            sender: seiAddress,
            custom_message: `${network} load runner`,
        });
        const result = await client.signAndBroadcast(
            seiAddress,
            [{ typeUrl: `/${Encoder.evm.MsgAssociate.$type}`, value: message }],
            { amount: coins('21000', 'usei'), gas: '200000' },
            `associate ${network} load runner`,
        );
        if (result.code !== 0) {
            throw new Error(`Runner association failed: ${result.rawLog}`);
        }
    } finally {
        client.disconnect();
    }
}

if (require.main === module) {
    prepareRunnerAccountMain().catch(error => {
        console.error('Fatal:', error instanceof Error ? error.message : error);
        process.exitCode = 1;
    });
}
