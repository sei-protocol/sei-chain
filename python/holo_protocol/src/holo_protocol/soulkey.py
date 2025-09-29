"""SoulKey identity helpers."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import json
from pathlib import Path
import secrets
from typing import Optional

PROFILE_DIR = Path.home() / ".holo_protocol"
PROFILE_PATH = PROFILE_DIR / "profile.json"


@dataclass(slots=True)
class SoulKeyProfile:
    """Dataclass describing the identity created during setup."""

    address: str
    soulkey: str
    totp_secret: str
    created_at: datetime
    label: Optional[str] = None

    def to_json(self) -> dict:
        return {
            "address": self.address,
            "soulkey": self.soulkey,
            "totp_secret": self.totp_secret,
            "created_at": self.created_at.isoformat(),
            "label": self.label,
        }

    @classmethod
    def from_json(cls, payload: dict) -> "SoulKeyProfile":
        return cls(
            address=payload["address"],
            soulkey=payload["soulkey"],
            totp_secret=payload["totp_secret"],
            created_at=datetime.fromisoformat(payload["created_at"]),
            label=payload.get("label"),
        )


class SoulKeyManager:
    """Create and manage SoulKey profiles."""

    def __init__(self, profile_path: Path = PROFILE_PATH) -> None:
        self._path = profile_path

    @property
    def path(self) -> Path:
        return self._path

    def exists(self) -> bool:
        return self._path.exists()

    def create(self, address: str, totp_secret: str, label: Optional[str] = None) -> SoulKeyProfile:
        soulkey = secrets.token_hex(32)
        profile = SoulKeyProfile(
            address=address,
            soulkey=soulkey,
            totp_secret=totp_secret,
            created_at=datetime.now(timezone.utc),
            label=label,
        )
        self.save(profile)
        return profile

    def save(self, profile: SoulKeyProfile) -> None:
        self._path.parent.mkdir(parents=True, exist_ok=True)
        with self._path.open("w", encoding="utf-8") as handle:
            json.dump(profile.to_json(), handle, indent=2)

    def load(self) -> SoulKeyProfile:
        if not self.exists():
            raise FileNotFoundError(
                "No SoulKey profile found. Run `holo-cli setup` to create one."
            )
        with self._path.open("r", encoding="utf-8") as handle:
            payload = json.load(handle)
        return SoulKeyProfile.from_json(payload)


__all__ = ["SoulKeyProfile", "SoulKeyManager", "PROFILE_PATH"]
