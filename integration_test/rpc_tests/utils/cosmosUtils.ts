import util from 'node:util';
import { createHash } from 'node:crypto';
import { ethers } from 'ethers';
import {
    QueryClient,
    setupBankExtension,
    BankExtension,
    SigningStargateClient,
    defaultRegistryTypes,
} from '@cosmjs/stargate';
import { Tendermint34Client } from '@cosmjs/tendermint-rpc';
import { DirectSecp256k1HdWallet, Registry } from '@cosmjs/proto-signing';
import { stringToPath } from '@cosmjs/crypto';
import { toBech32 } from '@cosmjs/encoding';
import { coins } from '@cosmjs/amino';
import { QueryBalanceRequest, QueryBalanceResponse } from 'cosmjs-types/cosmos/bank/v1beta1/query';
import { seiProtoRegistry, Encoder } from '@sei-js/cosmos/encoding';
import { Endpoints } from '../config/endpoints';
import { waitUntil } from './chainUtils';
import { SEI_HD_PATH } from './constants';

const exec = util.promisify(require('node:child_process').exec);

const DOCKER_NODE = 'sei-node-0';
const DOCKER_KEY_PASSWORD = '12345678';
// 10^12 usei == 10^6 SEI. Matches shared/Funder.fundAdminOnSei.
const DEFAULT_FUND_USEI = '1000000000000';
const SEID_ENV = 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin';

/** A `sei`-prefixed HD wallet derived from `mnemonic` at the shared coin-type-118 path. */
function seiWalletFromMnemonic(mnemonic: string): Promise<DirectSecp256k1HdWallet> {
    return DirectSecp256k1HdWallet.fromMnemonic(mnemonic, {
        prefix: 'sei',
        hdPaths: [stringToPath(SEI_HD_PATH)],
    });
}

// ---------------------------------------------------------------------------
// Bank queries + native MsgSend.
// ---------------------------------------------------------------------------

let clientPromise: Promise<QueryClient & BankExtension> | undefined;

async function bankClient(): Promise<QueryClient & BankExtension> {
    if (!clientPromise) {
        clientPromise = (async () => {
            const tm = await Tendermint34Client.connect(Endpoints.sei.cosmosRpc);
            return QueryClient.withExtensions(tm, setupBankExtension);
        })();
    }
    return clientPromise;
}

export interface CosmosBankSend {
    /** Cosmos tx hash (uppercase hex, no 0x prefix — as Tendermint reports it). */
    hash: string;
    /** Block height the bank send was included in. */
    height: number;
    /** DeliverTx result code (0 == success). */
    code: number;
    from: string;
    to: string;
    amountUsei: bigint;
    gasUsed: bigint;
}

/** A fresh, never-funded `sei1…` bech32 address (with its backing mnemonic). */
export async function generateSeiAddress(): Promise<string> {
    const wallet = await DirectSecp256k1HdWallet.generate(12, {
        prefix: 'sei',
        hdPaths: [stringToPath(SEI_HD_PATH)],
    });
    const [account] = await wallet.getAccounts();
    return account.address;
}

/** bech32 `sei1…` address for a mnemonic (cosmos coin type 118). */
export async function seiAddressFromMnemonic(mnemonic: string): Promise<string> {
    const wallet = await seiWalletFromMnemonic(mnemonic);
    const [account] = await wallet.getAccounts();
    return account.address;
}

/**
 * Sign and broadcast a native Cosmos `bank` MsgSend (usei) and wait for inclusion.
 * Returns the block height it landed in so callers can line it up against the EVM
 * block at the same height. This is a pure Cosmos transaction — it must NOT surface
 * through any EVM JSON-RPC (eth_getBlockReceipts, eth_getBlockByNumber, …).
 */
export async function cosmosBankSend(
    mnemonic: string,
    toSeiAddress: string,
    amountUsei: bigint,
): Promise<CosmosBankSend> {
    const wallet = await seiWalletFromMnemonic(mnemonic);
    const [account] = await wallet.getAccounts();
    const client = await SigningStargateClient.connectWithSigner(Endpoints.sei.cosmosRpc, wallet);
    try {
        const fee = { amount: coins(24500, 'usei'), gas: '200000' };
        const res = await client.sendTokens(
            account.address,
            toSeiAddress,
            coins(amountUsei.toString(), 'usei'),
            fee,
            'rpc_tests dual-vm block',
        );
        return {
            hash: res.transactionHash,
            height: Number(res.height),
            code: res.code,
            from: account.address,
            to: toSeiAddress,
            amountUsei,
            gasUsed: BigInt(res.gasUsed),
        };
    } finally {
        client.disconnect();
    }
}

export async function bankBalanceUsei(seiAddress: string, height?: number): Promise<bigint> {
    const qc = await bankClient();
    if (height === undefined) {
        const coin = await qc.bank.balance(seiAddress, 'usei');
        return BigInt(coin.amount);
    }
    const request = QueryBalanceRequest.encode({ address: seiAddress, denom: 'usei' }).finish();
    const { value } = await qc.queryAbci('/cosmos.bank.v1beta1.Query/Balance', request, height);
    const { balance } = QueryBalanceResponse.decode(value);
    return balance ? BigInt(balance.amount) : 0n;
}

