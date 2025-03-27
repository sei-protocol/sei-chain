import {Random, stringToPath} from '@cosmjs/crypto';
import {coins, DirectSecp256k1HdWallet} from '@cosmjs/proto-signing';
import {coin, SigningStargateClient} from '@cosmjs/stargate';
import {ethers, toBeHex} from 'ethers';
import {cosmos} from '@sei-js/proto';
import {secp256k1} from '@noble/curves/secp256k1';
import {hexToNumber, numberToHex} from 'viem';
import {waitFor} from './cmdUtils';
import {Coin} from '@sei-js/proto/dist/types/codegen/cosmos/base/v1beta1/coin';

export async function createSeiProvider(rpcUrl: string, wallet: DirectSecp256k1HdWallet){
  return await SigningStargateClient.connectWithSigner(rpcUrl, wallet);
}

export async function  createSeiWallet(){
  return await DirectSecp256k1HdWallet.generate(24, {
    prefix: 'sei'
  });
}

export async function associateWallet(evmProvider: ethers.JsonRpcProvider,  evmWallet: ethers.HDNodeWallet){
  const message = "account association";
  const signature = await evmWallet.signMessage(
    message,
  );
  const { r, s } = secp256k1.Signature.fromCompact(signature.slice(2, 130));
  const v = hexToNumber(`0x${signature.slice(130)}`);

  const messageLength = Buffer.from(message, "utf8").length;
  const messageToSign = `\x19Ethereum Signed Message:\n${messageLength}${message}`;
  const request = {
    r: numberToHex(r),
    s: numberToHex(s),
    v: numberToHex(v - 27),
    custom_message: messageToSign,
  };
  await evmProvider.send('sei_associate', [request]);
  await waitFor(2);
}

export async function signMessage(evmWallet: ethers.HDNodeWallet){
  const customMessage = 'associate wallets';
  const sign = await evmWallet.signMessage(customMessage);
  const values = ethers.Signature.from(sign);
  const {r,v,s} = values;
  return {r, v, s};
}

export async function generateEvmAddressFromMnemonic(seiWallet: DirectSecp256k1HdWallet){
  const evmWallet = ethers.HDNodeWallet.fromPhrase(seiWallet.mnemonic, '', 'm/44\'/118\'/0\'/0/0');
  return await evmWallet.getAddress()
}

export async function generateSeiAddressFromMnemonic(evmWallet: ethers.HDNodeWallet){
  const mnemonic = evmWallet.mnemonic!.phrase;
  const wallet = await DirectSecp256k1HdWallet.fromMnemonic(mnemonic, {
    prefix: 'sei',
    hdPaths: [stringToPath('m/44\'/60\'/0\'/0/0')]
  });
  return (await wallet.getAccounts())[0].address;
}

export async function sendFundsFromSeiClient(signingClient: SigningStargateClient, senderWallet: string, receiverAddress: string){
  const fee = {
    amount: coins(24000, "usei"), // fee amount
    gas: "250000", // gas limit
  };  const transferAmount = coin('100000', 'usei');
  const receipt = await signingClient.sendTokens(senderWallet, receiverAddress, [transferAmount], fee);
  return receipt.transactionHash;
}