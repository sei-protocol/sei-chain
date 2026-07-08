import util from 'node:util';
import fs from 'node:fs';
import { createHash } from 'node:crypto';
import { DirectSecp256k1HdWallet } from '@cosmjs/proto-signing';
import { MsgExecuteContractEncodeObject, SigningCosmWasmClient } from '@cosmjs/cosmwasm-stargate';
import { stringToPath } from '@cosmjs/crypto';
import { toUtf8 } from '@cosmjs/encoding';
import { TxRaw } from 'cosmjs-types/cosmos/tx/v1beta1/tx';
import { Endpoints } from '../config/endpoints';
import { waitUntil } from './chainUtils';
import { isInProcess } from './cosmosUtils';
import { seidNodeExec } from './nodeExec';
import {
    SEI_HD_PATH,
    DOCKER_NODE,
    SEID_ENV,
    DOCKER_KEY_PASSWORD,
    DOCKER_EVM_RPC,
    CW20_WASM_PATH,
    CW20_WASM_SHA256,
    WASM_FEE,
    EXEC_FEE,
} from './constants';

const exec = util.promisify(require('node:child_process').exec);

export interface Cw20InitMsg {
    name: string;
    symbol: string;
    decimals: number;
    initial_balances: { address: string; amount: string }[];
    mint?: { minter: string };
}

/**
 * True when the chain allows everybody to store and instantiate wasm. Mirrors
 * lib.js isWasmEnabled(): both `uploadAccess` and `instantiateAccess` must be
 * `Everybody`. Returns false (rather than throwing) on any query error so callers
 * can simply skip the dual-VM path on a wasm-disabled chain.
 */
export async function isWasmEnabled(): Promise<boolean> {
    try {
        const query = (key: string) => seidNodeExec(`query params subspace wasm ${key} -o json`);
        const [upload, instantiate] = await Promise.all([
            query('uploadAccess'),
            query('instantiateAccess'),
        ]);
        return (
            JSON.parse(upload.stdout).value.includes('Everybody') &&
            JSON.parse(instantiate.stdout).value.includes('Everybody')
        );
    } catch {
        return false;
    }
}

let adminClient: Promise<{ client: SigningCosmWasmClient; address: string }> | undefined;

/** Cached CosmWasm signing client for the admin mnemonic (coin type 118). */
function adminCosmWasm(mnemonic: string): Promise<{ client: SigningCosmWasmClient; address: string }> {
    if (!adminClient) {
        adminClient = (async () => {
            const wallet = await DirectSecp256k1HdWallet.fromMnemonic(mnemonic, {
                prefix: 'sei',
                hdPaths: [stringToPath(SEI_HD_PATH)],
            });
            const [account] = await wallet.getAccounts();
            const client = await SigningCosmWasmClient.connectWithSigner(
                Endpoints.sei.cosmosRpc,
                wallet,
            );
            return { client, address: account.address };
        })();
    }
    return adminClient;
}

function readVerifiedCw20Wasm(): Buffer {
    const wasm = fs.readFileSync(CW20_WASM_PATH);
    const digest = createHash('sha256').update(wasm).digest('hex');
    if (digest !== CW20_WASM_SHA256) {
        throw new Error(
            `cw20_base.wasm checksum mismatch: expected ${CW20_WASM_SHA256}, got ${digest}. ` +
                `Re-copy from contracts/wasm/cw20_base.wasm and update CW20_WASM_SHA256 in the same commit.`,
        );
    }
    return wasm;
}

/**
 * Store cw20_base and instantiate it with `initMsg`, signed client-side by the admin.
 * Returns the instantiated CW20 `sei1…` contract address (and its code id).
 */
export async function deployCw20(
    initMsg: Cw20InitMsg,
    adminMnemonic: string,
    label = 'rpc_tests cw20',
): Promise<{ codeId: number; address: string }> {
    const { client, address } = await adminCosmWasm(adminMnemonic);
    const wasm = readVerifiedCw20Wasm();
    const uploaded = await client.upload(address, wasm, WASM_FEE);
    const instantiated = await client.instantiate(
        address,
        uploaded.codeId,
        initMsg,
        label,
        WASM_FEE,
        { admin: address },
    );
    return { codeId: uploaded.codeId, address: instantiated.contractAddress };
}

/** Query the CW20's ERC20 pointer; returns '' until it is registered on-chain. */
async function queryCw20Pointer(cw20Address: string): Promise<string> {
    const { stdout } = await seidNodeExec(`query evm pointer CW20 ${cw20Address} -o json`);
    const res = JSON.parse(stdout);
    return res.exists && res.pointer ? res.pointer : '';
}

