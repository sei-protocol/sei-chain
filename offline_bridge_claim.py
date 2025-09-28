#!/usr/bin/env python3
"""Generate KinBridge offline proof-of-claim payloads.

The resulting fingerprint matches the value returned by the KinBridge UI hook
(`useSoulSigilProof`) so the script can be used in airgapped or automated
contexts to pre-compute claim authorisations.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from dataclasses import dataclass
from typing import Any, Dict

try:  # pragma: no cover - import guard for optional dependencies
    from eth_hash.auto import keccak  # type: ignore
except Exception:  # pragma: no cover
    try:
        import sha3  # type: ignore

        def keccak(data: bytes) -> bytes:  # type: ignore
            hasher = sha3.keccak_256()
            hasher.update(data)
            return hasher.digest()

    except Exception:  # pragma: no cover
        # Pure Python Keccak implementation adapted for offline environments.
        _ROTATION_OFFSETS = (
            (0, 36, 3, 41, 18),
            (1, 44, 10, 45, 2),
            (62, 6, 43, 15, 61),
            (28, 55, 25, 21, 56),
            (27, 20, 39, 8, 14),
        )

        _ROUND_CONSTANTS = (
            0x0000000000000001,
            0x0000000000008082,
            0x800000000000808A,
            0x8000000080008000,
            0x000000000000808B,
            0x0000000080000001,
            0x8000000080008081,
            0x8000000000008009,
            0x000000000000008A,
            0x0000000000000088,
            0x0000000080008009,
            0x000000008000000A,
            0x000000008000808B,
            0x800000000000008B,
            0x8000000000008089,
            0x8000000000008003,
            0x8000000000008002,
            0x8000000000000080,
            0x000000000000800A,
            0x800000008000000A,
            0x8000000080008081,
            0x8000000000008080,
            0x0000000080000001,
            0x8000000080008008,
        )

        def _rotl(value: int, shift: int) -> int:
            return ((value << shift) | (value >> (64 - shift))) & 0xFFFFFFFFFFFFFFFF

        def _keccak_f(state: list[int]) -> None:
            for round_constant in _ROUND_CONSTANTS:
                c = [0] * 5
                for x in range(5):
                    c[x] = (
                        state[x]
                        ^ state[x + 5]
                        ^ state[x + 10]
                        ^ state[x + 15]
                        ^ state[x + 20]
                    )

                d = [0] * 5
                for x in range(5):
                    d[x] = c[(x - 1) % 5] ^ _rotl(c[(x + 1) % 5], 1)

                for x in range(5):
                    for y in range(5):
                        state[x + 5 * y] ^= d[x]

                b = [0] * 25
                for x in range(5):
                    for y in range(5):
                        index = x + 5 * y
                        new_x = y
                        new_y = (2 * x + 3 * y) % 5
                        shift = _ROTATION_OFFSETS[x][y]
                        b[new_x + 5 * new_y] = _rotl(state[index], shift)

                for x in range(5):
                    for y in range(5):
                        index = x + 5 * y
                        state[index] = b[index] ^ ((~b[((x + 1) % 5) + 5 * y]) & b[((x + 2) % 5) + 5 * y])

                state[0] ^= round_constant

        def keccak(data: bytes) -> bytes:  # type: ignore
            rate = 136  # bytes
            state = [0] * 25
            padded = bytearray(data)
            padded.append(0x01)
            while (len(padded) % rate) != rate - 1:
                padded.append(0x00)
            padded.append(0x80)

            for offset in range(0, len(padded), rate):
                block = padded[offset : offset + rate]
                for i in range(rate // 8):
                    start = i * 8
                    state[i] ^= int.from_bytes(block[start : start + 8], "little")
                _keccak_f(state)

            output = bytearray()
            while len(output) < 32:
                for i in range(rate // 8):
                    output.extend(state[i].to_bytes(8, "little"))
                if len(output) >= 32:
                    break
                _keccak_f(state)

            return bytes(output[:32])


def _ensure_hex_address(value: str) -> str:
    if not isinstance(value, str) or not value.startswith("0x") or len(value) != 42:
        raise ValueError("account must be a 0x-prefixed 20 byte address")
    return value.lower()


def _pack_uint256(value: int) -> bytes:
    if value < 0:
        raise ValueError("numeric values must be positive")
    return value.to_bytes(32, byteorder="big")


def _pack_address(value: str) -> bytes:
    return bytes.fromhex(value[2:])


def compute_fingerprint(account: str, chain_id: int, timestamp: int) -> Dict[str, Any]:
    account_normalised = _ensure_hex_address(account)
    payload = {
        "account": account_normalised,
        "chainId": chain_id,
        "timestamp": timestamp,
    }

    packed = _pack_address(account_normalised) + _pack_uint256(chain_id) + _pack_uint256(timestamp)
    fingerprint = "0x" + keccak(packed).hex()

    return {
        "fingerprint": fingerprint,
        "payload": payload,
    }


@dataclass
class Arguments:
    account: str
    chain_id: int
    timestamp: int
    output: str | None


def parse_args(argv: list[str]) -> Arguments:
    parser = argparse.ArgumentParser(description="Generate a KinBridge claim proof")
    parser.add_argument("account", help="0x-prefixed address that is eligible to claim")
    parser.add_argument("chain_id", type=int, help="EVM chain identifier")
    parser.add_argument(
        "--timestamp",
        type=int,
        default=None,
        help="Unix timestamp to embed in the proof (defaults to current time)",
    )
    parser.add_argument(
        "--output",
        help="Optional file to write the JSON payload to (stdout when omitted)",
    )

    parsed = parser.parse_args(argv)
    timestamp = parsed.timestamp if parsed.timestamp is not None else int(time.time())

    return Arguments(
        account=parsed.account,
        chain_id=parsed.chain_id,
        timestamp=timestamp,
        output=parsed.output,
    )


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv or sys.argv[1:])

    try:
        result = compute_fingerprint(args.account, args.chain_id, args.timestamp)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    output_data = json.dumps(result, indent=2)

    if args.output:
        with open(args.output, "w", encoding="utf-8") as handle:
            handle.write(output_data + "\n")
    else:
        print(output_data)

    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
