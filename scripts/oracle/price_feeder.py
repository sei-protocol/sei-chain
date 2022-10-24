#!/usr/bin/python3
import argparse
import time
import subprocess
import requests
import re
from pathlib import Path
import sys
from price_fetcher import PriceFetcher

CMD = "printf '{password}\n' | {binary}"
VOTE_TMPL = (
    " tx oracle aggregate-vote {vote_prices} {val_addr} --from {key} "
    "--chain-id={chain_id} -y --broadcast-mode=sync --node={node}"
)

class PriceFeeder:
    def __init__(self, key, password, binary, chain_id, node, valoper, api_key, vote_period) -> None:
        self.key = key
        self.password = password
        self.binary = binary
        self.chain_id = chain_id
        self.node = node
        if valoper is None:
            self.val_addr = ""
            self.init_val_addr()
        else:
            self.val_addr = valoper
        self.api_key = api_key
        self.vote_period = vote_period

    def init_val_addr(self):
        self.val_addr = subprocess.check_output(
            [CMD.format(password=self.password, binary=self.binary) + f" keys show {self.key} --bech=val | grep address | cut -d':' -f2 | xargs"],
            stderr=subprocess.STDOUT,
            shell=True,
        ).decode()[:-1]
        print("validator addr is:", self.val_addr)

    def get_current_vote_period(self):
        res = requests.get("{node}/blockchain".format(node=self.node))
        body = res.json()
        return int(body["result"]["last_height"]) // self.vote_period

    def vote_for_period(self, vote_prices):
        result = subprocess.check_output(
            [
                CMD.format(password=self.password, binary=self.binary) +
                VOTE_TMPL.format(
                    key=self.key,
                    chain_id=self.chain_id,
                    val_addr=self.val_addr,
                    vote_prices=vote_prices,
                    node=self.node
                )
            ],
            stderr=subprocess.STDOUT,
            shell=True,
        )

        if re.search("code: \d{1,2}", result.decode("utf-8")).group(0) != "code: 0":
            print("Err: ", result)
            print("Oracle price didn't submit successfully!!")

    def vote_loop(self, coins, interval=0.2):
        last_voted_period = -1
        vote_loop_break = 0
        pf = PriceFetcher(self.api_key)

        while True:
            time.sleep(interval)
            current_vote_period = self.get_current_vote_period()
            if last_voted_period < current_vote_period:
                vote_prices = pf.create_price_feed(coins)
                if vote_prices is None:
                    print ("No price data available, sleep 5")
                    time.sleep(5)
                    continue

                print("submitting price feed ", vote_prices)
                self.vote_for_period(vote_prices)
                last_voted_period = current_vote_period
                vote_loop_break += 1

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("key", help='Your wallet (key) name', type=str)
    parser.add_argument("password", help='The keychain password', type=str)
    parser.add_argument('chain_id', help='Chain id', type=str)
    parser.add_argument('coins', help='The coins to use', type=str)
    parser.add_argument('--binary', help='Your seid binary path', type=str, default=str(Path.home()) + '/go/bin/seid')
    parser.add_argument('--node', help='The node to contact', type=str, default='http://localhost:26657')
    parser.add_argument('--interval', help='How long time to sleep between price checks', type=int, default=5)
    parser.add_argument('--vote-period', help='how many blocks is the vote period', type=int, default=10)
    parser.add_argument('--valoper', help='Validator address if using separate feeder account', type=str)
    parser.add_argument('--api-key', help='API Key for price fetcher', type=str)
    args=parser.parse_args()

    pf = PriceFeeder(args.key, args.password, args.binary, args.chain_id, args.node, args.valoper, args.api_key, args.vote_period)

    coins = args.coins.split(',')
    pf.vote_loop(coins, args.interval)

if __name__ == "__main__":
    main()
