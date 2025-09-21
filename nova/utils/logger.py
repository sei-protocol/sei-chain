"""Lightweight structured logger facade."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass
class Logger:
    name: str

    def info(self, event: str, **kwargs: Any) -> None:
        pass

    def warning(self, event: str, **kwargs: Any) -> None:
        pass

    def error(self, event: str, **kwargs: Any) -> None:
        pass


def get_logger(name: str) -> Logger:
    return Logger(name)
