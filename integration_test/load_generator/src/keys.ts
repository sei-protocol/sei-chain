import { DirectSecp256k1Wallet, Registry } from '@cosmjs/proto-signing';
import { defaultRegistryTypes } from '@cosmjs/stargate';
import { seiProtoRegistry } from '@sei-js/cosmos/encoding';
import { ethers } from 'ethers';

export function privateKeyAt(mnemonic: string, index: number): string {
    return ethers.HDNodeWallet.fromPhrase(
        mnemonic,
        '',
        `m/44'/118'/0'/0/${index}`,
    ).privateKey;
}

export function cosmosWalletAt(
    mnemonic: string,
    index: number,
): Promise<DirectSecp256k1Wallet> {
    return DirectSecp256k1Wallet.fromKey(ethers.getBytes(privateKeyAt(mnemonic, index)), 'sei');
}

export function replayRegistry(): Registry {
    return new Registry([...seiProtoRegistry, ...defaultRegistryTypes]);
}
