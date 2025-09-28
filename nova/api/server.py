"""FastAPI server exposing Nova control plane."""
from __future__ import annotations

from pathlib import Path
from typing import Any, Dict

from fastapi import Depends, FastAPI, HTTPException, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from nova.alerts import AlertRouter, TelegramProvider
from nova.config import NovaConfig, load_config
from nova.core import NovaOrchestrator
from nova.logic import RiskEngine
from nova.strategies import YieldOracle
from nova.wallet import LocalSigner, VaultSigner, WalletManager

bearer = HTTPBearer(auto_error=False)


def create_app(profile_path: str, token: str | None = None) -> FastAPI:
    config = load_config(Path(profile_path))
    orchestrator = _build_orchestrator(config)

    app = FastAPI(title="Nova API", version="0.1.0")

    def _auth(credentials: HTTPAuthorizationCredentials | None = Depends(bearer)) -> None:
        if token is None:
            return
        if credentials is None or credentials.credentials != token:
            raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Unauthorized")

    @app.get("/status")
    def get_status(_: None = Depends(_auth)) -> Dict[str, Any]:
        wallet = _build_wallet(config)
        balance = wallet.get_spendable_balance()
        oracle = YieldOracle(config.chain.rpc, config.chain.rest)
        ranked = oracle.rank_validators(config.strategy.validators)
        return {
            "wallet": config.wallet.address,
            "balance": balance,
            "validators": ranked,
        }

    @app.post("/compound")
    def trigger_compound(_: None = Depends(_auth)) -> Dict[str, str]:
        orchestrator.run(dry_run=False)
        return {"status": "ok"}

    @app.get("/config")
    def get_config(_: None = Depends(_auth)) -> Dict[str, Any]:
        return config.model_dump()

    return app


def _build_orchestrator(cfg: NovaConfig) -> NovaOrchestrator:
    wallet = _build_wallet(cfg)
    oracle = YieldOracle(cfg.chain.rpc, cfg.chain.rest)
    risk_engine = RiskEngine(
        buffer=cfg.strategy.buffer,
        max_delegate=cfg.strategy.max_delegate,
        validator_cap=len(cfg.strategy.validators),
    )
    alerts = _build_alerts(cfg)
    return NovaOrchestrator(cfg, wallet, oracle, risk_engine, alerts)


def _build_wallet(cfg: NovaConfig) -> WalletManager:
    signer_type = cfg.wallet.signer
    if signer_type == "vault" and cfg.security and cfg.security.vault:
        vault_cfg = cfg.security.vault
        signer = VaultSigner(vault_cfg.address, vault_cfg.role_id, vault_cfg.secret_path)
    else:
        signer = LocalSigner(cfg.wallet.address)
    return WalletManager(cfg, signer)


def _build_alerts(cfg: NovaConfig) -> AlertRouter:
    providers = []
    if cfg.alerts and cfg.alerts.telegram:
        tg = cfg.alerts.telegram
        providers.append(TelegramProvider(token=tg["token"], chat_id=tg["chat_id"]))
    return AlertRouter(providers)
