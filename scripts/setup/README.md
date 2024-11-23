# Sei Network Setup Script

Setup script for decentralized launches of Sei networks.

## setup-validator

run this if you're trying to join an existing network and just need to provision the validator node. Once provisioned, you'll need to request/get Sei tokens in order to stake as a validator

## prepare-genesis

run this if you're trying to launch a new network. This will generate the genesis file and the gentx file. You will need to distribute the genesis file to all the other nodes and the gentx file to the validator node.

## setup-price-feeder

run this if you're trying to setup a price feeder service on a validator node post genesis. Then you'll need to transfer sei tokens to the feeder address wallet in order for oracle to work properly

## Running Services

For seid and price-feeder processes, it's recommended to run as a systemd service.

seid

```
[Unit]
Description=Sei Node
After=network.target

[Service]
User=root
Type=simple
ExecStart=/root/go/bin/seid start --chain-id ${CHAIN_ID}
Restart=always
LimitNOFILE=6553500

[Install]
WantedBy=multi-user.target
```

price-feeder

```
[Unit]
Description=Oracle Price Feeder
After=network.target

[Service]
User=root
Type=simple
Environment="PRICE_FEEDER_PASS={KEYRING_PASSWORD}"
ExecStart=/root/go/bin/price-feeder {PATH-TO-CONFIG-TOML}
Restart=always
LimitNOFILE=6553500

[Install]
WantedBy=multi-user.target
```
