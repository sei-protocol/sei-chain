import { ethers } from 'ethers';
import { expect } from 'chai';
import {
    sharedRichBlock,
    sendSingleTx,
    SentTx,
    assertCanonicalTx,
    assertTxTypeSchema,
    TX_RECEIPT_SHARED_FIELDS,
} from './txUtils';
import { EvmAccount, fundFromUnlocked } from './evmUtils';

// The suite-wide rich block lives in txUtils (built once per process); re-export it here so
// the tx-lookup specs keep importing their fixtures from a single module.
export { sharedRichBlock };

/**
 * Shared fixtures + assertions for the transaction-lookup specs (eth_getTransactionByHash,
 * eth_getTransactionByBlockHashAndIndex, eth_getTransactionByBlockNumberAndIndex,
 * eth_getTransactionReceipt). Field partitioning per the execution-apis schemas
 * (transaction.yaml / receipt.yaml): SHARED (identical key+value on both), TX_ONLY (signed-intent,
 * tx only), RECEIPT_ONLY (execution-outcome, receipt only); the same value is tx.hash and
 * receipt.transactionHash (renamed).
 */
// Shared identity fields carried on both the tx object and the receipt. The partition
// lives in txUtils (CORE_RECEIPT_FIELDS / TX_RECEIPT_SHARED_FIELDS); re-export it here so
// the tx-lookup specs keep their `TX_RECEIPT_SHARED_KEYS` name without duplicating the list.
export const TX_RECEIPT_SHARED_KEYS = TX_RECEIPT_SHARED_FIELDS;

export const TX_ONLY_KEYS = ['gas', 'gasPrice', 'input', 'nonce', 'value', 'v', 'r', 's'] as const;

export const RECEIPT_ONLY_KEYS = [
    'cumulativeGasUsed',
    'effectiveGasPrice',
    'gasUsed',
    'logs',
    'logsBloom',
    'status',
] as const;

/** Assert a tx-lookup response is a fully-formed, correctly-linked, type-correct tx object. */
export function assertTxObject(tx: any, block: { hash: string; number: number }): void {
    assertCanonicalTx(tx, block);
    assertTxTypeSchema(tx);
}

/** Assert the RPC tx object's values match the transaction we actually sent. */
export function assertTxMatchesSent(tx: any, sent: SentTx): void {
    expect(tx.hash, 'hash').to.equal(sent.hash);
    expect(tx.from.toLowerCase(), 'from == sender').to.equal(sent.sender.toLowerCase());
    const to = tx.to === null ? null : (tx.to as string).toLowerCase();
    expect(to, 'to == recipient (null on creation)').to.equal(
        sent.to ? sent.to.toLowerCase() : null,
    );
    expect(BigInt(tx.value), 'value').to.equal(sent.value);
    expect(BigInt(tx.nonce), 'nonce').to.equal(BigInt(sent.nonce));
    expect(tx.input, 'input/calldata').to.equal(sent.data);
    expect(Number(BigInt(tx.type)), 'type byte').to.equal(sent.type);
    if (sent.type === 0 || sent.type === 1) {
        expect(BigInt(tx.gasPrice), 'legacy/2930 gasPrice').to.equal(sent.gasPrice!);
    }
    if (sent.type === 2 || sent.type === 4) {
        expect(BigInt(tx.maxFeePerGas), 'maxFeePerGas').to.equal(sent.maxFeePerGas!);
        expect(BigInt(tx.maxPriorityFeePerGas), 'maxPriorityFeePerGas').to.equal(
            sent.maxPriorityFeePerGas!,
        );
    }
}

/**
 * Assert the tx object and the receipt for the same transaction are mutually consistent
 * and correctly partitioned per the execution-apis schemas.
 */
