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
```

Start the price feeder in the background, note that you may want to submit all whitelisted coins' price, otherwise you may not be eligible for the oracle reward. ${coin_list} example: 'cosmos','usd-coin'
```
nohup python3 -u price_feeder.py admin 12345678 sei-chain ${coin_list} &
```

Examine there is no immediate error of the script
```
tail -f nohup.out
```

After successfully submit the prices, you should see the current price feeds from
```
seid query oracle exchange-rates
```

If you want to kill the background oracle script, do
```
kill -9 <PID>
```