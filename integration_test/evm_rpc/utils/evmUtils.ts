import {ethers} from 'ethers';
import abi from '../../../contracts/artifacts/src/BoxV2.sol/BoxV2.json';


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

  const txResponse = await wallet.sendTransaction(tx);

  const receipt = await txResponse.wait();
  return [receipt?.hash, receipt?.blockNumber, receipt?.blockHash];
}


export async function deployToChain(evmWallet: ethers.HDNodeWallet){
  const contractFactory = new ethers.ContractFactory(abi.abi, abi.bytecode, evmWallet);
  const contract = await contractFactory.deploy();
  return {
    address: contract.target,
    bytecode: abi.deployedBytecode
  };
}
