# testdata - .io and .iox RPC fixtures

**What it is:** Request/response fixtures for Ethereum JSON-RPC methods. The `rpc_io_test` package runs them against a Sei EVM RPC node.

- `**.io` files** - Plain request (`>>`) / expected response (`<<`) pairs. Source: curated mix from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) plus Sei-added tests. **90 files.** Data-dependent .io that required Ethereum fixture hashes were removed; equivalent coverage lives in `.iox`.
- `**.iox` files** - Extended format with bindings, reference pairs, and response/error assertions. **64 files.** All are Sei-generated and live only in this repo.

**Total: 154 tests** (90 `.io` + 64 `.iox`). **57** top-level method folders under `testdata/`. See `../RPC_IO_README.md` for how to run and outcome meanings.

**Important:** This directory is **not** a direct copy of execution-apis. Do **not** replace it by copying from execution-apis (that would remove all .iox and restore removed .io). To add or update **individual** tests from execution-apis, copy only the specific files you need and avoid overwriting existing `.iox` or curated `.io`. The suite expects both .io and .iox under `testdata/` (and subdirs); if the directory is empty, the integration test skips with a clear message.