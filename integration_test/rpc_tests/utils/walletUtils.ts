import { ethers } from 'ethers';
import { expect } from 'chai';

/**
 * Helpers for the node-hosted-key wallet methods (eth_sign, eth_signTransaction,
 * eth_sendTransaction), each requiring the node itself to hold the signer's private key.
 * geth --dev auto-unlocks its first account, so it is the reference signer. Sei never
 * hosts arbitrary keys over JSON-RPC (sign client-side and submit via
 * eth_sendRawTransaction), so the same calls are rejected there — the specs assert geth's
 * happy-path schema and Sei's rejection.
 */

/** A signature with no 0x prefix is exactly 65 bytes: r(32) ‖ s(32) ‖ v(1). */
export const SIGNATURE_65 = /^0x[0-9a-fA-F]{130}$/;

/** The geth --dev node's auto-unlocked account — the only key it can node-sign with. */
export async function gethUnlockedAccount(geth: ethers.JsonRpcProvider): Promise<string> {
    const accounts: string[] = await geth.send('eth_accounts', []);
    const from = accounts[0];
    if (!from) throw new Error('gethUnlockedAccount: geth --dev returned no unlocked account');
    return from;
}

/**
 * Assert an eth_sign result is a 65-byte signature that recovers `signer` over `message`.
 * eth_sign hashes with the EIP-191 personal-sign prefix, which ethers.verifyMessage mirrors.
 */
export function assertEthSignRecovers(signer: string, message: string, sig: string): void {
    expect(sig, 'signature is 0x + 65 bytes').to.match(SIGNATURE_65);
    const recovered = ethers.verifyMessage(ethers.getBytes(message), sig);
    expect(recovered.toLowerCase(), 'eth_sign recovers the signer').to.equal(signer.toLowerCase());
}

/**
 * A minimal value-transfer arg object for eth_sendTransaction, which fills in nonce, gas
 * price and gas itself from the node's defaults.
 */
export function transferArgs(from: string, to: string, valueWei: bigint): Record<string, string> {
    return {
        from,
        to,
        value: ethers.toQuantity(valueWei),
        gas: ethers.toQuantity(21000),
    };
}

/**
 * A fully-specified value-transfer arg object for eth_signTransaction, which (unlike
 * eth_sendTransaction) does not default nonce / gasPrice — every field must be supplied.
 */
export async function signableTxArgs(
    provider: ethers.JsonRpcProvider,
    from: string,
    to: string,
    valueWei: bigint,
): Promise<Record<string, string>> {
    const [nonce, price] = await Promise.all([
        provider.getTransactionCount(from, 'pending'),
        provider.send('eth_gasPrice', []),
    ]);
    return {
        ...transferArgs(from, to, valueWei),
        nonce: ethers.toQuantity(nonce),
        gasPrice: price,
    };
}
