import json
import requests
import os
from datetime import datetime, timedelta

DATE_TIME_FMT = "%Y-%m-%dT%H:%M:%S.%f"
def get_block_height(tx_hash_str):
    res = requests.get(f"http://0.0.0.0:1317/txs/{tx_hash_str}")
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
    res = requests.get(f"http://localhost:26657/block?height={height}")
    #timestamp: "2023-02-27T23:00:44.214Z"
    block = res.json()["block"]
    return {
        "height": height,
        "timestamp": datetime.strptime(block["header"]["time"][:26], DATE_TIME_FMT),
        "number_of_txs": len(block["data"]["txs"])
    }

def get_block_time(height):
    return get_block_info(height)["timestamp"]

def get_transaction_breakdown(height):
    res = requests.get(f"http://localhost:26657/tx_search?query=tx.height%3D{height}&prove=false&page=1&per_page=100000")
    output = res.json()["txs"]
    tx_mapping = {}
    for tx in output:
        module = None
        # Ignore failed txs
        if "code" in tx["tx_result"] and tx["tx_result"]["code"] != 0:
            continue
        if "events" in tx["tx_result"]:
            events = tx["tx_result"]["events"]
            for event in events:
                for attr in event["attributes"]:
                    if attr["key"] == "module":
                        module = attr["value"]
        if module not in tx_mapping:
            tx_mapping[module] = 1
        else:
            tx_mapping[module] += 1
    return tx_mapping

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
    total_duration = skip_edge_blocks[-1]["timestamp"] - skip_edge_blocks[0]["timestamp"]
    average_block_time = total_duration.total_seconds() / (len(skip_edge_blocks) - 1)
    total_txs_num = sum([block["number_of_txs"] for block in skip_edge_blocks])
    average_txs_num = total_txs_num / len(skip_edge_blocks)

    # Best block stats:
    max_tx, max_tx_height, max_tx_height_block_time = -1, -1, -1
    for block in block_info_list:
        if block["number_of_txs"] > max_tx:
            max_tx, max_tx_height, max_tx_height_block_time = block["number_of_txs"], block["height"], block["timestamp"]

  # Block times are set at proposal, so the best estimate is to compare block n with n+1 for block time
    tx_mapping = get_transaction_breakdown(max_tx_height)
    max_tx_height_block_time = (get_block_time(max_tx_height + 1) - max_tx_height_block_time) // timedelta(milliseconds=1)

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
            "height": max_tx_height,
            "num_txs": max_tx,
            "tx_mapping": tx_mapping,
            "block_time_ms": max_tx_height_block_time
        }
    }

print(json.dumps(get_metrics(), indent=4))
