import time
import subprocess
import requests
from pathlib import Path
import sys

# for internal loadtest only
CMD_TMPL = "printf '{password}\n' | " + str(Path.home()) + "/go/bin/seid"
PREVOTE_TMPL = (
    " tx oracle aggregate-prevote abc 0uusdc,0uatom {val_addr} --from={key} "
    "--chain-id={chain_id} --fees=2000usei -y --broadcast-mode=sync"
)
VOTE_TMPL = (
    " tx oracle aggregate-vote abc 0uusdc,0uatom {val_addr} --from={key} "
    "--chain-id={chain_id} --fees=2000usei -y --broadcast-mode=sync"
)

KEY = "default_account"
CHAIN_ID = "sei-testnet-2"
PASSWORD = ""
ADDR = ""
VAL_ADDR = ""

def get_current_vote_period():
    res = requests.get("http://localhost:26657/blockchain")
    body = res.json()
    return int(body["result"]["last_height"]) // 10

def vote_for_period():
    print("vote")
    result = subprocess.check_output(
        [CMD_TMPL.format(password=PASSWORD) + VOTE_TMPL.format(key=KEY, chain_id=CHAIN_ID, val_addr=VAL_ADDR)],
        stderr=subprocess.STDOUT,
        shell=True,
    )

def prevote_for_period():
    print("prevote")
    result = subprocess.check_output(
        [CMD_TMPL.format(password=PASSWORD) + PREVOTE_TMPL.format(key=KEY, chain_id=CHAIN_ID, val_addr=VAL_ADDR)],
        stderr=subprocess.STDOUT,
        shell=True,
    )

def vote_loop(interval=1):
    last_prevoted_period = -1
    last_voted_period = -1
    while True:
        time.sleep(interval)
        current_vote_period = get_current_vote_period()
        if last_prevoted_period > last_voted_period and last_prevoted_period < current_vote_period:
            vote_for_period()
            last_voted_period = last_prevoted_period
        elif last_prevoted_period == -1 or (last_voted_period >= last_prevoted_period and last_prevoted_period < current_vote_period):
            prevote_for_period()
            last_prevoted_period = current_vote_period

def main():
    global KEY
    global CHAIN_ID
    global PASSWORD
    global VAL_ADDR
    args = sys.argv[1:]
    KEY = args[0]
    CHAIN_ID = args[1]
    PASSWORD = args[2]
    VAL_ADDR = args[3]

    # fetch validator address if not provided
    if not VAL_ADDR:
        VAL_ADDR = subprocess.check_output(
            [CMD_TMPL.format(password=PASSWORD) + f" keys show {KEY} --bech val | grep address | cut -d':' -f2 | xargs"],
            stderr=subprocess.STDOUT,
            shell=True,
        ).decode()[:-1]
    vote_loop()

if __name__ == "__main__":
    main()