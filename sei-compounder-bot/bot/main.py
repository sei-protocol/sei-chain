from __future__ import annotations

import argparse
import sys
import time
from pathlib import Path
from typing import Any, Dict

from .telegram import send_alert
from .utils import (
    get_active_rpc,
    get_address,
    get_balance,
    load_config,
    run,
    setup_logger,
)
from .validators import select_validator

DEFAULT_CONFIG_PATH = Path(__file__).resolve().parent / "config.yaml"


def compound_once(config: Dict[str, Any]) -> None:
    logger = setup_logger(
        config.get("logging", {}).get("level", "INFO"),
        int(config.get("logging", {}).get("max_bytes", 1_048_576)),
        int(config.get("logging", {}).get("backup_count", 5)),
    )

    rpc = get_active_rpc(config, logger=logger)
    if not rpc:
        message = "❌ Unable to determine healthy RPC endpoint"
        logger.error(message)
        send_alert(message, config, logger=logger)
        return

    wallet_name = config.get("wallet_name")
    if not wallet_name:
        logger.error("Wallet name missing from configuration")
        return

    for key in ("chain_id", "fee", "gas"):
        if key not in config:
            logger.error("Required configuration value '%s' missing", key)
            return

    logger.info("🚀 Starting new compound cycle using RPC %s", rpc)

    address = get_address(wallet_name, logger=logger)
    if not address:
        message = f"❌ Unable to resolve address for wallet '{wallet_name}'"
        logger.error(message)
        send_alert(message, config, logger=logger)
        return

    logger.info("Detected wallet address: %s", address)

    try:
        validator = select_validator(config)
    except ValueError as exc:
        logger.error("Validator selection failed: %s", exc)
        return

    logger.info("Selected validator: %s", validator)

    withdraw_cmd = (
        f"seid tx distribution withdraw-rewards {validator} "
        f"--from {wallet_name} --chain-id {config['chain_id']} "
        f"--fees {config['fee']} --gas {config['gas']} --node {rpc} -y"
    )
    withdraw_output = run(withdraw_cmd, logger=logger)
    if withdraw_output is None:
        message = f"❌ Withdraw rewards transaction failed for {validator}"
        logger.error(message)
        send_alert(message, config, logger=logger)
        return

    logger.info("Withdraw transaction submitted: %s", withdraw_output)

    tx_wait = int(config.get("tx_wait_seconds", 12))
    logger.debug("Waiting %s seconds for transaction confirmation", tx_wait)
    time.sleep(tx_wait)

    balance = get_balance(address, rpc, logger=logger)
    if balance is None:
        message = "❌ Failed to fetch wallet balance"
        logger.error(message)
        send_alert(message, config, logger=logger)
        return

    logger.info("Current balance: %s usei", balance)

    min_buffer = int(config.get("min_balance_buffer", 0))
    delegate_amt = balance - min_buffer

    if delegate_amt <= 0:
        message = f"⚠️ Insufficient balance to delegate. Balance: {balance} usei"
        logger.warning(message)
        send_alert(message, config, logger=logger)
        return

    delegate_cmd = (
        f"seid tx staking delegate {validator} {delegate_amt}usei "
        f"--from {wallet_name} --chain-id {config['chain_id']} "
        f"--fees {config['fee']} --gas {config['gas']} --node {rpc} -y"
    )
    delegate_output = run(delegate_cmd, logger=logger)
    if delegate_output is None:
        message = f"❌ Delegate transaction failed for {delegate_amt} usei"
        logger.error(message)
        send_alert(message, config, logger=logger)
        return

    logger.info("Delegate transaction submitted: %s", delegate_output)
    success_message = f"✅ Delegated {delegate_amt} usei to {validator}"
    send_alert(success_message, config, logger=logger)


def run_forever(config_path: Path, once: bool = False) -> None:
    while True:
        config = load_config(config_path)
        sleep_seconds = int(config.get("sleep_interval", 3600))
        try:
            compound_once(config)
        except Exception as exc:  # pragma: no cover - safety net
            logger = setup_logger(
                config.get("logging", {}).get("level", "INFO"),
                int(config.get("logging", {}).get("max_bytes", 1_048_576)),
                int(config.get("logging", {}).get("backup_count", 5)),
            )
            logger.exception("Unexpected exception during compound cycle: %s", exc)
            send_alert(f"❌ Unexpected compounder error: {exc}", config, logger=logger)
        if once:
            break
        time.sleep(sleep_seconds)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="SEI auto compounder bot")
    parser.add_argument(
        "--config",
        type=Path,
        default=DEFAULT_CONFIG_PATH,
        help="Path to YAML configuration file",
    )
    parser.add_argument(
        "--once",
        action="store_true",
        help="Execute a single compounding cycle and exit",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    config_path = args.config
    if not config_path.exists():
        print(f"Configuration file not found: {config_path}", file=sys.stderr)
        return 1
    run_forever(config_path, once=args.once)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
