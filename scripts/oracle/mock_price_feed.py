import time
import subprocess
import requests
from pathlib import Path
print(Path.home())

CMD = str(Path.home()) + "/go/bin/seid"
PREVOTE_TMPL = (
    "tx oracle aggregate-prevote abc 100uusdc,50uatom {val_addr} --from={key} "
    "--chain-id={chain_id} --fees=100000usei --gas=100000 -y --broadcast-mode=sync"
)
VOTE_TMPL = (
    "tx oracle aggregate-vote abc 100uusdc,50uatom {val_addr} --from={key} "
    "--chain-id={chain_id} --fees=100000usei --gas=100000 -y --broadcast-mode=sync"
)

KEY = "tony"
CHAIN_ID = "sei-chain"
ADDR = ""
VAL_ADDR = ""

def get_current_vote_period():
    res = requests.get("http://localhost:26657/blockchain")
    body = res.json()
    return int(body["result"]["last_height"]) // 10

def vote_for_period():
    print("vote")
    result = subprocess.check_output(
        [CMD, VOTE_TMPL.format(key=KEY, chain_id=CHAIN_ID, val_addr=VAL_ADDR)],
        stderr=subprocess.STDOUT,
        shell=True,
    )
    print(result)

def prevote_for_period():
    print("prevote")
    print(PREVOTE_TMPL.format(key=KEY, chain_id=CHAIN_ID, val_addr=VAL_ADDR))
    print("\n")
    result = subprocess.check_output(
        [CMD, PREVOTE_TMPL.format(key=KEY, chain_id=CHAIN_ID, val_addr=VAL_ADDR)],
        stderr=subprocess.STDOUT,
        shell=True,
    )
    print(result)

def vote_loop(interval=0.1):
    global ADDR
    global VAL_ADDR

    ADDR = subprocess.check_output([CMD + f" keys show {KEY} -a"], stderr=subprocess.STDOUT,
        shell=True,).decode()[:-1]
    print(CMD + f" query staking delegations {ADDR} | grep validator_address | cut -d':' -f2 | xargs")
    VAL_ADDR = subprocess.check_output(
        [CMD + f" query staking delegations {ADDR} | grep validator_address | cut -d':' -f2 | xargs"],
        stderr=subprocess.STDOUT,
        shell=True,
    ).decode()
    print(VAL_ADDR)
    last_prevoted_period = -1
    while True:
        time.sleep(interval)
        current_vote_period = get_current_vote_period()
        if last_prevoted_period == -1:
            prevote_for_period()
            last_prevoted_period = get_current_vote_period()
            continue
        elif last_prevoted_period < current_vote_period:
            vote_for_period()
            prevote_for_period()
            last_prevoted_period = get_current_vote_period()

vote_loop()