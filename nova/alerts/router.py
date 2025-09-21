"""Alert routing abstraction."""
from __future__ import annotations

from typing import Iterable, Protocol


class AlertProvider(Protocol):
    def send(self, message: str, level: str = "info") -> None:
        ...


class AlertRouter:
    """Fan-out router that forwards alerts to registered providers."""

    def __init__(self, providers: Iterable[AlertProvider] | None = None) -> None:
        self._providers = list(providers or [])

    def register(self, provider: AlertProvider) -> None:
        self._providers.append(provider)

    def send(self, message: str, level: str = "info") -> None:
        for provider in self._providers:
            provider.send(message, level=level)
