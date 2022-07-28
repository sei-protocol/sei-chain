# Price Oracle Script
This is a simple oracle script that fetchs market prices of different token pairs from the CoinGecko. Sei team will add multiple 
price sources in this script so that Sei can decentralize the oracle prices.

# Setup (Local)
Install the coingecko api on your instance
```
git clone https://github.com/man-c/pycoingecko.git
cd pycoingecko
python3 setup.py install
```

Check the current oracle token pairs whitelist, note that current oracle only accepts whitelisted token prices. Example:
```
seid query oracle params
➜ params:
    lookback_duration: "3600"
    min_valid_per_window: "0.050000000000000000"
    reward_band: "0.020000000000000000"
    slash_fraction: "0.000100000000000000"
    slash_window: "201600"
    vote_period: "10"
    vote_threshold: "0.500000000000000000"
    whitelist:
    - name: uatom
    - name: uusdc
    - name: usei
    - name: ueth
```

Start the price feeder in the background
```
seid tx oracle aggregate-combined-vote abc 10.09uatom,1.0uusdc abc 10.09uatom,1.0uusdc seivaloper1mf9zymr0wk66ueqwgem7mmlfe05dlk0qzfnl5u --from admin --chain-id=sei-chain --fees=100000usei --gas=100000 -y --broadcast-mode=sync
```

After successfully submit the prices, you can check the status of the oracle on-chain by
```
seid query oracle exchange-rates
```
