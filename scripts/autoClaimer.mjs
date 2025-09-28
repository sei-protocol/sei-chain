#!/usr/bin/env node
import rewards from './rewards/index.js';

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

async function runFetchImplementation(options) {
  const provider = await rewards.getProvider(options.rpc ?? process.env.RPC_URL);
  const proxy = options.proxy ?? process.env.PROXY_ADDRESS ?? rewards.DEFAULT_PROXY_ADDRESS;
  const slot = options.slot ?? process.env.IMPLEMENTATION_SLOT ?? rewards.IMPLEMENTATION_SLOT;
  const implementation = await rewards.fetchImplementationAddress(provider, proxy, slot);

  if (options.json || process.env.JSON_OUTPUT) {
    console.log(
      JSON.stringify(
        {
          proxy,
          slot,
          implementation
        },
        null,
        2
      )
    );
    return;
  }

  console.log(`\u{1F9E0} Implementation address: ${implementation}`);
}

async function runCalldata(options) {
  const rewardType = options.method ?? options.rewardType ?? options.role ?? options._[0] ?? process.env.METHOD ?? process.env.REWARD_TYPE ?? process.env.ROLE ?? 'borrower';
  const tToken = options.ttoken ?? options.market ?? options._[1] ?? process.env.TTOKEN;
  const user = options.user ?? options.account ?? options._[2] ?? process.env.USER;
  const sendTokensInput = options.sendTokens ?? options.send ?? options._[3] ?? process.env.SEND_TOKENS;
  const sendTokens = rewards.normalizeBoolean(sendTokensInput, true);

  const calldata = await rewards.encodeDisburseCalldata({
    rewardType,
    tToken,
    user,
    sendTokens
  });

  if (options.json || process.env.JSON_OUTPUT) {
    console.log(
      JSON.stringify(
        {
          method: rewards.getMethodForRewardType(rewardType),
          proxy: options.proxy ?? process.env.PROXY_ADDRESS ?? rewards.DEFAULT_PROXY_ADDRESS,
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

async function runDisburse(options) {
  const rewardType = options.method ?? options.rewardType ?? options.role ?? options._[0] ?? process.env.REWARD_TYPE ?? process.env.ROLE ?? 'borrower';
  const tToken = options.ttoken ?? options.market ?? options._[1] ?? process.env.TTOKEN;
  const user = options.user ?? options.account ?? options._[2] ?? process.env.USER;
  const sendTokensInput = options.sendTokens ?? options.send ?? options._[3] ?? process.env.SEND_TOKENS;
  const sendTokens = rewards.normalizeBoolean(sendTokensInput, true);
  const waitForReceipt = !rewards.normalizeBoolean(options.noWait ?? process.env.NO_WAIT, false);

  const proxyAddress = options.proxy ?? process.env.PROXY_ADDRESS ?? rewards.DEFAULT_PROXY_ADDRESS;
  const provider = await rewards.getProvider(options.rpc ?? process.env.RPC_URL);
  const signer = await rewards.getSigner(options.privateKey ?? process.env.PRIVATE_KEY, provider);

  const { tx, receipt } = await rewards.disburseRewards({
    signer,
    proxyAddress,
    rewardType,
    tToken,
    user,
    sendTokens,
    waitForReceipt
  });

  if (options.json || process.env.JSON_OUTPUT) {
    console.log(
      JSON.stringify(
        {
          proxy: proxyAddress,
          rewardType: rewards.getMethodForRewardType(rewardType),
          tToken,
          user,
          sendTokens,
          txHash: tx.hash,
          blockNumber: receipt?.blockNumber ?? null
        },
        null,
        2
      )
    );
    return;
  }

  console.log(`\u{1F4E7} Submitted TX: ${tx.hash}`);
  if (waitForReceipt && receipt) {
    console.log(`\u{2705} Confirmed in block: ${receipt.blockNumber}`);
  } else if (!waitForReceipt) {
    console.log('\u{26A0}\u{FE0F} Waiting skipped (noWait flag set).');
  }
}

function printHelp() {
  console.log(`Usage: node scripts/autoClaimer.mjs <command> [options]\n`);
  console.log('Commands:');
  console.log('  fetch-implementation   Resolve proxy implementation address.');
  console.log('  disburse               Submit a borrower/supplier reward disbursement.');
  console.log('  calldata               Generate calldata for multisig or governance.');
  console.log('\nCommon options:');
  console.log('  --proxy <address>      Override proxy address (defaults to Sei rewards proxy).');
  console.log('  --rpc <url>            JSON-RPC endpoint (or set RPC_URL).');
  console.log('  --json                 Emit structured JSON instead of text output.');
  console.log('\nDisburse options:');
  console.log('  --rewardType <type>    borrower (default) or supplier.');
  console.log('  --ttoken <address>     Target market address.');
  console.log('  --user <address>       Account receiving rewards.');
  console.log('  --sendTokens <bool>    Whether to transfer accrued tokens (default true).');
  console.log('  --noWait               Do not wait for transaction confirmation.');
  console.log('\nCalldata options mirror disburse options.');
}

async function main() {
  const parsed = parseArgs(process.argv.slice(2));
  const [commandRaw = 'help', ...positionals] = parsed._;
  parsed._ = positionals;
  const command = commandRaw.toLowerCase();

  if (parsed.help || command === '--help') {
    printHelp();
    return;
  }

  if (command === 'fetch-implementation' || command === 'fetch' || command === 'implementation') {
    await runFetchImplementation(parsed);
    return;
  }

  if (command === 'disburse') {
    await runDisburse(parsed);
    return;
  }

  if (command === 'calldata') {
    await runCalldata(parsed);
    return;
  }

  printHelp();
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