/**
 * Register an ERC20 pointer for a CW20 via the `admin` key and return the pointer's `0x…`
 * EVM address. `register-evm-pointer` broadcasts in sync mode (modern seid no longer blocks
 * until commit), so we poll the pointer query until it appears rather than reading it once
 * right after broadcast.
 *
 * In-process the tx runs through the seid shim (which injects --home/--node) against the
 * node's dynamic EVM port; --keyring-dir/--keyring-backend are still needed because the
 * shim's --home alone does not repoint the keyring (same rule the runner's seedParityAdmin
 * follows). The docker path signs with the container keyring via the piped passphrase.
 */
export async function registerCw20Pointer(cw20Address: string): Promise<string> {
    if (isInProcess()) {
        await exec(
            `seid tx evm register-evm-pointer CW20 ${cw20Address} --evm-rpc=${Endpoints.sei.evmRpc} ` +
                `--from admin --keyring-dir ${process.env.SEID_HOME} --keyring-backend test ` +
                `-y --gas-limit 4900000 --fees 800000usei -b sync`,
        );
    } else {
        await exec(
            `docker exec ${DOCKER_NODE} /bin/bash -c '${SEID_ENV} && printf "${DOCKER_KEY_PASSWORD}\\n" | ` +
                `seid tx evm register-evm-pointer CW20 ${cw20Address} --evm-rpc=${DOCKER_EVM_RPC} ` +
                `--from admin -y --gas-limit 4900000 --fees 800000usei -b sync'`,
        );
    }
    return waitUntil<string>(
        async () => (await queryCw20Pointer(cw20Address)) || null,
        { timeoutMs: 60_000, intervalMs: 1_000, label: `CW20 pointer for ${cw20Address}` },
    );
}

export interface Cw20ExecResult {
    hash: string;
    height: number;
    code: number;
    /** Gas the Cosmos tx consumed — the same amount its shell receipt reports on-chain. */
    gasUsed: bigint;
}

export interface PreparedCw20Transfer {
    broadcast(): Promise<{ hash: string; wait: Promise<Cw20ExecResult> }>;
}

const CW20_TRANSFER_MEMO = 'rpc_tests dual-vm cw20 transfer';

async function waitForCw20Exec(
    client: SigningCosmWasmClient,
    hash: string,
): Promise<Cw20ExecResult> {
    const tx = await waitUntil(
        async () => client.getTx(hash),
        { timeoutMs: 60_000, intervalMs: 250, label: `CW20 transfer ${hash}` },
    );
    return {
        hash,
        height: tx.height,
        code: tx.code,
        gasUsed: BigInt(tx.gasUsed),
    };
}

/**
 * Sign a CW20 transfer without broadcasting it. Tests that need a mixed
 * EVM/Cosmos block use this to stage the Cosmos tx first, then release it in
 * the same mempool window as their EVM txs.
 */
export async function prepareCw20Transfer(
    cw20Address: string,
    recipientSei: string,
    amount: string,
    adminMnemonic: string,
): Promise<PreparedCw20Transfer> {
    const { client, address } = await adminCosmWasm(adminMnemonic);
    const msg: MsgExecuteContractEncodeObject = {
        typeUrl: '/cosmwasm.wasm.v1.MsgExecuteContract',
        value: {
            sender: address,
            contract: cw20Address,
            msg: toUtf8(JSON.stringify({ transfer: { recipient: recipientSei, amount } })),
            funds: [],
        },
    };
    const txRaw = await client.sign(address, [msg], EXEC_FEE, CW20_TRANSFER_MEMO);
    const txBytes = TxRaw.encode(txRaw).finish();
    return {
        async broadcast() {
            const hash = await client.broadcastTxSync(txBytes);
            return { hash, wait: waitForCw20Exec(client, hash) };
        },
    };
}

/**
 * Sign and broadcast a CW20 `transfer` from the admin and wait for inclusion. Returns
 * the block height so the rich-block builder can require it to co-locate with the EVM
 * batch. This is a pure Cosmos tx — it must NOT surface through any EVM JSON-RPC.
 */
export async function cw20Transfer(
    cw20Address: string,
    recipientSei: string,
    amount: string,
    adminMnemonic: string,
): Promise<Cw20ExecResult> {
    const prepared = await prepareCw20Transfer(cw20Address, recipientSei, amount, adminMnemonic);
    const pending = await prepared.broadcast();
    return pending.wait;
}
