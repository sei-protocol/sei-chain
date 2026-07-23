import util from 'node:util';
import { ethers } from 'ethers';
import {
    QueryClient,
    setupBankExtension,
    setupStakingExtension,
    setupGovExtension,
    setupDistributionExtension,
    BankExtension,
    StakingExtension,
    GovExtension,
    DistributionExtension,
    SigningStargateClient,
    defaultRegistryTypes,
    assertIsDeliverTxSuccess,
} from '@cosmjs/stargate';
import { Tendermint34Client } from '@cosmjs/tendermint-rpc';
import { DirectSecp256k1HdWallet, Registry } from '@cosmjs/proto-signing';
import { stringToPath } from '@cosmjs/crypto';
import { coins } from '@cosmjs/amino';
import { seiProtoRegistry, Encoder } from '@sei-js/cosmos/encoding';
import { Endpoints } from '../config/endpoints';
import { waitUntil } from './chainUtils';
import { SEI_HD_PATH, DOCKER_NODE, DOCKER_KEY_PASSWORD, SEID_ENV } from './constants';

const exec = util.promisify(require('node:child_process').exec);

// 10^12 usei == 10^6 SEI. Matches rpc_tests' fundAdminOnSei.
const DEFAULT_FUND_USEI = '1000000000000';

/** A `sei`-prefixed HD wallet derived from `mnemonic` at the shared coin-type-118 path. */
function seiWalletFromMnemonic(mnemonic: string): Promise<DirectSecp256k1HdWallet> {
    return DirectSecp256k1HdWallet.fromMnemonic(mnemonic, {
        prefix: 'sei',
        hdPaths: [stringToPath(SEI_HD_PATH)],
    });
}

export type CosmosQueryClient = QueryClient &
    BankExtension &
    StakingExtension &
    GovExtension &
    DistributionExtension;

let clientPromise: Promise<CosmosQueryClient> | undefined;

/**
 * The suite's Cosmos-side parity oracle: one shared query client over the
 * chain's own modules (bank, staking, gov, distribution). Precompile-reported
 * values and EVM-side effects are asserted against these reads.
 */
export async function cosmosQuery(): Promise<CosmosQueryClient> {
    if (!clientPromise) {
        clientPromise = (async () => {
            const tm = await Tendermint34Client.connect(Endpoints.sei.cosmosRpc);
            return QueryClient.withExtensions(
                tm,
                setupBankExtension,
                setupStakingExtension,
                setupGovExtension,
                setupDistributionExtension,
            );
        })();
    }
    return clientPromise;
}

const bankClient = cosmosQuery;

/** Operator addresses (seivaloper1…) of the currently bonded validators. */
export async function bondedValidators(): Promise<string[]> {
    const qc = await cosmosQuery();
    const { validators } = await qc.staking.validators('BOND_STATUS_BONDED');
    return validators.map(v => v.operatorAddress);
}

/** A fresh random BIP39 mnemonic (coin type 118) — used to mint an ephemeral admin. */
export async function generateMnemonic(length: 12 | 24 = 24): Promise<string> {
    const wallet = await DirectSecp256k1HdWallet.generate(length, {
        prefix: 'sei',
        hdPaths: [stringToPath(SEI_HD_PATH)],
    });
    return wallet.mnemonic;
}

/** A fresh, never-funded `sei1…` bech32 address (with its backing mnemonic discarded). */
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
 * The `denom` bank balance of a `sei1…` address, straight from the chain's own bank
 * module. This is the suite's parity oracle: precompile-reported values and EVM-side
 * effects are asserted against these Cosmos-side reads.
 */
export async function bankBalance(seiAddress: string, denom = 'usei'): Promise<bigint> {
    const qc = await bankClient();
    const coin = await qc.bank.balance(seiAddress, denom);
    return BigInt(coin.amount);
}

/** Total on-chain supply of `denom`, from the bank module. */
export async function bankSupplyOf(denom = 'usei'): Promise<bigint> {
    const qc = await bankClient();
    const coin = await qc.bank.supplyOf(denom);
    return BigInt(coin.amount);
}

/**
 * Create a tokenfactory denom from `mnemonic` and wait until its bank metadata
 * is visible. x/tokenfactory sets denom metadata automatically on creation
 * (name/symbol = the full `factory/<creator>/<subdenom>` string, exponent 0),
 * which is exactly the precondition the pointer precompile's addNativePointer
 * checks. Returns the full denom.
 */
export async function createTokenfactoryDenom(
    mnemonic: string,
    subdenom: string,
): Promise<string> {
    const wallet = await seiWalletFromMnemonic(mnemonic);
    const [account] = await wallet.getAccounts();
    const registry = new Registry([...seiProtoRegistry, ...defaultRegistryTypes]);
    const client = await SigningStargateClient.connectWithSigner(Endpoints.sei.cosmosRpc, wallet, {
        registry,
    });
    try {
        const msg = {
            typeUrl: `/${Encoder.tokenfactory.MsgCreateDenom.$type}`,
            value: Encoder.tokenfactory.MsgCreateDenom.fromPartial({
                sender: account.address,
                subdenom,
            }),
        };
        const fee = { amount: coins(40000, 'usei'), gas: '400000' };
        const res = await client.signAndBroadcast(account.address, [msg], fee, 'create denom');
        assertIsDeliverTxSuccess(res);
    } finally {
        client.disconnect();
    }

    const denom = `factory/${account.address}/${subdenom}`;
    const qc = await cosmosQuery();
    await waitUntil(async () => (await qc.bank.denomMetadata(denom)) ?? null, {
        timeoutMs: 30_000,
        label: `bank metadata for ${denom}`,
    });
    return denom;
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
        `docker exec ${DOCKER_NODE} /bin/bash -c '${SEID_ENV} && printf "${DOCKER_KEY_PASSWORD}\\n" | seid tx bank send ${containerAdmin} ${toSeiAddress} ${amountUsei}usei --fees 24500usei -b sync -y'`,
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
            custom_message: 'precompile_tests bootstrap',
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
 * Give the admin a spendable EVM balance on a local Sei docker devnet. Funding alone is
 * not enough — Sei only exposes an EVM balance once the account is associated — so
 * bank-send usei to the admin's cosmos address from the in-container `admin` key, then
 * broadcast MsgAssociate. Idempotent (returns early when the admin already has an EVM
 * balance). Throws when no local docker devnet is running (point at a pre-funded admin
 * via SEI_ADMIN_MNEMONIC instead).
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
    await waitUntil(async () => ((await bankBalance(seiAddress)) > 0n ? true : null), {
        timeoutMs: 30_000,
        label: 'admin sei bank balance',
    });
    await associate(mnemonic, seiAddress);
    await waitUntil(
        async () => ((await provider.getBalance(adminEvmAddress)) > 0n ? true : null),
        { timeoutMs: 30_000, label: 'admin evm balance after association' },
    );
}
