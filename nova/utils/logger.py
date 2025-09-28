import time
from pathlib import Path
from __future__ import annotations
from dataclasses import dataclass
from typing import Any


LOG_FILE = Path("nova.log")


@dataclass
class Logger:
    name: str

    def info(self, event: str, **kwargs: Any) -> None:
        self._log("INFO", event, **kwargs)

    def warning(self, event: str, **kwargs: Any) -> None:
        self._log("WARNING", event, **kwargs)

    def error(self, event: str, **kwargs: Any) -> None:
        self._log("ERROR", event, **kwargs)

    def _log(self, level: str, event: str, **kwargs: Any) -> None:
        now = time.strftime("%Y-%m-%d %H:%M:%S")
        formatted_msg = f"[{now}] {level} {self.name}: {event}"

        if kwargs:
            formatted_msg += " | " + " | ".join(f"{key}={value}" for key, value in kwargs.items())

        print(formatted_msg)

        # Log to file
        with LOG_FILE.open("a", encoding="utf-8") as log_file:
            log_file.write(f"{formatted_msg}\n")


def get_logger(name: str) -> Logger:
    return Logger(name)


def log(msg: str) -> None:
    now = time.strftime("%Y-%m-%d %H:%M:%S")
    formatted = f"[{now}] {msg}"
    print(formatted)
    with LOG_FILE.open("a", encoding="utf-8") as log_file:
        log_file.write(f"{formatted}\n")
