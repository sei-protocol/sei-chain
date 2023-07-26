# Sei

![Banner!](assets/SeiLogo.png)

Sei is a general purpose, open-source L1 blockchain offering the best infrastructure for the exchange of digital assets. The chain emphasizes reliability, security and high throughput above all else, enabling an entirely new echelon of ultra-high performance DeFi products built on top. Sei's on-chain CLOB and matching engine provides deep liquidity and price-time-priority matching for traders and apps. Apps built on Sei benefit from built-in orderbook infrastructure, deep liquidity, and a fully decentralized matching service. Users benefit from this exchange model with the ability to select price, size, and direction of their trades coupled with MEV protection.

# Sei
**Sei** is a blockchain built using Cosmos SDK and Tendermint. It is built using the Cosmos SDK and Tendermint core, and features a built-in central limit orderbook (CLOB) module. Decentralized applications building on Sei can build on top of the CLOB, and other Cosmos-based blockchains can leverage Sei's CLOB as a shared liquidity hub and create markets for any asset.

Designed with developers and users in mind, Sei serves as the infrastructure and shared liquidity hub for the next generation of DeFi. Apps can easily plug-and-play to trade on Sei orderbook infrastructure and access pooled liquidity from other apps. To prioritize developer experience, Sei Network has integrated the wasmd module to support CosmWasm smart contracts.

# Documentation
For the most up to date documentation please visit https://www.sei.io/

# Central Limit Orderbook
Most financial applications in traditional finance make use of CLOBs to create markets. This works well if you have cheap transaction fees and large amounts of liquidity. In decentralized finance however, the automated market-maker (AMM) model is more popular because it doesn't require constantly updating orders and works with lower amounts of liquidity. 

Sei offers cheap transaction fees and works with market makers to have large amounts of liquidity. As a result, it can offer the orderbook based trading experience in a decentralized, permissionless manner. This unlocks many use cases that previously didn't work with the AMM model. 

# Sei Ecosystem
Sei Network is an L1 blockchain with a built-in on-chain orderbook that allows smart contracts easy access to shared liquidity. Sei architecture enables composable apps that maintain modularity.

Sei Network serves as the matching core of the ecosystem, offering superior reliability and ultra-high transaction speed to ecosystem partners, each with their own functionality and user experience. Anyone can create a DeFi application that leverages Sei's liquidity and the entire ecosystem benefits.

Developers, traders, and users can all connect to Sei as ecosystem partners benefiting from shared liquidity and decentralized financial primitives.

# Testnet
## Get started
**How to validate on the Sei Testnet**
*This is the Sei Testnet-1 (sei-testnet-1)*

> Genesis [Published](https://github.com/sei-protocol/testnet/blob/main/sei-testnet-1/genesis.json)

> Peers [Published](https://github.com/sei-protocol/testnet/blob/main/sei-testnet-1/addrbook.json)

## Hardware Requirements
**Minimum**
* 64 GB RAM
* 1 TB NVME SSD
* 16 Cores (modern CPU's)

## Operating System 

> Linux (x86_64) or Linux (amd64) Recommended Arch Linux

**Dependencies**
> Prerequisite: go1.18+ required.
* Arch Linux: `pacman -S go`
* Ubuntu: `sudo snap install go --classic`

> Prerequisite: git. 
* Arch Linux: `pacman -S git`
* Ubuntu: `sudo apt-get install git`

> Optional requirement: GNU make. 
* Arch Linux: `pacman -S make`
* Ubuntu: `sudo apt-get install make`

## Seid Installation Steps

**Clone git repository**

```bash
git clone https://github.com/sei-protocol/sei-chain
cd sei-chain
git checkout $VERSION
make install
```
**Generate keys**

* `seid keys add [key_name]`

* `seid keys add [key_name] --recover` to regenerate keys with your mnemonic

* `seid keys add [key_name] --ledger` to generate keys with ledger device

## Validator setup instructions

* Install seid binary

* Initialize node: `seid init <moniker> --chain-id sei-testnet-1`

* Download the Genesis file: `wget https://github.com/sei-protocol/testnet/raw/main/sei-testnet-1/genesis.json -P $HOME/.sei/config/`
 
* Edit the minimum-gas-prices in ${HOME}/.sei/config/app.toml: `sed -i 's/minimum-gas-prices = ""/minimum-gas-prices = "0.01usei"/g' $HOME/.sei/config/app.toml`

* Start seid by creating a systemd service to run the node in the background
`nano /etc/systemd/system/seid.service`
> Copy and paste the following text into your service file. Be sure to edit as you see fit.

```bash
[Unit]
Description=Sei-Network Node
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/
ExecStart=/root/go/bin/seid start
Restart=on-failure
StartLimitInterval=0
RestartSec=3
LimitNOFILE=65535
LimitMEMLOCK=209715200

[Install]
WantedBy=multi-user.target
```
## Start the node

**Start seid on Linux**

* Reload the service files: `sudo systemctl daemon-reload` 
* Create the symlinlk: `sudo systemctl enable seid.service` 
* Start the node sudo: `systemctl start seid && journalctl -u seid -f`

**Start a chain on 4 node docker cluster**

* Start local 4 node cluster: `make docker-cluster-start`
* SSH into a docker container: `docker exec -it [container_name] /bin/bash`
* Stop local 4 node cluster: `make docker-cluster-stop`

### Create Validator Transaction
```bash
seid tx staking create-validator \
--from {{KEY_NAME}} \
--chain-id  \
--moniker="<VALIDATOR_NAME>" \
--commission-max-change-rate=0.01 \
--commission-max-rate=1.0 \
--commission-rate=0.05 \
--details="<description>" \
--security-contact="<contact_information>" \
--website="<your_website>" \
--pubkey $(seid tendermint show-validator) \
--min-self-delegation="1" \
--amount <token delegation>usei \
--node localhost:26657
```
# Build with Us!
If you are interested in building with Sei Network: 
Email us at team@seinetwork.io 
DM us on Twitter https://twitter.com/SeiNetwork
