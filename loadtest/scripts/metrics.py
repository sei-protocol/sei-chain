import json
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
    block = res.json()["block"]
    return {
        "timestamp": datetime.strptime(block["header"]["time"][:26], "%Y-%m-%dT%H:%M:%S.%f"),
        "number_of_txs": len(block["data"]["txs"])
    }

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
        }
    }

print(json.dumps(get_metrics(), indent=4))
