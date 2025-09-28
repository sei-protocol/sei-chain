from __future__ import annotations

import json
import logging
import subprocess
import time
from pathlib import Path
from typing import Any, Dict, Iterable, Optional

from logging.handlers import RotatingFileHandler

BASE_DIR = Path(__file__).resolve().parent
LOG_DIR = BASE_DIR / "logs"
LOG_FILE = LOG_DIR / "compound.log"


def ensure_log_dir() -> None:
    LOG_DIR.mkdir(parents=True, exist_ok=True)


def setup_logger(level: str, max_bytes: int, backup_count: int) -> logging.Logger:
    ensure_log_dir()
    logger = logging.getLogger("sei_compounder")
    logger.setLevel(getattr(logging, level.upper(), logging.INFO))
    if logger.handlers:
        return logger
    formatter = logging.Formatter("%(asctime)s [%(levelname)s] %(message)s")

    file_handler = RotatingFileHandler(LOG_FILE, maxBytes=max_bytes, backupCount=backup_count)
    file_handler.setFormatter(formatter)

    stream_handler = logging.StreamHandler()
    stream_handler.setFormatter(formatter)

    logger.addHandler(file_handler)
    logger.addHandler(stream_handler)
    logger.propagate = False

    return logger


def run(
    cmd: str,
    *,
    logger: Optional[logging.Logger] = None,
    retries: int = 3,
    retry_delay: int = 5,
) -> Optional[str]:
    """Run a shell command with retries.

    Args:
        cmd: The command to run.
        logger: Optional logger for status messages.
        retries: Number of attempts before giving up.
        retry_delay: Seconds to wait between attempts.

    Returns:
        The command output if successful, otherwise ``None``.
    """

    attempt = 0
    while attempt < retries:
        attempt += 1
        try:
            if logger:
                logger.debug("Running command (attempt %s/%s): %s", attempt, retries, cmd)
            output = subprocess.check_output(cmd, shell=True, stderr=subprocess.STDOUT)
            return output.decode().strip()
        except subprocess.CalledProcessError as exc:
            if logger:
                logger.error("Command failed (attempt %s/%s): %s\n%s", attempt, retries, cmd, exc.output.decode())
            if attempt >= retries:
                break
            time.sleep(retry_delay)
    return None


def get_address(wallet: str, *, logger: Optional[logging.Logger] = None) -> Optional[str]:
    cmd = f"seid keys show {wallet} -a"
    return run(cmd, logger=logger)


def get_balance(address: str, rpc: str, *, logger: Optional[logging.Logger] = None) -> Optional[int]:
    cmd = f"seid query bank balances {address} --node {rpc} --output json"
    output = run(cmd, logger=logger)
    if not output:
        return None
    try:
        data = json.loads(output)
    except json.JSONDecodeError:
        if logger:
            logger.error("Unable to parse balance response: %s", output)
        return None
    balances = data.get("balances", [])
    if not balances:
        return 0
    # Default to usei if multiple denoms exist.
    for entry in balances:
        if entry.get("denom") == "usei":
            return int(entry.get("amount", "0"))
    return int(balances[0].get("amount", "0"))


def iter_endpoints(config: Dict[str, Any]) -> Iterable[str]:
    nodes = config.get("rpc_nodes")
    if isinstance(nodes, list) and nodes:
        yield from nodes
    elif isinstance(config.get("rpc_node"), str):
        yield config["rpc_node"]
    else:
        raise ValueError("No RPC node configured")


def get_active_rpc(config: Dict[str, Any], *, logger: Optional[logging.Logger] = None) -> Optional[str]:
    for node in iter_endpoints(config):
        health_cmd = f"seid status --node {node}"
        if run(health_cmd, logger=logger, retries=1):
            if logger:
                logger.debug("Using RPC node: %s", node)
            return node
        if logger:
            logger.warning("RPC node appears down: %s", node)
    return None


def load_config(path: Path) -> Dict[str, Any]:
    import yaml

    with path.open("r", encoding="utf-8") as handle:
        return yaml.safe_load(handle)
