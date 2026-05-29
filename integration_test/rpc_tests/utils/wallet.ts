import { ethers, HDNodeWallet, Wallet } from 'ethers';
import { seiRpc } from './providers';

const HD_PATH = "m/44'/118'/0'/0/0";

export class EvmAccount {
    readonly wallet: HDNodeWallet | Wallet;
    readonly address: string;

    private constructor(wallet: HDNodeWallet | Wallet) {
        this.wallet = wallet;
        this.address = wallet.address;
    }

    static fromMnemonic(mnemonic: string, provider = seiRpc()): EvmAccount {
        const wallet = ethers.HDNodeWallet.fromPhrase(mnemonic, '', HD_PATH).connect(provider);
        return new EvmAccount(wallet);
    }

    static fromPrivateKey(privateKey: string, provider = seiRpc()): EvmAccount {
        const wallet = new ethers.Wallet(privateKey, provider);
        return new EvmAccount(wallet);
    }

    static random(provider = seiRpc()): EvmAccount {
        const wallet = ethers.Wallet.createRandom().connect(provider);
        return new EvmAccount(wallet);
    }

    nonce(blockTag: ethers.BlockTag = 'latest'): Promise<number> {
        return this.wallet.provider!.getTransactionCount(this.address, blockTag);
    }

    balance(blockTag: ethers.BlockTag = 'latest'): Promise<bigint> {
        return this.wallet.provider!.getBalance(this.address, blockTag);
    }
}
