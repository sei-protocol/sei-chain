import { ethers } from 'ethers';
import { EvmAccount } from './wallet';

/**
 * Helpers for EIP-7702 SetCode (type-4) transactions, kept self-contained so the
 * new_rpc_tests module does not depend on shared/ or the chain_tests pectra utils.
 */

/** Minimal ABI for the SimpleAccount7702 delegation target (executeBatch only). */
export const SIMPLE_ACCOUNT_ABI = [
    {
        inputs: [
            {
                components: [
                    { internalType: 'address', name: 'target', type: 'address' },
                    { internalType: 'uint256', name: 'value', type: 'uint256' },
                    { internalType: 'bytes', name: 'data', type: 'bytes' },
                ],
                internalType: 'struct BaseAccount.Call[]',
                name: 'calls',
                type: 'tuple[]',
            },
        ],
        name: 'executeBatch',
        outputs: [],
        stateMutability: 'nonpayable',
        type: 'function',
    },
] as const;

/** The 0xef0100-prefixed delegation designator geth/Sei store as an EOA's code. */
export function delegationDesignator(implementationAddress: string): string {
    return '0xef0100' + implementationAddress.replace(/^0x/, '').toLowerCase();
}

/**
 * Sign a self-authorization delegating `account` to `implementationAddress`. For a
 * self-sponsored type-4 tx the authorization nonce is the account's current nonce
 * + 1, because the outer tx consumes the current nonce first.
 */
export async function selfAuthorize(
    account: EvmAccount,
    implementationAddress: string,
): Promise<ethers.Authorization> {
    const provider = account.wallet.provider!;
    const [{ chainId }, latest] = await Promise.all([
        provider.getNetwork(),
        provider.getTransactionCount(account.address, 'latest'),
    ]);
    return account.wallet.authorize({
        address: implementationAddress,
        chainId,
        nonce: latest + 1,
    });
}

/** Broadcast a type-4 tx that installs the delegation designator on `account` itself. */
export async function setCodeForEOA(
    account: EvmAccount,
    authorizationList: ethers.Authorization[],
): Promise<ethers.TransactionReceipt | null> {
    const provider = account.wallet.provider!;
    const fee = await provider.getFeeData();
    const tx = await account.wallet.sendTransaction({
        to: account.address,
        data: '0x',
        maxFeePerGas: fee.maxFeePerGas!,
        maxPriorityFeePerGas: fee.maxPriorityFeePerGas!,
        authorizationList,
        type: 4,
    });
    return tx.wait();
}
