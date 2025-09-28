from __future__ import annotations

import logging
from typing import Any, Dict, Optional

import requests


def send_alert(message: str, config: Dict[str, Any], *, logger: Optional[logging.Logger] = None) -> None:
    telegram_cfg = config.get("telegram", {})
    if not telegram_cfg or not telegram_cfg.get("enabled"):
        return

    token = telegram_cfg.get("token")
    chat_id = telegram_cfg.get("chat_id")
    if not token or not chat_id:
        if logger:
            logger.warning("Telegram enabled but token/chat_id missing")
        return

    url = f"https://api.telegram.org/bot{token}/sendMessage"
    payload = {"chat_id": chat_id, "text": message}

    try:
        response = requests.post(url, data=payload, timeout=10)
        response.raise_for_status()
    except requests.RequestException as exc:
        if logger:
            logger.error("Failed to send telegram alert: %s", exc)
