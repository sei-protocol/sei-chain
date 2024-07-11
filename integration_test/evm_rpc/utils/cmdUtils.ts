import * as util from 'node:util';
const exec = util.promisify(require('node:child_process').exec);

import {ethers} from 'ethers';
import {DirectSecp256k1HdWallet} from '@cosmjs/proto-signing';


export async function fundEvmWallet(receiverWallet: ethers.HDNodeWallet, rpc: string){
  const {stdout} = await exec('seid keys show admin --address');
  const address = await receiverWallet.getAddress();
  await exec(`seid tx evm send ${address} 10000000000000000000000 --from admin --fees 24000use --evm-rpc=${rpc}`);
  console.log('Funded on evm');
}

export async function fundSeiWallet(receiverWallet: DirectSecp256k1HdWallet | string){
  let address: string;
  if(receiverWallet instanceof DirectSecp256k1HdWallet){
    const [accountData, _] = await receiverWallet.getAccounts();
    address = accountData.address;
  } else {
    address = receiverWallet;
  }
  const {stdout} = await exec('seid keys show admin --address');
  const senderAddress = stdout.trim().replaceAll(' ', '');
  console.log('Funding sei address');
  await exec(`seid tx bank send ${senderAddress} ${address} 10000000000usei --from admin --fees 24200usei -y`);
}

export async function waitFor(seconds: number){
  return new Promise<void>((resolve) =>{
    return setTimeout(() =>{
      resolve();
    }, seconds * 1000)
  })
}