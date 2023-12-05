import base64
import json
import requests
import os
from datetime import datetime, timedelta
import argparse

DATE_TIME_FMT = "%Y-%m-%dT%H:%M:%S.%f"
DEX_MSGS = ["MsgPlaceOrder"]
parser = argparse.ArgumentParser()
parser.add_argument("--node", default="localhost", help="Node base URL")
args = parser.parse_args()
node_base_url = args.node

def get_block_height(tx_hash_str):
    res = requests.get(f"http://{node_base_url}:1317/txs/{tx_hash_str}")
    body = res.json()
    if "height" not in body:
        return
    height = int(body["height"])
    return height

def get_all_heights():
    seen_heights = set([])
    home_path = os.path.expanduser('~')
    filename = f"{home_path}/outputs/test_tx_hash"
    with open(filename, "r") as f:
        curr = f.readline()[:-1]
        while curr:
            height = get_block_height(curr)
            curr = f.readline()[:-1]
            if height is not None:
                seen_heights.add(height)
    return sorted(list(seen_heights))

def get_block_info(height):
    res = requests.get(f"http://{node_base_url}:26657/block?height={height}")
    #timestamp: "2023-02-27T23:00:44.214Z"
    block = res.json()["block"]
    return {
        "height": height,
        "timestamp": datetime.strptime(block["header"]["time"][:26], DATE_TIME_FMT),
        "number_of_txs": len(block["data"]["txs"])
    }

def get_block_time(height):
    return get_block_info(height)["timestamp"]

"""
This code is quite brittle as it handles different message types differently, and if we ever change the names of
the proto or want to test more modules, we'll need to modify this. However, it works for now.
"""
def get_transaction_breakdown(height):
    res = requests.get(f"http://{node_base_url}:26657/block?height={height}")
    output = res.json()["block"]["data"]["txs"]
    tx_mapping = {}
    for tx in output:
        module = None
        b64_decoded = str(base64.b64decode(tx))
        if "MsgSend" in b64_decoded:
            module = "bank"
        elif "MsgAggregateExchangeRateVote" in b64_decoded:
            module = "oracle"
        elif "MsgDelegate" in b64_decoded:
            module = "staking"
        elif "MsgCreateDenom" in b64_decoded:
            module = "tokenfactory"
        else:
            # Dex orders
            for dex_msg in DEX_MSGS:
                if dex_msg in b64_decoded:
                    module = "dex"
                    break


        # Attributes may not be defined for custom module
        if module == None:
            module = "other"
        if module not in tx_mapping:
            tx_mapping[module] = 1
        else:
            tx_mapping[module] += 1
    return tx_mapping

def get_best_block_stats(block_info_list):
    max_throughput, max_block_height, max_block_time = -1, -1, -1
    for i in range(len(block_info_list)):
        block = block_info_list[i]
        next_block_time = get_block_time(block["height"] + 1)
        block_time = (next_block_time - block["timestamp"]) // timedelta(milliseconds=1)
        throughput = block["number_of_txs"] * 1000 / block_time
        print(f"Block {block['height']} has throughput {throughput} and block time {block_time} ms")
        if throughput > max_throughput:
            max_throughput = throughput
            max_block_height = block["height"]
            max_block_time = block_time
    return max_throughput, max_block_height, max_block_time
def get_metrics():
    all_heights = get_all_heights()
    if len(all_heights) <= 2:
        print("Not enough number of blocks to obtain meaningful metrics with. Exiting..")
        return
    block_info_list = []
    for height in all_heights:
        block_info_list.append(get_block_info(height))
    # Skip first and last block since it may have high deviation if we start it at the end of the block

    skip_edge_blocks = block_info_list[1:-1]
    total_duration = 0
    for i in range(len(skip_edge_blocks)):
        block = skip_edge_blocks[i]
        next_block_time = get_block_time(block["height"] + 1)
        block_time = (next_block_time - block["timestamp"]) // timedelta(milliseconds=1)
        total_duration += block_time
    average_block_time = total_duration / 1000 / len(skip_edge_blocks)
    total_txs_num = sum([block["number_of_txs"] for block in skip_edge_blocks])
    average_txs_num = total_txs_num / len(skip_edge_blocks)

    # Best block stats:
    max_throughput, max_block_height, max_block_time = get_best_block_stats(block_info_list)

    tx_mapping = get_transaction_breakdown(max_block_height)

    return {
        "Summary (excl. edge block)": {
            "average_block_time": average_block_time,
            "average_throughput_per_block": average_txs_num,
            "average_throughput_per_sec": average_txs_num / average_block_time,
            "number_of_full_blocks": len(skip_edge_blocks),
            "full_blocks": all_heights[1:-1],
            "total_txs_num": total_txs_num,
        },
        "Detail (incl. edge blocks)": {
            "blocks": all_heights,
            "txs_per_block": [block["number_of_txs"] for block in block_info_list]
        },
        "Best block": {
            "height": max_block_height,
            "tps": max_throughput,
            "tx_mapping": tx_mapping,
            "block_time_ms": max_block_time
        }
    }

print(json.dumps(get_metrics(), indent=4))
