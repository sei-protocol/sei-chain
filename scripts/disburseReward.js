#!/usr/bin/env node
'use strict';

const rewards = require('./rewards');

async function main() {
  const proxyAddress = process.env.PROXY_ADDRESS ?? rewards.DEFAULT_PROXY_ADDRESS;
  const rewardType = process.env.REWARD_TYPE ?? process.env.ROLE ?? 'borrower';
  const tToken = process.env.TTOKEN;
  const user = process.env.USER;
  const sendTokens = rewards.normalizeBoolean(process.env.SEND_TOKENS, true);
  const waitForReceipt = !rewards.normalizeBoolean(process.env.NO_WAIT, false);

  const provider = await rewards.getProvider(process.env.RPC_URL);
  const signer = await rewards.getSigner(process.env.PRIVATE_KEY, provider);
  const { tx, receipt } = await rewards.disburseRewards({
    signer,
    proxyAddress,
    rewardType,
    tToken,
    user,
    sendTokens,
    waitForReceipt
  });

  console.log(`\u{1F4E7} Submitted TX: ${tx.hash}`);
  if (waitForReceipt && receipt) {
    console.log(`\u{2705} Confirmed in block: ${receipt.blockNumber}`);
  } else {
    console.log('\u{26A0}\u{FE0F} Waiting skipped (NO_WAIT set).');
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
