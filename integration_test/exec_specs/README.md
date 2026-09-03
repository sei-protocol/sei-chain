# Ethereum Execution Specs

This suite runs the upstream Ethereum execution-spec tests against a live,
local Sei devnet through EEST's remote execution mode. It covers the Prague
transaction, RLP, and state-transition vectors that can be represented on a
persistent Sei chain.

It also runs the legacy `ethereum/tests` `TransactionTests` and `RLPTests`
directly through Sei's pinned go-ethereum fork. Those library-level suites are
fast and do not require a devnet.

The upstream repository and revision are pinned in `scripts/install.sh`. A
versioned compatibility patch adapts the remote runner to Sei without copying
the upstream vectors into this repository. Applicability exclusions are
documented under `config/`, while predicate-based skips and deterministic
sharding live under `plugin/`.

## Run locally

Prerequisites:

- a running four-node devnet (`make docker-cluster-start`)
- Node.js and the dependencies from `integration_test/precompile_tests`
- Python 3.12 and [`uv`](https://docs.astral.sh/uv/)

From the repository root:

```bash
npm ci --prefix integration_test/precompile_tests
seed_key="$(
  npm exec --prefix integration_test/precompile_tests -- \
    tsx integration_test/exec_specs/scripts/prepare_admin.ts
)"
EEST_SEED_KEY="${seed_key}" \
  bash integration_test/exec_specs/scripts/run_suite.sh
```

Run the transaction and RLP fixtures from the repository root:

```bash
bash integration_test/exec_specs/scripts/run_transaction_rlp.sh
```

Run only the converted legacy state tests against the funded devnet:

```bash
EEST_SEED_KEY="${seed_key}" \
EEST_TEST_PATHS_FILE=integration_test/exec_specs/config/ported-static-paths.list \
  bash integration_test/exec_specs/scripts/run_suite.sh
```

`prepare_admin.ts` creates a fresh mnemonic in memory and reuses the existing
integration-test funding and association helpers. The mnemonic is never stored
in this repository. `EEST_SEED_KEY` may also point to another funded,
disposable account when running against a non-local chain.

Local runs strictly validate pending `eth_getTransactionByHash` responses.
CI explicitly sets `EEST_TOLERATE_MALFORMED_PENDING_TX=1` because Sei currently
omits `chainId`, `accessList`, `maxFeePerGas`, and `maxPriorityFeePerGas` from
pending typed-transaction responses. Tolerated validation errors remain visible
as warnings and polling validates the transaction after block inclusion.

## Compatibility patch policy

Most of `patches/sei-compat.patch` adapts EEST's remote runner: it funds
ephemeral accounts, submits transactions in order, polls delayed post-state,
and handles Sei RPC compatibility. The upstream test-body changes are limited
to cases that cannot otherwise execute faithfully against a live Sei chain:

- `test_chainid.py` and `test_eip150_selfdestruct.py` use the devnet gas price
  instead of fixed legacy prices below Sei's admission minimum.
- Two converted static tests drop `pre_alloc_mutable` because their current
  bodies only create fresh accounts; the marker makes EEST's remote executor
  skip them even though they do not mutate existing pre-state.
- The EIP-150 SELFDESTRUCT tests skip vectors that require restoring a
  precompile's balance, which a persistent remote chain cannot do.
- `test_set_code_txs.py` raises fixed EIP-1559 fee caps above the devnet base
  fee so the transaction reaches the behavior under test.

These changes adapt transaction construction or remote-state limitations; they
do not alter expected post-state. Re-evaluate each hunk when updating the
pinned EEST revision.

## Known Sei deviations

- `eip7623-admission` and `eip7623-floor-data-gas` are tracked by
  [#4068](https://github.com/sei-protocol/sei-chain/issues/4068).
- `eip6780-repeated-selfdestruct` is tracked by
  [#4069](https://github.com/sei-protocol/sei-chain/issues/4069).

Other exclusions in `plugin/eest_plugin.py` and `config/prague-ignores.list`
describe persistent-chain or genesis-configuration limitations rather than
known execution defects.

## CI

The jobs run only after changes reach `main`:

- Transaction and RLP fixtures run together on one standard runner.
- Regular execution-spec vectors are distributed over eight independent
  devnets.
- The EIP-2929 precompile family is excluded from remote execution because its
  vectors require clean state per test. It will remain disabled until it can
  run through a state-isolated fixture runner.
- Converted legacy state tests also run as one explicit, unsharded partition
  so their established ~18 minute result remains easy to review. The EIP-2929
  family is excluded there for the same persistent-state limitation.

JUnit reports are uploaded per EEST partition and summarized in the workflow
run. Transaction/RLP results are printed directly in their job log.

The workflow runs only after changes reach `main`, so it is not a pull-request
gate. Failures remain visible as failed post-merge workflow runs instead of
being converted into green results.
