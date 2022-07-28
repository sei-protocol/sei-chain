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
PREVOTE_TMPL = (
    " tx oracle aggregate-prevote abc 100uusdc,50uatom {val_addr} --from={key} "
    "--chain-id={chain_id} --fees={fees}usei --gas={gas} -y --broadcast-mode=sync --node={node}"
)
VOTE_TMPL = (
    " tx oracle aggregate-vote abc 100uusdc,50uatom {val_addr} --from={key} "
    "--chain-id={chain_id} --fees={fees}usei --gas={gas} -y --broadcast-mode=sync --node={node}"
)
COMBINED_TMPL = (
    " tx oracle aggregate-combined-vote {salt} {prevote_prices} {salt} {vote_prices} {val_addr} --from {key} "
    "--chain-id={chain_id} --fees={fees}usei --gas={gas} -y --broadcast-mode=sync --node={node}"
)

class PriceFeeder:
    def __init__(self, key, password, binary, chain_id, node, fees, gas) -> None:
        self.key = key
        self.password = password
        self.binary = binary
        self.chain_id = chain_id
        self.node = node
        self.fees = fees
        self.gas = gas
        self.val_addr = ""
        self.init_val_addr()

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
        return int(body["result"]["last_height"]) // 10

    def vote_for_period(self):
        print("Vote")
        result = subprocess.check_output(
            [
                CMD.format(password=self.password, binary=self.binary) + 
                VOTE_TMPL.format(
                    key=self.key, 
                    chain_id=self.chain_id, 
                    val_addr=self.val_addr, 
                    gas=self.gas, 
                    fees=self.fees, 
                    node=self.node
                )
            ],
            stderr=subprocess.STDOUT,
            shell=True,
        )

    def prevote_for_period(self):
        print("Prevote")
        result = subprocess.check_output(
            [
                CMD.format(password=self.password, binary=self.binary) + 
                PREVOTE_TMPL.format(
                    key=self.key, 
                    chain_id=self.chain_id, 
                    val_addr=self.val_addr, 
                    gas=self.gas, 
                    fees=self.fees,
                    node=self.node
                )
            ],
            stderr=subprocess.STDOUT,
            shell=True,
        )

    def combined_vote_for_period(self, vote_prices, prevote_prices, salt):
        result = subprocess.check_output(
            [
                CMD.format(password=self.password, binary=self.binary) + 
                COMBINED_TMPL.format(
                    key=self.key, 
                    chain_id=self.chain_id, 
                    val_addr=self.val_addr, 
                    prevote_prices=prevote_prices, 
                    vote_prices=vote_prices,
                    gas=self.gas, 
                    fees=self.fees,
                    salt=salt,
                    node=self.node
                )
            ],
            stderr=subprocess.STDOUT,
            shell=True,
        )

        if re.search("code: \d{1,2}", result.decode("utf-8")).group(0) != "code: 0":
            print("Err: ", result)
            print("Oracle price didn't submit successfully!!")

    def vote_loop(self, coins, salt, interval=0.2):
        last_prevoted_period = -1
        last_voted_period = -1
        last_combined_voted_period = -1
        vote_loop_break = 0
        pf = PriceFetcher()

        vote_prices = pf.create_price_feed(coins)
        while True:
            time.sleep(interval)
            current_vote_period = self.get_current_vote_period()
            if last_combined_voted_period < current_vote_period:
                prevote_prices = pf.create_price_feed(coins)
                print("submitting price feeds ", vote_prices, prevote_prices)
                self.combined_vote_for_period(vote_prices, prevote_prices, salt)
                vote_prices = prevote_prices
                last_combined_voted_period = current_vote_period
                vote_loop_break += 1

            # sleep for 3s between every pairs of successful combined-votes to not be throttled by price API
            if vote_loop_break > 1:
                print("sleep for 3...")
                time.sleep(3)
                vote_loop_break = 0

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("key", help='Your wallet (key) name', type=str)
    parser.add_argument("password", help='The keychain password', type=str)
    parser.add_argument('chain_id', help='Chain id', type=str)
    parser.add_argument('coins', help='The coins to use', type=str)
    parser.add_argument('--binary', help='Your seid binary path', type=str, default=str(Path.home()) + '/go/bin/seid')
    parser.add_argument('--node', help='The node to contact', type=str, default='http://localhost:26657')
    parser.add_argument('--fees', help='The fees to use', type=int, default=100000)
    parser.add_argument('--gas', help='The gas to use', type=int, default=100000)
    parser.add_argument('--salt', help='The salt to use', type=str, default='abc')
    parser.add_argument('--interval', help='How long time to sleep between price checks', type=int, default=5)
    args=parser.parse_args()

    pf = PriceFeeder(args.key, args.password, args.binary, args.chain_id, args.node, args.fees, args.gas)

    coins = args.coins.split(',')
    pf.vote_loop(coins, args.salt, args.interval)

if __name__ == "__main__":
    main()