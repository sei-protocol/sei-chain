/**
 * Transactions the record phase puts on chain so the verify phase has real
 * pre-upgrade history to re-read.
 *
 * The read-only probes answer "does the chain still behave the same". These
 * answer the harder question: after state is removed, can the chain still serve
 * what it already committed? A receipt that stops resolving, or a mined failure
 * whose reason changes, is the shape a bad state migration takes.
 *
 * These need a funded key. Without one the record phase skips them and the
 * verify phase has nothing to re-read, which keeps the read-only suite usable
 * against any endpoint with no setup.
 */
import { ethers } from 'ethers';
import { ADDRESSES, precompileInterface, rawSei, seiRpc } from './chain';
import type { RecordedTx } from './artifact';

/** Sei's HD path, matching integration_test/dapp_tests/hardhat.config.js. */
const SEI_HD_PATH = "m/44'/118'/0'/0/0";

export function walletFromMnemonic(mnemonic: string): ethers.HDNodeWallet {
    return ethers.HDNodeWallet.fromPhrase(mnemonic, undefined, SEI_HD_PATH).connect(seiRpc());
}

/**
 * eth_getVMError is the only channel carrying a precompile's exact error for a
 * mined, failed transaction: eth_call rewrites most executor errors to a bare
 * "execution reverted". Returns undefined when the node does not serve one.
 */
export async function vmError(txHash: string): Promise<string | undefined> {
    const envelope = await rawSei<string>('eth_getVMError', [txHash]);
    return envelope.result && envelope.result.length > 0 ? envelope.result : undefined;
}

async function record(
    label: string,
    sent: Promise<ethers.TransactionResponse>,
): Promise<RecordedTx> {
    const tx = await sent;
    // A failing transaction rejects in wait(); its receipt is on the error.
    const receipt: ethers.TransactionReceipt | undefined = await tx
        .wait()
        .then(r => r ?? undefined)
        .catch((e: { receipt?: ethers.TransactionReceipt }) => e.receipt);
    if (!receipt) {
        throw new Error(`${label}: transaction ${tx.hash} never produced a receipt`);
    }
    return {
        label,
        hash: receipt.hash,
        blockNumber: receipt.blockNumber,
        status: receipt.status ?? 0,
        vmError: receipt.status === 0 ? await vmError(receipt.hash) : undefined,
    };
}

/**
 * Two transactions: one aimed at a retired precompile, which must mine as a
 * failure, and one ordinary transfer that must mine as a success. Recording both
 * means the verify phase can tell "history is gone" from "failed history is
 * gone", which are different bugs.
 */
export async function sendProbeTransactions(mnemonic: string): Promise<RecordedTx[]> {
    const wallet = walletFromMnemonic(mnemonic);
    const oracleIface = precompileInterface('oracle');

    const retired = await record(
        'oracle.getExchangeRates',
        // An explicit gasLimit is required: estimateGas on a reverting call
        // throws client-side, so the transaction would never be broadcast.
        wallet.sendTransaction({
            to: ADDRESSES.oracle,
            data: oracleIface.encodeFunctionData('getExchangeRates', []),
            gasLimit: 200_000,
        }),
    );

    const transfer = await record(
        'selfTransfer',
        wallet.sendTransaction({ to: wallet.address, value: 1n }),
    );

    return [retired, transfer];
}

export async function fundedAddress(mnemonic: string): Promise<{ address: string; balance: bigint }> {
    const wallet = walletFromMnemonic(mnemonic);
    return { address: wallet.address, balance: await seiRpc().getBalance(wallet.address) };
}
