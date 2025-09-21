"""Telegram alert provider."""
from __future__ import annotations

from typing import Optional

from urllib import request, parse


class TelegramProvider:
    def __init__(self, token: str, chat_id: str, parse_mode: Optional[str] = "MarkdownV2") -> None:
        self._token = token
        self._chat_id = chat_id
        self._parse_mode = parse_mode

    def send(self, message: str, level: str = "info") -> None:
        prefix = {
            "success": "✅",
            "warning": "⚠️",
            "error": "❌",
        }.get(level, "ℹ️")
        payload = {
            "chat_id": self._chat_id,
            "text": f"{prefix} {message}",
        }
        if self._parse_mode:
            payload["parse_mode"] = self._parse_mode
        data = parse.urlencode(payload).encode()
        try:
            request.urlopen(  # nosec B310 - used for simple webhook call
                f"https://api.telegram.org/bot{self._token}/sendMessage", data=data, timeout=5.0
            )
        except Exception:
            pass
