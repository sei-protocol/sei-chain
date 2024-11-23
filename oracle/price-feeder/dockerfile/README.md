# Oracle Price Feeder Dockerfile

## Build Docker Image
Change `VERSION` to the release you want to build.

```bash
VERSION=main
git clone https://github.com/sei-protocol/sei-chain.git
cd oracle/price-feeder/dockerfile || exit
docker build --build-arg VERSION=$VERSION -t price-feeder:latest .
```

## Create `config.toml`
Edit your `address`, `validator`, `grpc_endpoint`, `tmrpc_endpoint` you may need to modifify your firewall to allow this container to reach your chain-node. See [official docs](https://docs.kujira.app/validators/run-a-node/oracle-price-feeder) for more details.

```bash
sudo tee config.toml <<EOF
gas_adjustment = 1.5
gas_prices = "0.00125usei"
enable_server = true
enable_voter = true
provider_timeout = "500ms"

[server]
listen_addr = "0.0.0.0:7171"
read_timeout = "20s"
verbose_cors = true
write_timeout = "20s"

[[deviation_thresholds]]
base = "USDT"
threshold = "2"

[account]
address = "sei..."
chain_id = "sei-chain"
validator = "seivaloper..."
prefix = "sei"

[keyring]
backend = "file"
dir = "/root/.sei"

[rpc]
grpc_endpoint = "localhost:9090"
rpc_timeout = "100ms"
tmrpc_endpoint = "http://localhost:26657"

[telemetry]
enable_hostname = true
enable_hostname_label = true
enable_service_label = true
enabled = true
global_labels = [["chain_id", "kaiyo-1"]]
service_name = "price-feeder"
type = "prometheus"
prometheus_retention = 120

[[provider_endpoints]]
name = "binance"
rest = "https://api1.binance.com"
websocket = "stream.binance.com:9443"

[[currency_pairs]]
base = "ATOM"
chain_denom = "uatom"
providers = [
  "binance",
  "kraken",
  "coinbase",
]
quote = "USD"
EOF
```

## Create `client.toml`
change node to your favorite `rpc` node

```bash
sudo tee client.toml <<EOF
chain-id = "sei-chain"
keyring-backend = "file"
output = "text"
node = "tcp://localhost:26657"
broadcast-mode = "sync"
EOF
```

## Recover oracle `keyring-file` to local file
```bash
seid keys add oracle --keyring-backend file --recover
```
In the sei home directory (~/.sei/) you should see the `keyring-file` folder.  This will be mounted as a volume when running the docker container.

## Run Docker Image
```bash
docker run \
--env PRICE_FEEDER_PASS=password \
-v ~/.sei/keyring-file:/root/.sei/keyring-file \
-v "$PWD"/config.toml:/root/price-feeder/config.toml \
-v "$PWD"/client.toml:/root/.sei/config/client.toml \
-it price-feeder /root/price-feeder/config.toml
```
