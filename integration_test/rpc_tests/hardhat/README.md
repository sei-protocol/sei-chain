# Local Hardhat mainnet fork

This config spins up a JSON-RPC node on `http://127.0.0.1:9546` that mirrors
Ethereum mainnet at the latest (or a pinned) block. It's the canonical Ethereum
reference for `tests/new_rpc_tests/` — we compare Sei's RPC behavior against it
instead of hitting an upstream Alchemy/Infura endpoint, which is flaky and rate
limited.

## Quick start

```bash
# In a dedicated terminal, leave this running for the duration of your test
# session.
yarn rpc:fork
```

Then in another terminal:

```bash
yarn test:rpc
```

## Environment

| Variable                | Default                  | Purpose                                                      |
| ----------------------- | ------------------------ | ------------------------------------------------------------ |
| `ETH_MAINNET_UPSTREAM`  | (required, no default)   | Mainnet RPC URL the fork pulls state from. Provide your own. |
| `ETH_MAINNET_FORK_BLOCK`| (unset → latest)         | Pin to a specific block for determinism.                     |

## Notes

- `chainId` is `1`, matching mainnet, so `eth_chainId` and `net_version`
  assertions against the fork agree with the upstream Ethereum semantics.
- Artifacts and cache live under `.artifacts/`, `.cache/` inside this folder so
  they do not collide with the repository-level `artifacts/`.
- This fork is only an RPC reference. Sei deployments still happen on the local
  Sei node — see `_start/00_bootstrap.spec.ts`.
