/**
 * Derive, associate, and fund replay users on Arctic-1 or Atlantic-2.
 *
 *   TARGET_NETWORK=<target> EXECUTE=1 npm run replay:users
 */
import fs from 'fs/promises';
import path from 'path';
import { coins } from '@cosmjs/amino';
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
import { sendFundingBatches } from './funding';

const provisionConfig = loadProvisionConfig();
const {
    userCount: USER_COUNT,
    fundSei: FUND_SEI,
    targetUsei: TARGET_USEI,
    execute: EXECUTE,
} = provisionConfig;
const ASSOCIATION_BUFFER_USEI = 100_000n;
const PROVISION_CONCURRENCY = 5;

interface LoadUser {
    index: number;
    derivationPath: string;
    seiAddress: string;
    evmAddress: string;
}

export async function provisionUsersMain(): Promise<void> {
    const target = loadTargetConfig();
    console.log(
        `Replay users: ${target.network}, ${USER_COUNT} users, target ${FUND_SEI} SEI each`,
    );
    console.log(`Manifest: ${target.usersPath}`);
    if (!target.mnemonic) {
        if (!EXECUTE) {
            console.log('Dry-run only. Set TARGET_MNEMONIC to print the funding account.');
            return;
        }
        throw new Error('TARGET_MNEMONIC or SEI_ADMIN_MNEMONIC is required for provisioning');
    }
    const adminWallet = await cosmosWalletAt(target.mnemonic, 0);
    const adminAddress = (await adminWallet.getAccounts())[0].address;
    const workerFundingUsei = TARGET_USEI * BigInt(USER_COUNT);
    console.log(`Funding account: ${adminAddress}`);
    console.log(
        `Worker funding target: ${formatUsei(workerFundingUsei)} SEI ` +
            `(${workerFundingUsei} usei), excluding fees`,
    );
    if (!EXECUTE) {
        console.log('Dry-run only. Set EXECUTE=1 to associate and fund users.');
        return;
    }

    const provider = new ethers.JsonRpcProvider(target.evmRpcUrl);
    try {
        await verifyTargetRpc(target, provider);
        const users = await deriveUsers(target.mnemonic);
        const admin = await SigningStargateClient.connectWithSigner(
            target.cosmosRpcUrl,
            adminWallet,
            {
                registry: replayRegistry(),
                broadcastPollIntervalMs: 200,
            },
        );
        try {
            await verifyTargetCosmosRpc(target, admin);

            const associations = await mapConcurrent(users, PROVISION_CONCURRENCY, async user => {
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
            const initialBalances = await readBalances(admin, users);
            await sendFundingBatches(
                admin,
                adminAddress,
                users.map((user, index) => ({
                    address: user.seiAddress,
                    amount:
                        associations[index].associated ||
                        initialBalances[index] >= ASSOCIATION_BUFFER_USEI
                            ? 0n
                            : ASSOCIATION_BUFFER_USEI - initialBalances[index],
                })),
                `fund ${target.network} replay association buffer`,
            );

            let associated = 0;
            let newAssociations = 0;
            await mapConcurrent(users, PROVISION_CONCURRENCY, async user => {
                const mapping = associationByAddress.get(user.seiAddress)!;
                if (mapping.associated) {
                    associated++;
                    return;
                }
                const wallet = await cosmosWalletAt(target.mnemonic, user.index);
                const client = await SigningStargateClient.connectWithSigner(
                    target.cosmosRpcUrl,
                    wallet,
                    {
                        registry: replayRegistry(),
                        broadcastPollIntervalMs: 200,
                    },
                );
                const message = Encoder.evm.MsgAssociate.fromPartial({
                    sender: user.seiAddress,
                    custom_message: `${target.network} Pacific replay`,
                });
                try {
                    const result = await client.signAndBroadcast(
                        user.seiAddress,
                        [{ typeUrl: `/${Encoder.evm.MsgAssociate.$type}`, value: message }],
                        { amount: coins('21000', 'usei'), gas: '200000' },
                        `associate ${target.network} replay user`,
                    );
                    if (result.code !== 0 && !/already|associated/i.test(result.rawLog ?? '')) {
                        throw new Error(
                            `Association failed for user ${user.index}: ${result.rawLog}`,
                        );
                    }
                } finally {
                    client.disconnect();
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

            const balancesAfterAssociation = await readBalances(admin, users);
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

            const finalBalances = await readBalances(admin, users);
            const manifest = {
                schemaVersion: 1,
                network: target.network,
                chainId: Number(target.evmChainId),
                generatedAt: new Date().toISOString(),
                targetBalanceSei: FUND_SEI,
                targetBalanceUsei: TARGET_USEI.toString(),
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
            console.log(`Saved ${users.length} users to ${target.usersPath}`);
        } finally {
            admin.disconnect();
        }
    } finally {
        provider.destroy();
    }
}

async function readBalances(
    client: SigningStargateClient,
    users: LoadUser[],
): Promise<bigint[]> {
    return mapConcurrent(users, PROVISION_CONCURRENCY, async user =>
        BigInt((await client.getBalance(user.seiAddress, 'usei')).amount),
    );
}

function formatUsei(usei: bigint): string {
    const whole = usei / 1_000_000n;
    const fraction = (usei % 1_000_000n).toString().padStart(6, '0').replace(/0+$/, '');
    return fraction ? `${whole}.${fraction}` : whole.toString();
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

if (require.main === module) {
    provisionUsersMain().catch(error => {
        console.error('Fatal:', error instanceof Error ? error.message : error);
        process.exitCode = 1;
    });
}
