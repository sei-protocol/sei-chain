/**
 * Derive, associate, and fund replay users on Arctic-1 or Atlantic-2.
 *
 *   TARGET_NETWORK=arctic-1 EXECUTE=1 npm run replay:users
 *   TARGET_NETWORK=atlantic-2 EXECUTE=1 npm run replay:users
 */
import fs from 'fs/promises';
import path from 'path';
import { coins } from '@cosmjs/amino';
import { EncodeObject } from '@cosmjs/proto-signing';
import { SigningStargateClient } from '@cosmjs/stargate';
import { Encoder } from '@sei-js/cosmos/encoding';
import { ethers } from 'ethers';
import {
    loadProvisionConfig,
    loadTargetConfig,
    verifyTargetCosmosRpc,
    verifyTargetRpc,
} from './config';
import { writeJsonAtomic } from './io';
import { mapConcurrent } from './concurrency';
import { cosmosWalletAt, privateKeyAt, replayRegistry } from './keys';
import { queryEvmAssociation } from './association';

const provisionConfig = loadProvisionConfig();
const {
    userCount: USER_COUNT,
    fundSei: FUND_SEI,
    targetUsei: TARGET_USEI,
    execute: EXECUTE,
} = provisionConfig;
const ASSOCIATION_BUFFER_USEI = 100_000n;

interface LoadUser {
    index: number;
    derivationPath: string;
    seiAddress: string;
    evmAddress: string;
}

async function main(): Promise<void> {
    const target = loadTargetConfig();
    console.log(
        `Replay users: ${target.network}, ${USER_COUNT} users, target ${FUND_SEI} SEI each`,
    );
    console.log(`Manifest: ${target.usersPath}`);
    if (!EXECUTE) {
        console.log('Dry-run only. Set EXECUTE=1 to associate and fund users.');
        return;
    }
    if (!target.mnemonic) {
        throw new Error('TARGET_MNEMONIC or SEI_ADMIN_MNEMONIC is required for provisioning');
    }

    const provider = new ethers.JsonRpcProvider(target.evmRpcUrl);
    await verifyTargetRpc(target, provider);
    const users = await deriveUsers(target.mnemonic);
    const adminWallet = await cosmosWalletAt(target.mnemonic, 0);
    const adminAddress = (await adminWallet.getAccounts())[0].address;
    const admin = await SigningStargateClient.connectWithSigner(target.cosmosRpcUrl, adminWallet, {
        registry: replayRegistry(),
        broadcastPollIntervalMs: 200,
    });
    try {
        await verifyTargetCosmosRpc(target, admin);
    } catch (error) {
        admin.disconnect();
        throw error;
    }

    const associations = await mapConcurrent(users, 5, async user => {
        const mapping = await queryEvmAssociation(target.cosmosRpcUrl, user.seiAddress);
        if (
            mapping.associated &&
            mapping.evmAddress.toLowerCase() !== user.evmAddress.toLowerCase()
        ) {
            throw new Error(
                `User ${user.index} is associated with ${mapping.evmAddress}, ` +
                    `expected ${user.evmAddress}`,
            );
        }
        return mapping;
    });
    const associationByAddress = new Map(
        users.map((user, index) => [user.seiAddress, associations[index]]),
    );
    const initialBalances = await Promise.all(
        users.map(async user => BigInt((await admin.getBalance(user.seiAddress, 'usei')).amount)),
    );
    await sendFundingBatches(
        admin,
        adminAddress,
        users.map((user, index) => ({
            address: user.seiAddress,
            amount:
                associations[index].associated || initialBalances[index] >= ASSOCIATION_BUFFER_USEI
                    ? 0n
                    : ASSOCIATION_BUFFER_USEI - initialBalances[index],
        })),
        `fund ${target.network} replay association buffer`,
    );

    let associated = 0;
    let newAssociations = 0;
    await mapConcurrent(users, 5, async user => {
        const mapping = associationByAddress.get(user.seiAddress)!;
        if (mapping.associated) {
            associated++;
            return;
        }
        const wallet = await cosmosWalletAt(target.mnemonic, user.index);
        const client = await SigningStargateClient.connectWithSigner(target.cosmosRpcUrl, wallet, {
            registry: replayRegistry(),
            broadcastPollIntervalMs: 200,
        });
        const message = Encoder.evm.MsgAssociate.fromPartial({
            sender: user.seiAddress,
            custom_message: `${target.network} Pacific replay`,
        });
        const result = await (async () => {
            try {
                return await client.signAndBroadcast(
                    user.seiAddress,
                    [{ typeUrl: `/${Encoder.evm.MsgAssociate.$type}`, value: message }],
                    { amount: coins('21000', 'usei'), gas: '200000' },
                    `associate ${target.network} replay user`,
                );
            } finally {
                client.disconnect();
            }
        })();
        if (result.code !== 0 && !/already|associated/i.test(result.rawLog ?? '')) {
            throw new Error(`Association failed for user ${user.index}: ${result.rawLog}`);
        }
        associated++;
        newAssociations++;
        if (associated % 10 === 0 || associated === users.length) {
            console.log(`  associated ${associated}/${users.length}`);
        }
    });
    console.log(
        `Association check complete: ${associated}/${users.length} valid, ` +
            `${newAssociations} newly associated`,
    );

    const balancesAfterAssociation = await Promise.all(
        users.map(async user => BigInt((await admin.getBalance(user.seiAddress, 'usei')).amount)),
    );
    await sendFundingBatches(
        admin,
        adminAddress,
        users.map((user, index) => ({
            address: user.seiAddress,
            amount:
                balancesAfterAssociation[index] >= TARGET_USEI
                    ? 0n
                    : TARGET_USEI - balancesAfterAssociation[index],
        })),
        `fund ${target.network} replay users`,
    );

    const finalBalances = await Promise.all(
        users.map(async user => BigInt((await admin.getBalance(user.seiAddress, 'usei')).amount)),
    );
    const manifest = {
        schemaVersion: 1,
        network: target.network,
        chainId: Number(target.evmChainId),
        generatedAt: new Date().toISOString(),
        targetBalanceSei: FUND_SEI,
        derivation: {
            source: 'TARGET_MNEMONIC or SEI_ADMIN_MNEMONIC',
            privateKeysPersisted: false,
        },
        users: users.map((user, index) => ({
            ...user,
            balanceUsei: finalBalances[index].toString(),
        })),
    };
    await fs.mkdir(path.dirname(target.usersPath), { recursive: true });
    await writeJsonAtomic(target.usersPath, manifest);
    admin.disconnect();
    console.log(`Saved ${users.length} users to ${target.usersPath}`);
}

