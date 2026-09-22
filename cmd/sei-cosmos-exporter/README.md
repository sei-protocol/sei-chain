# sei-cosmos-exporter

`sei-cosmos-exporter` is a Prometheus exporter that queries a Sei node over
gRPC and Tendermint RPC and serves the results as metrics. It is a fork of
[solarlabsteam/cosmos-exporter](https://github.com/solarlabsteam/cosmos-exporter)
and runs as a sidecar next to `seid` on port 9300.

```sh
make build-sei-cosmos-exporter
./build/sei-cosmos-exporter --node localhost:9090 --tendermint-rpc http://localhost:26657
```

Or as a container, built from the repository root:

```sh
docker build -f docker/cosmos-exporter/Dockerfile .
```

## Endpoints

| Path                  | Query param | Metrics prefix        |
| --------------------- | ----------- | --------------------- |
| `/metrics/general`    |             | `cosmos_general_*`    |
| `/metrics/params`     |             | `cosmos_params_*`     |
| `/metrics/validators` |             | `cosmos_validators_*` |
| `/metrics/validator`  | `address`   | `cosmos_validator_*`  |
| `/metrics/wallet`     | `address`   | `cosmos_wallet_*`     |
| `/metrics/oracle`     | `address`   | `cosmos_oracle_*`     |
| `/metrics/event`      |             | `cosmos_bank_*`       |

Every metric carries a `chain_id` label taken from the node's Tendermint status.

## Flags

- `--node` — gRPC endpoint. Defaults to `localhost:9090`.
- `--tendermint-rpc` — Tendermint RPC endpoint. Defaults to `http://localhost:26657`.
- `--listen-address` — address to serve metrics on. Defaults to `:9300`.
- `--denom` — display denom. Fetched from the node's bank metadata when unset.
- `--denom-coefficient` / `--denom-exponent` — base-to-display conversion; provide at most one.
- `--bech-prefix` — Bech32 prefix. Defaults to `sei`. Individual prefixes can be
  overridden with `--bech-account-prefix`, `--bech-validator-prefix`,
  `--bech-consensus-node-prefix` and their `-pubkey-` variants.
- `--bank-transfer-threshold` — minimum amount for a bank transfer to be counted
  by `/metrics/event`. Defaults to `1e12`.
- `--limit` — pagination limit for gRPC requests. Defaults to `1000`.
- `--log-level`, `--json` — logging level and JSON output.
- `--config` — path to a viper-compatible config file supplying any of the above.
