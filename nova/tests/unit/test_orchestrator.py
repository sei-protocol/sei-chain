from nova.alerts.router import AlertRouter
from nova.config.loader import NovaConfig
from nova.core.orchestrator import NovaOrchestrator
from nova.logic.risk import RiskEngine
from nova.strategies.yield_oracle import YieldOracle
from nova.wallet.manager import WalletManager


class DummySigner:
    def __init__(self) -> None:
        self.balance = 100_000
        self.delegations: list[tuple[str, int]] = []

    def withdraw_rewards(self, validators, dry_run=False):
        return 1000

    def get_balance(self):
        return self.balance

    def delegate(self, validator, amount):
        self.delegations.append((validator, amount))
        return "txhash"


class DummyOracle(YieldOracle):
    def __init__(self) -> None:  # type: ignore[override]
        pass

    def rank_validators(self, validators):  # type: ignore[override]
        return list(validators)


def test_orchestrator_builds_plan():
    cfg = NovaConfig.from_dict(
        {
            "wallet": {"address": "sei1", "signer": "local"},
            "chain": {"id": "pacific-1", "rpc": "https://rpc"},
            "strategy": {"buffer": 1000, "validators": ["val1", "val2"], "schedule": {"interval_minutes": 60, "jitter_seconds": 10}},
        }
    )
    signer = DummySigner()
    wallet = WalletManager(cfg, signer)
    oracle = DummyOracle()
    risk = RiskEngine(buffer=cfg.strategy.buffer)
    alerts = AlertRouter([])
    orchestrator = NovaOrchestrator(cfg, wallet, oracle, risk, alerts)

    orchestrator.run(dry_run=True)
    assert signer.delegations == []
