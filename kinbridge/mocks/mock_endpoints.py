#!/usr/bin/env python3
"""Mock Circle CCTP and Chainlink CCIP endpoints for KinBridge integration tests."""

from __future__ import annotations

import argparse
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Dict


class KinBridgeMockHandler(BaseHTTPRequestHandler):
    server_version = "KinBridgeMock/1.0"

    def log_message(self, format: str, *args: Any) -> None:  # pragma: no cover - suppress noisy output
        return

    def _read_body(self) -> Dict[str, Any]:
        length = int(self.headers.get("Content-Length", "0"))
        raw_body = self.rfile.read(length) if length else b""
        if not raw_body:
            return {}

        try:
            return json.loads(raw_body.decode("utf-8"))
        except json.JSONDecodeError:
            self.send_error(400, "invalid_json")
            raise

    def _write_json(self, status: int, payload: Dict[str, Any]) -> None:
        response = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)

    def do_POST(self) -> None:  # noqa: N802 - required by BaseHTTPRequestHandler
        try:
            body = self._read_body()
        except json.JSONDecodeError:
            return

        if self.path == "/cctp/burns":
            burn_id = body.get("burnId") or "burn-mock"
            self._write_json(
                200,
                {
                    "burnId": burn_id,
                    "status": "completed",
                    "amount": body.get("amount", "0"),
                    "sourceChain": body.get("sourceChain", "sei"),
                    "destinationChain": body.get("destinationChain", "ethereum"),
                },
            )
            return

        if self.path == "/cctp/mints":
            self._write_json(
                200,
                {
                    "mintId": body.get("mintId") or "mint-mock",
                    "status": "completed",
                    "txHash": "0xmockmint",  # deterministic for testing
                },
            )
            return

        if self.path == "/ccip/send":
            self._write_json(
                200,
                {
                    "messageId": body.get("messageId") or "ccip-msg-mock",
                    "status": "dispatched",
                    "payload": body.get("payload"),
                },
            )
            return

        if self.path == "/ccip/receive":
            self._write_json(
                200,
                {
                    "messageId": body.get("messageId") or "ccip-msg-mock",
                    "status": "delivered",
                    "receipt": body.get("receipt", {}),
                },
            )
            return

        self._write_json(404, {"error": "unknown_route", "path": self.path})


def serve(host: str, port: int) -> HTTPServer:
    server = HTTPServer((host, port), KinBridgeMockHandler)
    print(f"KinBridge mock server listening on http://{host}:{port}")
    return server


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Run KinBridge mock transport endpoints")
    parser.add_argument("--host", default="127.0.0.1", help="Address to bind the mock server to")
    parser.add_argument("--port", type=int, default=8787, help="Port to listen on")
    args = parser.parse_args(argv)

    server = serve(args.host, args.port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:  # pragma: no cover - manual exit path
        print("\nMock server stopped")
    finally:
        server.server_close()

    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
