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
    usersPerPartition: USERS_PER_PARTITION,
    activePerPartition: ACTIVE_PER_PARTITION,
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

/**
 * A pod reads users [partition * USERS_PER_PARTITION, + WORKER_COUNT), so only the first
 * ACTIVE_PER_PARTITION indexes of each stride are ever spent from. The rest exist to keep
 * derivation indexes aligned as replicas scale, and are derived into the manifest unfunded.
 */
function isActive(index: number): boolean {
    return (index - 1) % USERS_PER_PARTITION < ACTIVE_PER_PARTITION;
}

export async function provisionUsersMain(): Promise<void> {
    const target = loadTargetConfig();
    const activeCount = countActive(USER_COUNT);
    console.log(
        `Replay users: ${target.network}, ${USER_COUNT} users, ` +
            `${activeCount} funded to ${FUND_SEI} SEI each`,
    );
    if (activeCount < USER_COUNT) {
        console.log(
            `Reserved: ${USER_COUNT - activeCount} users left unfunded, holding derivation ` +
                `indexes past the first ${ACTIVE_PER_PARTITION} of every ` +
                `${USERS_PER_PARTITION}. Raise ACTIVE_PER_PARTITION and re-run to fund more.`,
        );
    }
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
    const workerFundingUsei = TARGET_USEI * BigInt(activeCount);
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
        const pool = await deriveUsers(target.mnemonic);
        // Everything below this line acts on the funded set only. Reserved users cost a
        // derivation each and are rejoined when the manifest is written.
        const users = pool.filter(user => isActive(user.index));
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
            const balanceByIndex = new Map(
                users.map((user, index) => [user.index, finalBalances[index].toString()]),
            );
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
                usersPerPartition: USERS_PER_PARTITION,
                activePerPartition: ACTIVE_PER_PARTITION,
                // A reserved user is written without balanceUsei rather than with a zero.
                // The runner refuses to start on an entry that has no balance, so widening
                // WORKER_COUNT past the funded width fails at boot instead of part way
                // through a run once the empty accounts start rejecting.
                users: pool.map(user => {
                    const balanceUsei = balanceByIndex.get(user.index);
                    return balanceUsei ? { ...user, balanceUsei } : { ...user };
                }),
            };
            await fs.mkdir(path.dirname(target.usersPath), { recursive: true });
            await writeJsonAtomic(target.usersPath, manifest);
            console.log(
                `Saved ${pool.length} users (${users.length} funded) to ${target.usersPath}`,
            );
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

function countActive(userCount: number): number {
    let count = 0;
    for (let index = 1; index <= userCount; index++) {
        if (isActive(index)) count++;
    }
    return count;
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
