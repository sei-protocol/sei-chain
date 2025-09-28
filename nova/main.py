import time, yaml
from utils import run, get_balance, log, get_address
from validators import select_validator
from telegram import send_alert

# Load configuration
with open("config.yaml", "r", encoding="utf-8") as f:
    config = yaml.safe_load(f)

while True:
    log("🚀 Nova cycle initiated...")

    addr = get_address(config["wallet_name"])
    validator = select_validator(config["validators"])

    # Step 1: Withdraw rewards
    log(f"🔄 Withdrawing rewards from {validator}")
    run(f"seid tx distribution withdraw-rewards {validator} "
        f"--from {config['wallet_name']} --chain-id {config['chain_id']} "
        f"--fees {config['fee']} --gas {config['gas']} "
        f"--node {config['rpc_node']} -y")
    
    time.sleep(12)

    # Step 2: Get balance
    balance = get_balance(addr, config["rpc_node"])
    log(f"💰 Wallet Balance: {balance} usei")

    delegate_amt = balance - config["min_balance_buffer"]
    if delegate_amt <= 0:
        msg = f"❌ Not enough SEI to delegate: {balance} usei"
        log(msg)
        if config["telegram"]["enabled"]:
            send_alert(msg)
    else:
        log(f"📥 Delegating {delegate_amt} usei to {validator}")
        run(f"seid tx staking delegate {validator} {delegate_amt}usei "
            f"--from {config['wallet_name']} --chain-id {config['chain_id']} "
            f"--fees {config['fee']} --gas {config['gas']} "
            f"--node {config['rpc_node']} -y")
        if config["telegram"]["enabled"]:
            send_alert(f"✅ Delegated {delegate_amt} usei to {validator}")

    log(f"😴 Sleeping for {config['sleep_interval']}s...\n")
    time.sleep(config["sleep_interval"])
