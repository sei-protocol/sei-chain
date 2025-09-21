"""Helper for interacting with HashiCorp Vault."""
from __future__ import annotations

from dataclasses import dataclass

import httpx


@dataclass
class VaultSession:
    base_url: str
    role_id: str
    secret_id: str | None = None

    def authenticate(self) -> str:
        payload = {"role_id": self.role_id}
        if self.secret_id:
            payload["secret_id"] = self.secret_id
        with httpx.Client(timeout=5.0) as client:
            resp = client.post(f"{self.base_url}/v1/auth/approle/login", json=payload)
            resp.raise_for_status()
            data = resp.json()
            return data["auth"]["client_token"]
