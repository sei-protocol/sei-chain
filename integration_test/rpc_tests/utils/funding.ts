import { ethers } from 'ethers';
import { EvmAccount } from './wallet';

/**
 * Send native sei (in wei) from `from` to `to` and wait for inclusion.
 * Used by the bootstrap to seed fresh EVM accounts.
 *
 * Returns the receipt so callers can record the block number it landed in.
 */
export async function fundEvm(
    from: EvmAccount,
    to: string,
    amountWei: bigint,
): Promise<ethers.TransactionReceipt> {
    const tx = await from.wallet.sendTransaction({ to, value: amountWei });
    const receipt = await tx.wait();
    if (!receipt) {
        throw new Error(`fundEvm: transaction ${tx.hash} did not confirm`);
    }
    return receipt;
}

/**
 * Fund a recipient from an account the node itself holds unlocked, letting the
 * node sign (`eth_sendTransaction`) rather than a local key.
 *
 * This is how we seed a deployer on `geth --dev`: the pre-funded developer account
 * lives in the node's keyring (auto-unlocked) and is regenerated on every restart,
 * so we never have its private key client-side. We send from it via the node, wait
 * for the (insta-mined) receipt, and hand the funded recipient a key we *do* control
 * for subsequent local-signed deploys.
 */
export async function fundFromUnlocked(
    provider: ethers.JsonRpcProvider,
    from: string,
    to: string,
    amountWei: bigint,
): Promise<ethers.TransactionReceipt> {
    const hash: string = await provider.send('eth_sendTransaction', [
        // toQuantity gives the minimal hex encoding geth's hexutil.Big requires.
        // toBeHex pads to whole bytes and can emit a leading zero ("0x056b…"),
        // which geth rejects as "hex number with leading zero digits".
        { from, to, value: ethers.toQuantity(amountWei) },
    ]);
    const receipt = await provider.waitForTransaction(hash);
    if (!receipt) {
        throw new Error(`fundFromUnlocked: transaction ${hash} did not confirm`);
    }
    return receipt;
}

/**
 * Fund many recipients in parallel from a single funder. We do this one nonce at
 * a time but submit broadcast concurrently — Sei's mempool accepts gapless nonces
 * from the same sender, so this is the fastest correct pattern.
 */
export async function fundManyEvm(
    from: EvmAccount,
    recipients: string[],
    amountWei: bigint,
): Promise<ethers.TransactionReceipt[]> {
    if (recipients.length === 0) return [];
    const startNonce = await from.nonce('pending');
    const txs = await Promise.all(
        recipients.map((to, i) =>
            from.wallet.sendTransaction({ to, value: amountWei, nonce: startNonce + i }),
        ),
    );
    const receipts = await Promise.all(txs.map(t => t.wait()));
    receipts.forEach((r, i) => {
        if (!r) throw new Error(`fundManyEvm: tx ${txs[i].hash} did not confirm`);
    });
    return receipts as ethers.TransactionReceipt[];
}
