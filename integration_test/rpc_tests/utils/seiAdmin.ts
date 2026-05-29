import util from 'node:util';
import { ethers } from 'ethers';
import { DirectSecp256k1HdWallet, Registry } from '@cosmjs/proto-signing';
import { stringToPath } from '@cosmjs/crypto';
import { SigningStargateClient, defaultRegistryTypes } from '@cosmjs/stargate';
import { coins } from '@cosmjs/amino';
import { seiProtoRegistry, Encoder } from '@sei-js/cosmos/encoding';
import { Endpoints } from '../config/endpoints';
import { waitUntil } from './waitFor';
import { bankBalanceUsei } from './cosmos';

const exec = util.promisify(require('node:child_process').exec);

// Sei keys use cosmos coin type 118; the EVM key derives from the same path, so a
// single mnemonic yields a matching (sei, 0x) address pair.
const SEI_HD_PATH = "m/44'/118'/0'/0/0";
const DOCKER_NODE = 'sei-node-0';
const DOCKER_KEY_PASSWORD = '12345678';
// 10^12 usei == 10^6 SEI. Matches shared/Funder.fundAdminOnSei.
const DEFAULT_FUND_USEI = '1000000000000';
const SEID_ENV = 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin';

/** bech32 `sei1…` address for a mnemonic (cosmos coin type 118). */
export async function seiAddressFromMnemonic(mnemonic: string): Promise<string> {
    const wallet = await DirectSecp256k1HdWallet.fromMnemonic(mnemonic, {
        prefix: 'sei',
        hdPaths: [stringToPath(SEI_HD_PATH)],
    });
    const [account] = await wallet.getAccounts();
    return account.address;
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
    const wallet = await DirectSecp256k1HdWallet.fromMnemonic(mnemonic, {
        prefix: 'sei',
        hdPaths: [stringToPath(SEI_HD_PATH)],
    });
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
