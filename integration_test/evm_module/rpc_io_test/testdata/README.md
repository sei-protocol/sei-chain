# testdata - .io and .iox RPC fixtures

**What it is:** Request/response fixtures for Ethereum JSON-RPC methods. The `rpc_io_test` package runs them against a Sei EVM RPC node.

- `**.io` files** - Plain request (`>>`) / expected response (`<<`) pairs. Source: curated mix from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) plus Sei-added tests. **105 files.** Data-dependent .io that required Ethereum fixture hashes were removed; equivalent coverage lives in `.iox`.
- `**.iox` files** - Extended format with `@ bind` and optional `@ expect_same_block`; data comes from a first request (e.g. latest block, deploy receipt). **150 files.** All are Sei-generated and live only in this repo.

**Total: 255 tests** (105 .io + 150 .iox). See `../RPC_IO_README.md` for how to run and outcome meanings.

**Important:** This directory is **not** a direct copy of execution-apis. Do **not** replace it by copying from execution-apis (that would remove all .iox and restore removed .io). To add or update **individual** tests from execution-apis, copy only the specific files you need and avoid overwriting existing `.iox` or curated `.io`. The suite expects both .io and .iox under `testdata/` (and subdirs); if the directory is empty, the integration test skips with a clear message.