// ---------------------------------------------------------------------------
// Admin funding / association on a local Sei docker devnet.
// ---------------------------------------------------------------------------

/**
 * The cosmos module account address for the `fee_collector` (where EVM tx fees accrue),
 * derived the Cosmos SDK way: bech32 of the first 20 bytes of sha256("fee_collector").
 */
export function feeCollectorCosmosAddress(seiPrefix: string): string {
    const hash = createHash('sha256').update('fee_collector').digest();
    return toBech32(seiPrefix, hash.subarray(0, 20));
}

/** True when a local `sei-node-0` docker container is running. */
export async function isSeiDocker(): Promise<boolean> {
    try {
        const { stdout } = await exec(
            `docker ps --filter 'name=${DOCKER_NODE}' --format '{{.Names}}'`,
        );
        return stdout.includes(DOCKER_NODE);
    } catch {
        return false;
    }
}

async function bankSendFromContainerAdmin(toSeiAddress: string, amountUsei: string): Promise<void> {
    const { stdout } = await exec(
        `docker exec ${DOCKER_NODE} /bin/bash -c '${SEID_ENV} && printf "${DOCKER_KEY_PASSWORD}\\n" | seid keys show admin -a'`,
    );
    const containerAdmin = stdout.trimEnd();
    await exec(
        `docker exec ${DOCKER_NODE} /bin/bash -c '${SEID_ENV} && printf "${DOCKER_KEY_PASSWORD}\\n" | seid tx bank send ${containerAdmin} ${toSeiAddress} ${amountUsei}usei --fees 24500usei -y'`,
    );
}

/**
 * Broadcast MsgAssociate so the account's pubkey lands on-chain. Until an account is
 * associated, Sei cannot map its cosmos balance to its EVM address and
 * eth_getBalance returns 0. Tolerates an already-associated account.
 */
async function associate(mnemonic: string, seiAddress: string): Promise<void> {
    const wallet = await seiWalletFromMnemonic(mnemonic);
    const registry = new Registry([...seiProtoRegistry, ...defaultRegistryTypes]);
    const client = await SigningStargateClient.connectWithSigner(Endpoints.sei.cosmosRpc, wallet, {
        registry,
    });
    const msg = {
        typeUrl: `/${Encoder.evm.MsgAssociate.$type}`,
        value: Encoder.evm.MsgAssociate.fromPartial({
            sender: seiAddress,
            custom_message: 'new_rpc_tests bootstrap',
        }),
    };
    const fee = { amount: coins(21000, 'usei'), gas: '200000' };
    try {
        await client.signAndBroadcast(seiAddress, [msg], fee, 'associate');
    } catch (e: any) {
        // An already-associated account rejects a second association; that's fine —
        // the final balance check below is the real gate.
        if (!/already|associated/i.test(e?.message ?? '')) throw e;
    } finally {
        client.disconnect();
    }
}

/**
 * Mirror of UserFactory.fundAdminOnSei: give the admin a spendable EVM balance on a
 * local Sei docker devnet. Funding alone is not enough — Sei only exposes an EVM
 * balance once the account is associated — so we bank-send usei to the admin's
 * cosmos address from the in-container `admin` key, then broadcast MsgAssociate.
 *
 * Idempotent: returns early when the admin already has an EVM balance. Throws when
 * no local docker devnet is running, since the admin then cannot be funded
 * automatically (point at a pre-funded admin via SEI_ADMIN_MNEMONIC instead).
 */
export async function fundAdminOnSei(
    adminEvmAddress: string,
    mnemonic: string,
    provider: ethers.JsonRpcProvider,
    amountUsei = DEFAULT_FUND_USEI,
): Promise<void> {
    if ((await provider.getBalance(adminEvmAddress)) > 0n) return;

    if (!(await isSeiDocker())) {
        throw new Error(
            `fundAdminOnSei: admin ${adminEvmAddress} has no EVM balance and no local ` +
                `${DOCKER_NODE} container is running to fund it. Start the cluster ` +
                '(cd sei-chain && make docker-cluster-start) or set SEI_ADMIN_MNEMONIC to a ' +
                'pre-funded account.',
        );
    }

    const seiAddress = await seiAddressFromMnemonic(mnemonic);
    await bankSendFromContainerAdmin(seiAddress, amountUsei);
    // Wait for the bank send to land so association has gas to spend.
    await waitUntil(async () => ((await bankBalanceUsei(seiAddress)) > 0n ? true : null), {
        timeoutMs: 30_000,
        label: 'admin sei bank balance',
    });
    await associate(mnemonic, seiAddress);
    await waitUntil(
        async () => ((await provider.getBalance(adminEvmAddress)) > 0n ? true : null),
        { timeoutMs: 30_000, label: 'admin evm balance after association' },
    );
}
