#!/usr/bin/env node
'use strict';

const rewards = require('./rewards');

async function main() {
  const provider = await rewards.getProvider(process.env.RPC_URL);
  const proxyAddress = process.env.PROXY_ADDRESS ?? rewards.DEFAULT_PROXY_ADDRESS;
  const slot = process.env.IMPLEMENTATION_SLOT ?? rewards.IMPLEMENTATION_SLOT;
  const implementation = await rewards.fetchImplementationAddress(provider, proxyAddress, slot);
  console.log(`\u{1F9E0} Implementation address: ${implementation}`);
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
