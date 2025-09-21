"""Simple scheduler for recurring compounding."""
from __future__ import annotations

import threading
from typing import Callable


class Scheduler:
    """Very small scheduler using threading.Timer."""

    def __init__(self) -> None:
        self._timer: threading.Timer | None = None
        self._running = False

    def start(self, func: Callable[[], None], interval_minutes: int, jitter_seconds: int) -> None:
        self._running = True

        def _run() -> None:
            if not self._running:
                return
            func()
            self._schedule_next()

        self._func = func  # type: ignore[attr-defined]
        self._interval = interval_minutes * 60  # type: ignore[attr-defined]
        self._timer = threading.Timer(self._interval, _run)
        self._timer.daemon = True
        self._timer.start()

    def _schedule_next(self) -> None:
        if not self._running:
            return
        self._timer = threading.Timer(self._interval, self._wrapped_func)
        self._timer.daemon = True
        self._timer.start()

    def stop(self) -> None:
        self._running = False
        if self._timer:
            self._timer.cancel()

    def is_running(self) -> bool:
        return self._running

    def _wrapped_func(self) -> None:
        if not self._running:
            return
        self._func()
        self._schedule_next()