async function deriveUsers(mnemonic: string): Promise<LoadUser[]> {
    return Promise.all(
        Array.from({ length: USER_COUNT }, async (_, offset) => {
            const index = offset + 1;
            const derivationPath = `m/44'/118'/0'/0/${index}`;
            const privateKey = privateKeyAt(mnemonic, index);
            const evmAddress = new ethers.Wallet(privateKey).address;
            const cosmosWallet = await cosmosWalletAt(mnemonic, index);
            const seiAddress = (await cosmosWallet.getAccounts())[0].address;
            return { index, derivationPath, seiAddress, evmAddress };
        }),
    );
}

async function sendFundingBatches(
    client: SigningStargateClient,
    sender: string,
    amounts: Array<{ address: string; amount: bigint }>,
    memo: string,
): Promise<void> {
    const nonZero = amounts.filter(item => item.amount > 0n);
    for (let start = 0; start < nonZero.length; start += 10) {
        const batch = nonZero.slice(start, start + 10);
        const messages: EncodeObject[] = batch.map(item => ({
            typeUrl: '/cosmos.bank.v1beta1.MsgSend',
            value: {
                fromAddress: sender,
                toAddress: item.address,
                amount: coins(item.amount.toString(), 'usei'),
            },
        }));
        const result = await client.signAndBroadcast(
            sender,
            messages,
            {
                amount: coins(String(30_000 + batch.length * 5_000), 'usei'),
                gas: String(200_000 + batch.length * 100_000),
            },
            memo,
        );
        if (result.code !== 0) throw new Error(`Funding batch failed: ${result.rawLog}`);
        console.log(`  funded batch ${start / 10 + 1}/${Math.ceil(nonZero.length / 10)}`);
    }
}

main().catch(error => {
    console.error('Fatal:', error instanceof Error ? error.message : error);
    process.exit(1);
});
