# Split Go test runner

This directory contains a standalone split-test runner that is independent of the `Makefile` split logic.

## Script

- `run-go-test-shard.sh`

The script:

1. Discovers packages that actually contain tests.
2. Optionally filters excluded package regexes.
3. Assigns packages deterministically to shards using a hash of package import path.
4. Runs `go test` for one shard with optional `-race` and/or coverage flags.

## Local usage

```bash
# List packages assigned to shard 0/4
bash scripts/testing/run-go-test-shard.sh \
  --shard-index 0 \
  --shard-count 4 \
  --list-packages

# Run shard 2/8 with race detector
bash scripts/testing/run-go-test-shard.sh \
  --module-dir . \
  --shard-index 2 \
  --shard-count 8 \
  --tags "ledger test_ledger_mock" \
  --race

# Run shard 1/8 with coverage output
bash scripts/testing/run-go-test-shard.sh \
  --module-dir . \
  --shard-index 1 \
  --shard-count 8 \
  --tags "ledger test_ledger_mock" \
  --coverage \
  --coverprofile coverage-1.out
```

## GitHub Actions matrix example

```yaml
strategy:
  fail-fast: false
  matrix:
    shard: [0, 1, 2, 3, 4, 5, 6, 7]

steps:
  - uses: actions/checkout@v5
  - uses: actions/setup-go@v6
    with:
      go-version: "1.25.6"

  - name: Run sharded race tests
    run: |
      bash scripts/testing/run-go-test-shard.sh \
        --module-dir . \
        --shard-index ${{ matrix.shard }} \
        --shard-count 8 \
        --tags "ledger test_ledger_mock" \
        --race

  - name: Run sharded coverage tests
    run: |
      bash scripts/testing/run-go-test-shard.sh \
        --module-dir . \
        --shard-index ${{ matrix.shard }} \
        --shard-count 8 \
        --tags "ledger test_ledger_mock" \
        --coverage \
        --coverprofile coverage-${{ matrix.shard }}.out
```

To merge coverage shards, use your existing coverage tooling (e.g., upload each profile separately or merge with a post-processing step).
