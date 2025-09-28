#!/usr/bin/env node
'use strict';

const rewards = require('./rewards');

function parseArgs(argv) {
  const result = { _: [] };
  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (!token.startsWith('--')) {
      result._.push(token);
      continue;
    }
    const key = token.slice(2);
    const next = argv[i + 1];
    if (next && !next.startsWith('--')) {
      result[key] = next;
      i += 1;
    } else {
      result[key] = true;
    }
  }
  return result;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const rewardType = args.method ?? args.rewardType ?? args.role ?? args._[0] ?? process.env.METHOD ?? process.env.REWARD_TYPE ?? process.env.ROLE ?? 'borrower';
  const tToken = args.ttoken ?? args.market ?? args._[1] ?? process.env.TTOKEN;
  const user = args.user ?? args.account ?? args._[2] ?? process.env.USER;
  const sendTokensInput = args.sendTokens ?? args.send ?? args._[3] ?? process.env.SEND_TOKENS;
  const sendTokens = rewards.normalizeBoolean(sendTokensInput, true);

  const calldata = await rewards.encodeDisburseCalldata({
    rewardType,
    tToken,
    user,
    sendTokens
  });

  if (args.json || process.env.JSON_OUTPUT) {
    console.log(
      JSON.stringify(
        {
          method: rewards.getMethodForRewardType(rewardType),
          proxy: process.env.PROXY_ADDRESS ?? rewards.DEFAULT_PROXY_ADDRESS,
          tToken,
          user,
          sendTokens,
          calldata
        },
        null,
        2
      )
    );
    return;
  }

  console.log(`\u{1F517} Calldata: ${calldata}`);
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
