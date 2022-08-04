import requests
import os
from datetime import datetime

def get_block_height_and_timestamp(tx_hash_str):
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
            height = get_block_height_and_timestamp(curr)
            curr = f.readline()[:-1]
            if height is not None:
                seen_heights.add(height)
    return sorted(list(seen_heights))

def get_block_info(height):
    res = requests.get(f"http://localhost:26657/block?height={height}")
    block = res.json()["result"]["block"]
    return {
        "timestamp": datetime.strptime(block["header"]["time"][:26], "%Y-%m-%dT%H:%M:%S.%f"),
        "number_of_txs": len(block["data"]["txs"])
    }

def get_metrics():
    all_heights = get_all_heights()
    print(all_heights)
    if len(all_heights) == 1:
        print("Not enough number of blocks to obtain meaningful metrics with. Exiting..")
        return
    block_info_list = []
    for height in all_heights:
        block_info_list.append(get_block_info(height))
    total_duration = block_info_list[-1]["timestamp"] - block_info_list[0]["timestamp"]
    average_block_time = total_duration.total_seconds() / (len(block_info_list) - 1)
    total_txs_num = sum([block["number_of_txs"] for block in block_info_list])
    average_txs_num = total_txs_num / len(block_info_list)
    return {
        "average_block_time": average_block_time,
        "average_throughput_per_block": average_txs_num / average_block_time,
        "number_of_full_blocks": len(block_info_list),
        "total_txs_num": total_txs_num,
    }

print(get_metrics())
