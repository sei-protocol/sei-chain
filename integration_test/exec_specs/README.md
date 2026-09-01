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

The jobs are informational (`continue-on-error`) and therefore do not make the
post-merge workflow fail.