export function assertTxReceiptConsistency(tx: any, rc: any): void {
    expect(tx.hash, 'tx.hash == receipt.transactionHash').to.equal(rc.transactionHash);

    for (const k of TX_RECEIPT_SHARED_KEYS) {
        // Sei currently drops `to` on creation receipts; compare only when present.
        if (k === 'to' && !(k in rc)) continue;
        expect(tx[k], `shared field ${k} agrees between tx and receipt`).to.deep.equal(rc[k]);
    }

    for (const k of RECEIPT_ONLY_KEYS) {
        expect(k in tx, `tx object must NOT expose receipt field ${k}`).to.equal(false);
    }
    for (const k of TX_ONLY_KEYS) {
        expect(k in rc, `receipt must NOT expose tx field ${k}`).to.equal(false);
    }
}

/**
 * Fund a fresh signer from geth's unlocked --dev account and land one EIP-1559 tx.
 * Used purely as the schema/error oracle: geth is the reference for the response
 * key-set and the JSON-RPC error messages, nothing more.
 */
export async function oneGethTx(
    geth: ethers.JsonRpcProvider,
): Promise<{ number: number; hash: string; tx: SentTx }> {
    const gethDev: string = (await geth.send('eth_accounts', []))[0];
    const signer = EvmAccount.fromPrivateKey(ethers.Wallet.createRandom().privateKey, geth);
    await fundFromUnlocked(geth, gethDev, signer.address, ethers.parseEther('10'));
    return sendSingleTx(geth, signer);
}

// All four tx-lookup specs need the geth oracle tx only to compare response key-sets and
// error messages, so build it ONCE per process (mirroring the shared rich block). This cuts
// four geth fund+mine round-trips down to one and removes the main `before`-hook timeout
// surface (a single geth tx that occasionally takes >60s to confirm under suite load).
// A failed build resets the cache so the next spec retries rather than inheriting the error.
type GethTx = { number: number; hash: string; tx: SentTx };
let cachedGethTx: GethTx | undefined;
let cachedGethTxPromise: Promise<GethTx> | undefined;

export async function sharedGethTx(geth: ethers.JsonRpcProvider): Promise<GethTx> {
    if (cachedGethTx) return cachedGethTx;
    if (!cachedGethTxPromise) {
        cachedGethTxPromise = (async () => {
            let lastErr: unknown;
            for (let attempt = 0; attempt < 3; attempt++) {
                try {
                    cachedGethTx = await oneGethTx(geth);
                    return cachedGethTx;
                } catch (e) {
                    lastErr = e;
                }
            }
            throw new Error(`sharedGethTx: geth oracle tx failed after 3 attempts: ${lastErr}`);
        })().catch(e => {
            cachedGethTxPromise = undefined; // let a later spec retry from scratch
            throw e;
        });
    }
    return cachedGethTxPromise;
}

/**
 * Assert two tx objects expose the same field set, ignoring `blockTimestamp` — a known Sei
 * divergence (geth stamps it on tx objects; Sei does not). Both objects must be the SAME
 * transaction type, since the typed-tx EIPs gate which fee fields appear.
 */
export function assertTxKeysetParity(seiTx: any, gethTx: any): void {
    const keys = (o: any) =>
        Object.keys(o)
            .filter(k => k !== 'blockTimestamp')
            .sort();
    expect(
        keys(seiTx),
        'tx-object key set parity (excluding the known blockTimestamp divergence)',
    ).to.deep.equal(keys(gethTx));
}

/** All logs in a block, fetched via eth_getLogs by blockHash. */
export function logsByBlockHash(provider: ethers.JsonRpcProvider, blockHash: string): Promise<any[]> {
    return provider.send('eth_getLogs', [{ blockHash }]);
}

/** All logs across a (single-block) range, fetched via the filter lifecycle. */
export async function filterLogsForBlock(
    provider: ethers.JsonRpcProvider,
    blockNumber: number,
): Promise<any[]> {
    const tag = ethers.toQuantity(blockNumber);
    const id = await provider.send('eth_newFilter', [{ fromBlock: tag, toBlock: tag }]);
    try {
        return await provider.send('eth_getFilterLogs', [id]);
    } finally {
        await provider.send('eth_uninstallFilter', [id]);
    }
}
