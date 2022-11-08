# Oracle Price Feeder Dockerfile

## Build Docker Image
Change `VERSION` to the release you want to build.

```bash
VERSION=master
git clone https://github.com/Team-Kujira/oracle-price-feeder.git
cd oracle-price-feeder/dockerfile || exit
docker build --build-arg VERSION=$VERSION -t price-feeder:latest .
```

## Create `config.toml`
Edit your `address`, `validator`, `grpc_endpoint`, `tmrpc_endpoint` you may need to modifify your firewall to allow this container to reach your chain-node. See [offical docs](https://docs.kujira.app/validators/run-a-node/oracle-price-feeder) for more details.

```bash
sudo tee config.toml <<EOF
gas_adjustment = 1.5
gas_prices = "0.00125ukuji"
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
address = "kujira..."
chain_id = "kaiyo-1"
validator = "kujiravaloper..."
prefix = "kujira"

[keyring]
backend = "file"
dir = "/root/.kujira"

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
providers = [
  "binance",
  "kraken",
  "osmosis",
]
quote = "USD"
EOF
```

## Create `client.toml`
change node to your favorite `rpc` node

```bash
sudo tee client.toml <<EOF
chain-id = "kaiyo-1"
keyring-backend = "file"
output = "text"
node = "tcp://rpc-kujira.synergynodes.com:80"
broadcast-mode = "sync"
EOF
```

## Recover oracle `keyring-file` to local file
```bash
kujirad keys add oracle --keyring-backend file --recover
```
In the kujira home directory (~/.kujira/) you should see the `keyring-file` folder.  This will be mounted as a volume when running the docker container.

## Run Docker Image
```bash
docker run \
--env PRICE_FEEDER_PASS=password \
-v ~/.kujira/keyring-file:/root/.kujira/keyring-file \
-v "$PWD"/config.toml:/root/price-feeder/config.toml \
-v "$PWD"/client.toml:/root/.kujira/config/client.toml \
-it price-feeder /root/price-feeder/config.toml
```
