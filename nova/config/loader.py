"""Load and validate Nova configuration profiles."""
from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Dict, Iterable, Optional

from nova.utils.simple_yaml import safe_load


@dataclass
class ScheduleConfig:
    interval_minutes: int
    jitter_seconds: int = 120

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ScheduleConfig":
        return cls(
            interval_minutes=int(data.get("interval_minutes", 60)),
            jitter_seconds=int(data.get("jitter_seconds", 120)),
        )


@dataclass
class StrategyConfig:
    buffer: int
    validators: list[str]
    schedule: ScheduleConfig = field(default_factory=lambda: ScheduleConfig(interval_minutes=60))
    max_delegate: Optional[int] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "StrategyConfig":
        schedule_data = data.get("schedule", {})
        return cls(
            buffer=int(data.get("buffer", 0)),
            validators=list(data.get("validators", [])),
            schedule=ScheduleConfig.from_dict(schedule_data),
            max_delegate=int(data["max_delegate"]) if "max_delegate" in data else None,
        )


@dataclass
class VaultConfig:
    address: Optional[str] = None
    role_id: Optional[str] = None
    secret_path: Optional[str] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "VaultConfig":
        return cls(
            address=data.get("address"),
            role_id=data.get("role_id"),
            secret_path=data.get("secret_path"),
        )


@dataclass
class SecurityConfig:
    vault: Optional[VaultConfig] = None
    failover_wallet: Optional[str] = None
    max_slash_alert: Optional[float] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "SecurityConfig":
        vault_data = data.get("vault")
        return cls(
            vault=VaultConfig.from_dict(vault_data) if isinstance(vault_data, dict) else None,
            failover_wallet=data.get("failover_wallet"),
            max_slash_alert=float(data["max_slash_alert"]) if "max_slash_alert" in data else None,
        )


@dataclass
class AlertsConfig:
    telegram: Optional[Dict[str, str]] = None
    slack: Optional[Dict[str, str]] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "AlertsConfig":
        telegram = data.get("telegram") if isinstance(data.get("telegram"), dict) else None
        slack = data.get("slack") if isinstance(data.get("slack"), dict) else None
        return cls(telegram=telegram, slack=slack)


@dataclass
class TelemetryConfig:
    sentry_dsn: Optional[str] = None
    prometheus_enabled: bool = False

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "TelemetryConfig":
        return cls(
            sentry_dsn=data.get("sentry_dsn"),
            prometheus_enabled=bool(data.get("prometheus_enabled", False)),
        )


@dataclass
class WalletConfig:
    address: str
    name: Optional[str] = None
    signer: str = "local"

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "WalletConfig":
        if "address" not in data:
            raise ValueError("wallet.address is required")
        return cls(
            address=str(data["address"]),
            name=data.get("name"),
            signer=str(data.get("signer", "local")),
        )


@dataclass
class ChainConfig:
    id: str
    rpc: str
    rest: Optional[str] = None
    gas_price: Optional[str] = None
    timeout_seconds: int = 30

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ChainConfig":
        if "id" not in data or "rpc" not in data:
            raise ValueError("chain.id and chain.rpc are required")
        return cls(
            id=str(data["id"]),
            rpc=str(data["rpc"]),
            rest=data.get("rest"),
            gas_price=data.get("gas_price"),
            timeout_seconds=int(data.get("timeout_seconds", 30)),
        )


@dataclass
class NovaConfig:
    wallet: WalletConfig
    chain: ChainConfig
    strategy: StrategyConfig
    security: Optional[SecurityConfig] = None
    alerts: Optional[AlertsConfig] = None
    telemetry: Optional[TelemetryConfig] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "NovaConfig":
        if "wallet" not in data or "chain" not in data or "strategy" not in data:
            raise ValueError("wallet, chain, and strategy sections are required")
        return cls(
            wallet=WalletConfig.from_dict(data["wallet"]),
            chain=ChainConfig.from_dict(data["chain"]),
            strategy=StrategyConfig.from_dict(data["strategy"]),
            security=SecurityConfig.from_dict(data.get("security", {})) if "security" in data else None,
            alerts=AlertsConfig.from_dict(data.get("alerts", {})) if "alerts" in data else None,
            telemetry=TelemetryConfig.from_dict(data.get("telemetry", {})) if "telemetry" in data else None,
        )

    def model_dump(self) -> Dict[str, Any]:
        return {
            "wallet": self.wallet.__dict__,
            "chain": self.chain.__dict__,
            "strategy": {
                "buffer": self.strategy.buffer,
                "validators": list(self.strategy.validators),
                "schedule": {
                    "interval_minutes": self.strategy.schedule.interval_minutes,
                    "jitter_seconds": self.strategy.schedule.jitter_seconds,
                },
                "max_delegate": self.strategy.max_delegate,
            },
            "security": self.security.__dict__ if self.security else None,
            "alerts": self.alerts.__dict__ if self.alerts else None,
            "telemetry": self.telemetry.__dict__ if self.telemetry else None,
        }


def load_config(path: str | Path) -> NovaConfig:
    data = _load_yaml(path)
    return NovaConfig.from_dict(data)


def _load_yaml(path: str | Path) -> Dict[str, Any]:
    with Path(path).expanduser().open("r", encoding="utf-8") as handle:
        return safe_load(handle.read())
