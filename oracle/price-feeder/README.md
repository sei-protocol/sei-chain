# Oracle Price Feeder

This is a standalone version of [Umee's fantastic work](https://github.com/umee-network/umee/tree/main/price-feeder) and integrating it more closely with the Cosmos SDK.

## Changes

- `exchange_rates` when broadcasting votes has been reverted to the Cosmos SDK denom string, as is used in Se's Oracle module
- `config.toml` supports an `account.prefix` property, to provide compatibility across multiple networks

---


# Setup
If a cluster is running Oracle price-feeder, your validator is also required to run a price feeder or your validator will be jailed for missing votes.


## Create an account for Oracle Price Feeder Delegate
1) To avoid account sequence errors with the admin account, it's recommended to create a different account as an Oracle delegate. To do so, you'll need to create the account with
`seid keys add price-feeder-delegate` or any other account name. This may still cause account sequence errors for the delegate account but since it's only being used for the Oracle price feeder, it's not a concern
2) With the account address output, `export PRICE_FEEDER_DELEGATE_ADDR=<output>`
3) `seid tx oracle set-feeder $PRICE_FEEDER_DELEGATE_ADDR --from <validator-wallet> --fees 2000usei -b block -y --chain-id {chain-id}`
4) Make sure to send bank a tiny amount to the account in order for the account to be created `seid tx bank send [VALIDATOR_ACCOUNT] $PRICE_FEEDER_DELEGATE_ADDR --from [VALIDATOR_ACCOUNT] [AMOUNT] --fees=2000usei -b block -y`

Then you need to export `PRICE_FEEDER_PASS` environment variable to set up the keyring password. That was entered during the account setup.

Ex :
`export PRICE_FEEDER_PASS=keyringPassword`

If this environment variable is not set, the price feeder will prompt the user for input.

## Setup Healthchecks
Add this to the config.toml, you need to add the timeout field as it's required
```
[[healthchecks]]
url = "https://hc-ping.com/xxxxxx"
timeout = "5s"
```


## Make and install the binary
From the root of the Git repo

```bash
make install-price-feeder
```

## Run Price Feeder
You can run it as a seperate binary but it's reccomedned to run it as a systemd service, you can use the following as an example.

You need to setup a config.toml file (see [this for example](./config.example.toml)), you need to set the following fields in

```bash
...
[account]
address = "<UPDATE ME>"  <-- $PRICE_FEEDER_DELEGATE_ADDR from above
chain_id = "<UPDATE ME>"
validator = "<UPDATE ME>" <-- validator address
...
```

## Systemd Configuration

In order to run the price feeder as a background process, you can set up a systemd service for it. Here is an example of the service that will run the price feeder process. Then you just need to run `systemctl enable <service-name>` and `systemctl start <service-name>`

```ini
[Unit]
Description=Oracle Price **Feeder**
After=network.target

[Service]
User=root
Type=simple
Environment="PRICE_FEEDER_PASS={KEYRING_PASSWORD}"
ExecStart=/root/go/bin/price-feeder {PATH-TO-CONFIG-TOML}
Restart=on-failure
LimitNOFILE=6553500

[Install]
WantedBy=multi-user.target
```

## Providers

The list of current supported providers:

- [Binance](https://www.binance.com/en)
- [MEXC](https://www.mexc.com/)
- [Coinbase](https://www.coinbase.com/)
- [Gate](https://www.gate.io/)
- [Huobi](https://www.huobi.com/en-us/)
- [Kraken](https://www.kraken.com/en-us/)
- [Okx](https://www.okx.com/)

## Usage

The `price-feeder` tool runs off of a single configuration file. This configuration
file defines what exchange rates to fetch and what providers to get them from.
In addition, it defines the oracle's keyring and feeder account information.
The keyring's password is defined via environment variables or user input.
More information on the keyring can be found [here](#keyring)
Please see the [example configuration](./config.example.toml) for more details.

```shell
$ price-feeder /path/to/price_feeder_config.toml
```

## Configuration

### `telemetry`

A set of options for the application's telemetry, which is disabled by default. An in-memory sink is the default, but Prometheus is also supported. We use the [cosmos sdk telemetry package](https://github.com/cosmos/cosmos-sdk/blob/main/docs/core/telemetry.md).

### `deviation`

Deviation allows validators to set a custom amount of standard deviations around the median which is helpful if any providers become faulty. It should be noted that the default for this option is 1 standard deviation.

### `provider_endpoints`

The provider_endpoints option enables validators to setup their own API endpoints for a given provider.

### `server`

The `server` section contains configuration pertaining to the API served by the
`price-feeder` process such the listening address and various HTTP timeouts.

### `currency_pairs`

The `currency_pairs` sections contains one or more exchange rates along with the
providers from which to get market data from. It is important to note that the
providers supplied in each `currency_pairs` must support the given exchange rate.

For example, to get multiple price points on ATOM, you could define `currency_pairs`
as follows:

```toml
[[currency_pairs]]
base = "ATOM"
providers = [
  "binance",
]
quote = "USDT"

[[currency_pairs]]
base = "ATOM"
providers = [
  "kraken",
]
quote = "USD"
```

Providing multiple providers is beneficial in case any provider fails to return
market data. Prices per exchange rate are submitted on-chain via pre-vote and
vote messages using a time-weighted average price (TVWAP).

### `account`

The `account` section contains the oracle's feeder and validator account information.
These are used to sign and populate data in pre-vote and vote oracle messages.

### `keyring`

The `keyring` section contains Keyring related material used to fetch the key pair
associated with the oracle account that signs pre-vote and vote oracle messages.

### `rpc`

The `rpc` section contains the Tendermint and Cosmos application gRPC endpoints.
These endpoints are used to query for on-chain data that pertain to oracle
functionality and for broadcasting signed pre-vote and vote oracle messages.

### `healthchecks`

The `healthchecks` section defines optional healthcheck endpoints to ping on successful
oracle votes. This provides a simple alerting solution which can integrate with a service
like [healthchecks.io](https://healthchecks.io). It's recommended to configure additional
monitoring since third-party services can be unreliable.

## Keyring

Our keyring must be set up to sign transactions before running the price feeder.
Additional info on the different keyring modes is available [here](https://docs.cosmos.network/master/run-node/keyring.html).
**Please note that the `test` and `memory` modes are only for testing purposes.**
**Do not use these modes for running the price feeder against mainnet.**

