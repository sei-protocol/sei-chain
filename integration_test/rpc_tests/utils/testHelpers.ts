import { ethers } from 'ethers';
import { expect } from 'chai';
import { EvmAccount } from './wallet';
import { RuntimeState } from './state';
import { JsonRpcEnvelope } from './rpc';

/**
 * Assert two JSON-RPC envelopes carry byte-identical errors (code, message and data).
 * Used by the parity specs to prove Sei and the geth reference fail the exact same way.
 */
export function expectSameError(s: JsonRpcEnvelope, g: JsonRpcEnvelope): void {
    expect(g.error, `geth must error, got result ${JSON.stringify(g.result)}`).to.not.equal(
        undefined,
    );
    expect(s.error, `sei must error, got result ${JSON.stringify(s.result)}`).to.not.equal(
        undefined,
    );
    expect(s.error!.code, 'error.code parity').to.equal(g.error!.code);
    expect(s.error!.message, 'error.message parity').to.equal(g.error!.message);
    expect(s.error!.data, 'error.data parity').to.deep.equal(g.error!.data);
}

/**
 * Deterministically claim `count` accounts from the pre-funded pool, offset by a hash
 * of `salt` so different specs tend to take disjoint slices and avoid serialising on a
 * shared nonce. Accounts are returned connected to `provider`.
 */
export function claimPool(
    runtime: RuntimeState,
    provider: ethers.JsonRpcProvider,
    count: number,
    salt: string,
): EvmAccount[] {
    const pool = runtime.funded.pool;
    let h = 0;
    for (const ch of salt) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
    const start = h % pool.length;
    return Array.from({ length: count }, (_, i) =>
        EvmAccount.fromPrivateKey(pool[(start + i) % pool.length].privateKey, provider),
    );
}

/** Left-pad a uint into its canonical 32-byte ABI word. */
export const encodeUint = (value: bigint): string =>
    ethers.zeroPadValue(ethers.toBeHex(value), 32);

/** Calldata encoders and result decoders bound to a specific ERC20 ABI. */
export class Erc20Calldata {
    constructor(private readonly iface: ethers.Interface) {}

    balanceOf(holder: string): string {
        return this.iface.encodeFunctionData('balanceOf', [holder]);
    }

    transfer(to: string, amount: bigint): string {
        return this.iface.encodeFunctionData('transfer', [to, amount]);
    }

    decodeBalance(hex: string): bigint {
        return this.iface.decodeFunctionResult('balanceOf', hex)[0] as bigint;
    }
}
