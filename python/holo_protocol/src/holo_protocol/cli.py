"""Command line interface mirroring the reference repo."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys
from textwrap import indent

from .alerts import AlertConfig, SeiAlertStream
from .guardian import GuardianEngine, Transaction
from .soulkey import SoulKeyManager
from .totp import ascii_qr, generate_totp_secret, provisioning_uri, totp_code


def _print_profile(manager: SoulKeyManager, show_qr: bool) -> None:
    profile = manager.load()
    uri = provisioning_uri(profile.totp_secret, profile.label or profile.address, "Holo")
    print(f"SoulKey: {profile.soulkey}")
    print(f"Address: {profile.address}")
    if profile.label:
        print(f"Label: {profile.label}")
    print(f"Created: {profile.created_at.isoformat()}")
    print(f"TOTP Secret: {profile.totp_secret}")
    print(f"Provisioning URI: {uri}")
    print(f"Current TOTP: {totp_code(profile.totp_secret)}")
    if show_qr:
        qr = ascii_qr(uri)
        if qr:
            print("\n" + qr)
        else:
            print("\nInstall the optional `qrcode` dependency to render ASCII QR codes.")


def cmd_setup(args: argparse.Namespace) -> None:
    manager = SoulKeyManager()
    if manager.exists() and not args.force:
        print("A SoulKey profile already exists. Use --force to overwrite.", file=sys.stderr)
        sys.exit(1)
    secret = generate_totp_secret()
    profile = manager.create(address=args.address, totp_secret=secret, label=args.label)
    print("Created SoulKey profile:")
    print(indent(json.dumps(profile.to_json(), indent=2), prefix="  "))


def cmd_status(args: argparse.Namespace) -> None:
    manager = SoulKeyManager()
    _print_profile(manager, show_qr=args.show_qr)


def cmd_alerts(args: argparse.Namespace) -> None:
    stream = SeiAlertStream()
    config = AlertConfig(address=args.address, poll_interval=args.interval, limit=args.limit)
    print(f"Streaming Sei alerts for {config.address} (limit={config.limit})...")
    for alert in stream.stream(config):
        print(
            f"[{alert.timestamp.isoformat()}] tx={alert.tx_hash} sender={alert.sender} "
            f"recipient={alert.recipient} amount={alert.amount} {alert.denom} memo='{alert.memo}'"
        )


def cmd_guardian(args: argparse.Namespace) -> None:
    engine = GuardianEngine()
    payload = json.loads(Path(args.tx_file).read_text(encoding="utf-8"))
    tx = Transaction(
        amount=float(payload["amount"]),
        denom=payload.get("denom", "usei"),
        sender=payload.get("sender", ""),
        recipient=payload.get("recipient", ""),
        location=payload.get("location", "unknown"),
        memo=payload.get("memo", ""),
        risk_tags=list(payload.get("risk_tags", [])),
    )
    decision = engine.evaluate(tx)
    print(json.dumps({"verdict": decision.verdict, "score": decision.score, "reasons": decision.reasons}, indent=2))


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Holo Protocol CLI")
    sub = parser.add_subparsers(dest="command")

    setup = sub.add_parser("setup", help="Initialise a new SoulKey profile")
    setup.add_argument("--address", required=True, help="Sei account address")
    setup.add_argument("--label", help="Optional label for the SoulKey")
    setup.add_argument("--force", action="store_true", help="Overwrite an existing profile")
    setup.set_defaults(func=cmd_setup)

    status = sub.add_parser("status", help="Display the active SoulKey profile")
    status.add_argument("--show-qr", action="store_true", help="Render the provisioning QR code")
    status.set_defaults(func=cmd_status)

    alerts = sub.add_parser("alerts", help="Stream Sei transaction alerts")
    alerts.add_argument("--address", required=True, help="Sei account address to monitor")
    alerts.add_argument("--interval", type=float, default=1.5, help="Polling interval in seconds")
    alerts.add_argument("--limit", type=int, help="Number of alerts to stream (default infinite)")
    alerts.set_defaults(func=cmd_alerts)

    guardian = sub.add_parser("guardian", help="Evaluate a transaction with the Guardian engine")
    guardian.add_argument("--tx-file", required=True, help="Path to a JSON transaction payload")
    guardian.set_defaults(func=cmd_guardian)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if not hasattr(args, "func"):
        parser.print_help()
        return 1
    args.func(args)
    return 0


if __name__ == "__main__":  # pragma: no cover - CLI entry point
    raise SystemExit(main())
