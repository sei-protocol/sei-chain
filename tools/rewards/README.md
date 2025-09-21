# Reward Automation Toolkit

Utilities for automating MultiRewardDistributor flows on Sei. The toolkit is intentionally modular so it can be wired into cronjobs, serverless workers or custom bots.

## Installation

The scripts rely on [`ethers@6`](https://docs.ethers.org/v6/) and modern Node.js (>= 18). Install dependencies from the project root or inside this directory:

```bash
npm install ethers
```

Optionally install [`dotenv`](https://www.npmjs.com/package/dotenv) if you prefer loading environment variables from a file:

```bash
npm install dotenv
node --env-file=.env tools/rewards/autoClaimer.mjs --help
```

## Auto Claimer

```
node tools/rewards/autoClaimer.mjs \
  --distributor 0x28BF6D71b6Dc837F56F5afbF1F4A46AaC0B1f31E \
  --mode both \
  --loop --interval 120
```

Environment variables:

- `RPC_URL` – HTTP endpoint for Sei EVM RPC.
- `PRIVATE_KEY` – Executor wallet private key.
- Optional overrides `REWARD_DISTRIBUTOR`, `REWARD_USER`.

Flags:

- `--mode` selects `borrow`, `supply` or `both` streams.
- `--gas-limit` provides a manual gas limit override.
- `--dry-run` prints planned transactions without broadcasting.
- `--loop` and `--interval` turn the script into a polling daemon.

## Monitor / Cron Worker

```
node tools/rewards/monitor.mjs \
  --distributor 0x28BF6D71b6Dc837F56F5afbF1F4A46AaC0B1f31E \
  --webhook https://hooks.slack.com/... \
  --metrics-file ./outstanding.log
```

This script only needs `RPC_URL`. It collects outstanding rewards, prints a concise summary and optionally ships structured JSON payloads to a webhook or appends them to a metrics file—ideal for `cron`, GitHub Actions, AWS Lambda, or Cloudflare Workers.

`MONITOR_INTERVAL`, `MONITOR_WEBHOOK`, and `MONITOR_METRICS_FILE` can also be used to configure the daemon via environment variables.

## SDK Helper

Importable helpers live in `tools/rewards/sdk/index.mjs`:

```js
import { createRewardClient } from "./tools/rewards/sdk/index.mjs";
import { ethers } from "ethers";

const provider = new ethers.JsonRpcProvider(process.env.RPC_URL);
const client = createRewardClient(provider, "0x28BF6D71b6Dc837F56F5afbF1F4A46AaC0B1f31E");

await client.claimAllRewards("0xb2b297eF9449aa0905bC318B3bd258c4804BAd98");
```

Each method returns an array of transaction promises so you can await confirmations or feed them into a queueing system.

## Configuration Template

Copy `config.example.json` and adjust addresses for local scripts or bots:

```bash
cp tools/rewards/config.example.json config/rewards.local.json
```

Use it to hydrate environment variables in CI workflows or runtime configs.

## Smart-Contract Hook Adapter

For protocols that prefer on-chain automation, deploy `contracts/src/rewards/MultiRewardHookAdapter.sol` and wire it into the Comptroller/TToken hook callbacks:

```solidity
MultiRewardHookAdapter hook = new MultiRewardHookAdapter(0x28BF6D71b6Dc837F56F5afbF1F4A46AaC0B1f31E);

// Comptroller configuration
comptroller._setBorrowVerify(hook);
comptroller._setMintVerify(hook);
```

Borrow and mint lifecycle events now forward to the distributor so indices stay fresh without waiting for off-chain bots.
