# Price Oracle
Implementation of a Price Oracle that fetches price data from Coingecko and Binance. This is heavily influenced from Kava Labs' implementation https://github.com/Kava-Labs/kava-tools/blob/master/oracle/

# Setup (Local)
All steps should be run in this directory (`sei-chain/oracle/`)

Install the oracle software
```
yarn
```

Configure a `.env` file. Example:
```
# Cron tab for how frequently prices will be posted (ex: 5 minutes)
CRONTAB="*/5 * * * *"

# List of markets the oracle will post prices for. See pricefeed parameters for the list of active markets.
MARKET_IDS="bnb:usd,bnb:usd:30,btc:usd,btc:usd:30,xrp:usd,xrp:usd:30,busd:usd,busd:usd:30,kava:usd,kava:usd:30,hard:usd,hard:usd:30,usdx:usd"
```

Run the oracle process:
```
node main.js
```

# Setup (Launch ec2 instance)
All the local steps aboved are bundled in an Ansible playbook that will launch, configure and start an Oracle for you in AWS.
Simply run:
```
GIT_USER=$GIT_USER GIT_ACCESS_TOKEN=$GIT_ACCESS_TOKEN ansible-playbook oracle/deploy.yaml
```
