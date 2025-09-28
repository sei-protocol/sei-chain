#!/usr/bin/env node
'use strict';

const rewards = require('./rewards');

async function main() {
  const proxyAddress = process.env.PROXY_ADDRESS ?? rewards.DEFAULT_PROXY_ADDRESS;
  const slot = process.env.IMPLEMENTATION_SLOT ?? rewards.IMPLEMENTATION_SLOT;
  const rewardType = process.env.REWARD_TYPE ?? process.env.ROLE ?? 'borrower';
  const sendTokens = rewards.normalizeBoolean(process.env.SEND_TOKENS, true);
  const waitForReceipt = !rewards.normalizeBoolean(process.env.NO_WAIT, false);

  const provider = await rewards.getProvider(process.env.RPC_URL);
  const implementation = await rewards.fetchImplementationAddress(provider, proxyAddress, slot);
  console.log(`\u{1F9E0} Impl address: ${implementation}`);

  const signer = await rewards.getSigner(process.env.PRIVATE_KEY, provider);
  const { tx, receipt } = await rewards.disburseRewards({
    signer,
    proxyAddress,
    rewardType,
    tToken: process.env.TTOKEN,
    user: process.env.USER,
    sendTokens,
    waitForReceipt
  });

  console.log(`\u{1F4E7} Submitted: ${tx.hash}`);
  if (waitForReceipt && receipt) {
    console.log(`\u{2705} Done in block: ${receipt.blockNumber}`);
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
