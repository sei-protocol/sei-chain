import {ethers} from 'ethers';

export async function createEvmProvider(rpcUrl: string) {
  return new ethers.JsonRpcProvider(rpcUrl)
}

export async function createEvmWallet(evmClient: ethers.JsonRpcProvider) {
  return ethers.Wallet.createRandom(evmClient);
}

export async function sendFundsFromEvmClient(wallet: ethers.HDNodeWallet, recipientAddress: string) {
  const tx = {
    to: recipientAddress,
    value: ethers.parseUnits('0.1', 'ether'),
  };

  // Send the transaction
  const txResponse = await wallet.sendTransaction(tx);

  // Wait for the transaction to be mined
  const receipt = await txResponse.wait();
  return [receipt?.hash, receipt?.blockNumber, receipt?.blockHash];
}