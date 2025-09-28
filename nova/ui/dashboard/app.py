"""Flask dashboard for Nova."""
from __future__ import annotations

from pathlib import Path

from flask import Flask, render_template

from nova.config import load_config
from nova.wallet import LocalSigner, VaultSigner, WalletManager


def create_dashboard_app(profile_path: str) -> Flask:
    cfg = load_config(Path(profile_path))
    app = Flask(__name__, template_folder="templates", static_folder="static")

    @app.route("/")
    def index():
        wallet = _build_wallet(cfg)
        balance = wallet.get_spendable_balance()
        return render_template("index.html", address=cfg.wallet.address, balance=balance)

    return app


def _build_wallet(cfg) -> WalletManager:
    if cfg.wallet.signer == "vault" and cfg.security and cfg.security.vault:
        vault_cfg = cfg.security.vault
        signer = VaultSigner(vault_cfg.address, vault_cfg.role_id, vault_cfg.secret_path)
    else:
        signer = LocalSigner(cfg.wallet.address)
    return WalletManager(cfg, signer)
