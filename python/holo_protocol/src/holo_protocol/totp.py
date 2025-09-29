"""Time-based one-time password helpers."""

from __future__ import annotations

import base64
import hashlib
import hmac
import os
import struct
import time
from typing import Optional

try:
    import qrcode  # type: ignore
except Exception:  # pragma: no cover - optional dependency
    qrcode = None  # pragma: no cover


def generate_totp_secret(length: int = 20) -> str:
    """Create a base32-encoded secret suitable for TOTP provisioning."""

    if length <= 0:
        raise ValueError("Secret length must be positive")
    raw = os.urandom(length)
    return base64.b32encode(raw).decode("ascii").rstrip("=")


def provisioning_uri(secret: str, account_name: str, issuer: str, period: int = 30) -> str:
    """Return an otpauth URI for the provided parameters."""

    return (
        f"otpauth://totp/{issuer}:{account_name}?secret={secret}&issuer={issuer}&period={period}"
    )


def _dynamic_truncate(hmac_digest: bytes, digits: int) -> str:
    offset = hmac_digest[-1] & 0x0F
    code = struct.unpack_from(">I", hmac_digest, offset)[0] & 0x7FFFFFFF
    return str(code % (10**digits)).zfill(digits)


def totp_code(secret: str, timestamp: Optional[int] = None, interval: int = 30, digits: int = 6) -> str:
    """Generate a TOTP code for the provided secret."""

    if timestamp is None:
        timestamp = int(time.time())
    key = base64.b32decode(secret.upper() + "=" * ((8 - len(secret) % 8) % 8))
    counter = timestamp // interval
    msg = struct.pack(">Q", counter)
    digest = hmac.new(key, msg, hashlib.sha1).digest()
    return _dynamic_truncate(digest, digits)


def ascii_qr(data: str) -> Optional[str]:  # pragma: no cover - depends on optional qrcode
    """Render a QR code using the optional `qrcode` dependency."""

    if qrcode is None:
        return None
    qr = qrcode.QRCode(border=1)
    qr.add_data(data)
    qr.make(fit=True)
    matrix = qr.get_matrix()
    lines = ["".join("██" if cell else "  " for cell in row) for row in matrix]
    return "\n".join(lines)


__all__ = ["generate_totp_secret", "provisioning_uri", "totp_code", "ascii_qr"]
