from __future__ import annotations

from pathlib import Path
import click

from nova.alerts import AlertRouter, TelegramProvider
from nova.config import load_config
from nova.core import NovaOrchestrator, Scheduler
from nova.logic import RiskEngine
from nova.strategies import YieldOracle
from nova.wallet import LocalSigner, VaultSigner, WalletManager
from nova.utils.logger import get_logger

logger = get_logger(__name__)

@click.group()
def nova() -> None:
    """Nova – SEI validator compounding control plane."""

@nova.command()
@click.option("--profile", type=click.Path(exists=True, path_type=Path), required=True)
@click.option("--dry-run", is_flag=True, default=False)
def run(profile: Path, dry_run: bool) -> None:
    """Execute a single compounding loop."""
    cfg = load_config(profile)
    orchestrator = _build_orchestrator(cfg)
    orchestrator.run(dry_run=dry_run)

@nova.command()
@click.option("--profile", type=click.Path(exists=True, path_type=Path), required=True)
def status(profile: Path) -> None:
    """Show wallet and validator status."""
    cfg = load_config(profile)
    wallet = _build_wallet(cfg)
    balance = wallet.get_spendable_balance()
    click.echo(f"Wallet: {cfg.wallet.address}")
    click.echo(f"Balance: {balance} usei")

@nova.command()
@click.option("--profile", type=click.Path(exists=True, path_type=Path), required=True)
def auto(profile: Path) -> None:
    """Start the recurring scheduler."""
    cfg = load_config(profile)
    orchestrator = _build_orchestrator(cfg)
    schedule = cfg.strategy.schedule
    scheduler = Scheduler()
    scheduler.start(lambda: orchestrator.run(dry_run=False), schedule.interval_minutes, schedule.jitter_seconds)
    click.echo("Scheduler started. Press Ctrl+C to exit.")
    try:
        while True:
            pass
    except KeyboardInterrupt:
        scheduler.stop()

def _build_orchestrator(cfg):
    wallet = _build_wallet(cfg)
    oracle = YieldOracle(cfg.chain.rpc, cfg.chain.rest)
    risk_engine = RiskEngine(
        buffer=cfg.strategy.buffer,
        max_delegate=cfg.strategy.max_delegate,
        validator_cap=len(cfg.strategy.validators),
    )
    alerts = _build_alerts(cfg)
    return NovaOrchestrator(cfg, wallet, oracle, risk_engine, alerts)

def _build_wallet(cfg):
    signer_type = cfg.wallet.signer
    if signer_type == "vault" and cfg.security and cfg.security.vault:
        vault_cfg = cfg.security.vault
        signer = VaultSigner(vault_cfg.address, vault_cfg.role_id, vault_cfg.secret_path)
    else:
        signer = LocalSigner(cfg.wallet.address)
    return WalletManager(cfg, signer)

def _build_alerts(cfg):
    providers = []
    if cfg.alerts and cfg.alerts.telegram:
        tg = cfg.alerts.telegram
        providers.append(TelegramProvider(token=tg["token"], chat_id=tg["chat_id"]))
    return AlertRouter(providers)

if __name__ == "__main__":
    nova()
