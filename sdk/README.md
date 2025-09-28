# @web3plus/reward-utils

Typed helper SDK for interacting with Sei reward distributor contracts. The package wraps
common read and write flows, validates configuration, and exposes utilities that are safe to
compose from bot frameworks or dashboards.

## Installation

```bash
npm install @web3plus/reward-utils
```

## Quick start

```ts
import { RewardClient, parseRewardConfig } from "@web3plus/reward-utils";

const config = parseRewardConfig({
  rpcUrl: "https://sei.example.org",
  distributorAddress: "0xDistributor",
});

const client = new RewardClient(config);
const markets = await client.getMarketsWithRewards("0xUser");
console.log("Reward markets", markets);
```

## Features

- ⚙️ Strongly-typed configuration parsing and validation
- 🔄 Batched reward lookups with `getMarketsWithRewards`
- ⛽ Gas estimation helpers through `estimateClaimGas`
- 🧹 Balance sweeping via `sweepTokensTo`
- 🧠 Reusable agent-facing types for automation layers

## Building

```bash
npm run build
```

The compiled output is emitted into `dist/` and includes both JavaScript and type definitions.

## License

MIT
