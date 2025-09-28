from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from typing import Iterable, List

from wallet.signer import (
    available_balance,
    delegate,
    get_address,
    withdraw_rewards,
)
from strategies.yield_oracle import get_validators
from config.loader import load_config
from alerts.telegram import notify
from utils.logger import log

from nova.logic.risk import RiskEngine
from nova.strategies.yield_oracle import YieldOracle
from nova.wallet.manager import WalletManager
from nova.alerts.router import AlertRouter
from nova.config.loader import NovaConfig
from nova.utils.logger import get_logger

logger = get_logger(__name__)

@dataclass
class DelegationPlan:
    validator: str
    amount: int


class NovaOrchestrator:
    """Coordinates compounding flows using injected domain services."""

    def __init__(
        self,
        config: NovaConfig,
        wallet: WalletManager,
        oracle: YieldOracle,
        risk_engine: RiskEngine,
        alerts: AlertRouter,
    ) -> None:
        self._config = config
        self._wallet = wallet
        self._oracle = oracle
        self._risk_engine = risk_engine
        self._alerts = alerts

    def run(self, dry_run: bool = False) -> None:
        logger.info("nova.compound.start", dry_run=dry_run)
        rewards = self._wallet.withdraw_rewards(self._config.strategy.validators, dry_run=dry_run)
        balance = self._wallet.get_spendable_balance()
        logger.info("nova.compound.state", rewards=rewards, balance=balance)

        if not self._risk_engine.within_limits(balance):
            self._alerts.send("Risk check failed. Halting compounding run.")
            logger.warning("nova.compound.risk_blocked", balance=balance)
            return

        plan = self._build_plan(balance)
        if not plan:
            logger.info("nova.compound.noop", reason="empty plan")
            return

        for step in plan:
            if dry_run:
                logger.info(
                    "nova.compound.plan", validator=step.validator, amount=step.amount, dry_run=True
                )
                continue

            tx_hash = self._wallet.delegate(step.validator, step.amount)
            self._alerts.send(
                f"Delegated {step.amount} usei to {step.validator}. Tx: {tx_hash}",
                level="success",
            )
            logger.info(
                "nova.compound.delegated",
                validator=step.validator,
                amount=step.amount,
                tx_hash=tx_hash,
                timestamp=datetime.utcnow().isoformat(),
            )

    def _build_plan(self, balance: int) -> List[DelegationPlan]:
        buffer = self._config.strategy.buffer
        if balance <= buffer:
            logger.info("nova.compound.noop", reason="buffer", buffer=buffer, balance=balance)
            return []

        available = balance - buffer
        candidates = self._oracle.rank_validators(self._config.strategy.validators)
        safe_candidates: Iterable[str] = self._risk_engine.filter_validators(candidates)

        allocation = self._risk_engine.split_allocation(available, safe_candidates)
        return [DelegationPlan(validator=val, amount=amt) for val, amt in allocation]


def run_cycle():
    cfg = load_config()
    addr = get_address(cfg["wallet_name"])
    validators = get_validators()

    withdraw_rewards(cfg, addr)

    balance = available_balance(cfg, addr)
    if balance < cfg["buffer"]:
        log(f"⛔ Buffer too low ({balance}). Skipping delegate.")
        return

    per_val = balance // len(validators)
    for val in validators:
        delegate(cfg, addr, val, per_val)
        notify(f"✅ Delegated {per_val} to {val}")
