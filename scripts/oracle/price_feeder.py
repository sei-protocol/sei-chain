import time
import subprocess
import requests
from pathlib import Path
import sys
from price_fetcher import PriceFetcher

HOME_PATH = str(Path.home())
CMD = "printf '{password}\n' | {home_path}/go/bin/seid"
PREVOTE_TMPL = (
    " tx oracle aggregate-prevote abc 100uusdc,50uatom {val_addr} --from={key} "
    "--chain-id={chain_id} --fees={fees}usei --gas={gas} -y --broadcast-mode=sync"
)
VOTE_TMPL = (
    " tx oracle aggregate-vote abc 100uusdc,50uatom {val_addr} --from={key} "
    "--chain-id={chain_id} --fees={fees}usei --gas={gas} -y --broadcast-mode=sync"
)
COMBINED_TMPL = (
    " tx oracle aggregate-combined-vote {salt} {prevote_prices} {salt} {vote_prices} {val_addr} --from {key} "
    "--chain-id={chain_id} --fees={fees}usei --gas={gas} -y --broadcast-mode=sync"
)

class PriceFeeder:
    def __init__(self, key="admin", password="12345678", chain_id="sei-chain", port="26657", fees="100000", gas="100000") -> None:
        self.key = key
        self.password = password
        self.chain_id = chain_id
        self.port = port
        self.fees = fees
        self.gas = gas
        self.addr = ""
        self.val_addr = ""
        self.init_node_info()

    def init_node_info(self):
        self.addr = subprocess.check_output(
            [CMD.format(password=self.password, home_path=HOME_PATH) + f" keys show {self.key} -a"], 
            stderr=subprocess.STDOUT,
            shell=True,
        ).decode()[:-1]

        self.val_addr = subprocess.check_output(
            [CMD.format(password=self.password, home_path=HOME_PATH) + f" query staking delegations {self.addr} | grep validator_address | cut -d':' -f2 | xargs"],
            stderr=subprocess.STDOUT,
            shell=True,
        ).decode()[:-1]

    def get_current_vote_period(self):
        res = requests.get("http://localhost:{port_num}/blockchain".format(port_num=self.port))
        body = res.json()
        return int(body["result"]["last_height"]) // 10

    def vote_for_period(self):
        print("vote")
        result = subprocess.check_output(
            [
                CMD.format(password=self.password, home_path=HOME_PATH) + 
                VOTE_TMPL.format(
                    key=self.key, 
                    chain_id=self.chain_id, 
                    val_addr=self.val_addr, 
                    gas=self.gas, 
                    fees=self.fees, 
                )
            ],
            stderr=subprocess.STDOUT,
            shell=True,
        )

    def prevote_for_period(self):
        print("prevote")
        result = subprocess.check_output(
            [
                CMD.format(password=self.password, home_path=HOME_PATH) + 
                PREVOTE_TMPL.format(
                    key=self.key, 
                    chain_id=self.chain_id, 
                    val_addr=self.val_addr, 
                    gas=self.gas, 
                    fees=self.fees, 
                )
            ],
            stderr=subprocess.STDOUT,
            shell=True,
        )

    def combined_vote_for_period(self, vote_prices, prevote_prices, salt):
        result = subprocess.check_output(
            [
                CMD.format(password=self.password, home_path=HOME_PATH) + 
                COMBINED_TMPL.format(
                    key=self.key, 
                    chain_id=self.chain_id, 
                    val_addr=self.val_addr, 
                    prevote_prices=prevote_prices, 
                    vote_prices=vote_prices,
                    gas=self.gas, 
                    fees=self.fees,
                    salt=salt 
                )
            ],
            stderr=subprocess.STDOUT,
            shell=True,
        )

    def vote_loop(self, coins, salt, interval=5):
        last_prevoted_period = -1
        last_voted_period = -1
        last_combined_voted_period = -1
        pf = PriceFetcher()

        vote_prices = pf.create_price_feed(coins)
        while True:
            time.sleep(interval)
            print("submitting price feeds ", vote_prices)
            current_vote_period = self.get_current_vote_period()
            if last_combined_voted_period < current_vote_period:
                prevote_prices = pf.create_price_feed(coins)
                self.combined_vote_for_period(vote_prices, prevote_prices, salt)
                vote_prices = prevote_prices
                last_combined_voted_period = current_vote_period

def main():
    args = sys.argv[1:]
    salt = "abc"

    if len(args) < 4:
        print("You need to specify your chain-id, key name, password and the list of coins to send an oracle price feed.")
        return
    elif len(args) == 4:
        pf = PriceFeeder(args[0], args[1], args[2])
    elif len(args) > 4 and len(args) < 8:
        print("You need to specify entire list of custom parameters to send an oracle price feed.")
        return
    else:
        pf = PriceFeeder(args[0], args[1], args[2], args[4], args[5], args[6])
        salt = args[7]

    coins = args[3].split(',')
    pf.vote_loop(coins, salt)

if __name__ == "__main__":
    main()