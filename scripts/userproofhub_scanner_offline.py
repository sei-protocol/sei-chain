"""Offline UserProofHub indicator scanner.

This script scans verified contract source code across EVM networks for known
Zendity/Ava Labs "UserProofHub" indicators. It operates against
Etherscan-compatible explorer APIs but can also read from user-provided files,
making it suitable for disconnected/offline review workflows when paired with
cached API responses.
"""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path
from typing import Dict, Iterable, List, Mapping, MutableMapping, Optional

import requests

# --------------------------------------------------------------------------------------
# Detection constants
# --------------------------------------------------------------------------------------
MATCH_SELECTORS: List[str] = [
    "verify(address,bytes32)",
    "getUserProofHash(address)",
    "isUserVerified(address)",
    "transportProof(address,bytes32,string)",
]

KEYWORDS: List[str] = [
    "userProofHashes",
    "ProofVerified",
    "ITeleporterMessenger",
    "sendCrossChainMessage",
    "TeleporterMessageInput",
    "TeleporterFeeInfo",
    "UserProofHub",
    "Zendity",
    "Ava Labs",
]

DEFAULT_BASE_URLS: Dict[str, str] = {
    "ethereum": "https://api.etherscan.io/api",
    "avalanche": "https://api.snowtrace.io/api",
    "base": "https://api.basescan.org/api",
    "arbitrum": "https://api.arbiscan.io/api",
    "optimism": "https://api-optimistic.etherscan.io/api",
    "sei": "https://sei-evm.blockscout.com/api",
}


# --------------------------------------------------------------------------------------
# Helper utilities
# --------------------------------------------------------------------------------------

def get_source_code(address: str, base_url: str, api_key: Optional[str] = None) -> Mapping[str, str]:
    """Fetch verified source metadata for an address using an explorer API."""

    params = {"module": "contract", "action": "getsourcecode", "address": address}
    if api_key:
        params["apikey"] = api_key

    try:
        response = requests.get(base_url, params=params, timeout=10)
        response.raise_for_status()
    except Exception as exc:  # pragma: no cover - simple logging path
        print(f"[!] Error fetching {address} from {base_url}: {exc}")
        return {}

    try:
        payload = response.json()
    except ValueError:  # pragma: no cover - invalid JSON
        print(f"[!] Invalid JSON for {address} from {base_url}")
        return {}

    result = payload.get("result")
    if isinstance(result, list) and result:
        entry = result[0]
        if isinstance(entry, dict):
            return entry
    return {}


def parse_args(argv: Optional[Iterable[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Scan contracts for UserProofHub indicators.")
    parser.add_argument(
        "--address",
        action="append",
        help="Specific contract address to scan formatted as chain:0x...",
    )
    parser.add_argument(
        "--address-file",
        action="append",
        help="File containing newline-separated addresses formatted as chain:path/to/file",
    )
    parser.add_argument(
        "--address-json",
        help="Path to JSON file containing {chain: [addresses]} mapping",
    )
    parser.add_argument(
        "--api-key",
        action="append",
        help="Explorer API key formatted as chain:KEY",
    )
    parser.add_argument(
        "--base-url",
        action="append",
        help="Override explorer base URL formatted as chain:https://custom",
    )
    parser.add_argument(
        "--include-non-matches",
        action="store_true",
        help="Include contracts with no matching indicators in the output",
    )
    parser.add_argument(
        "--output",
        default="findings_userproofhub.json",
        help="Destination JSON file for findings",
    )
    return parser.parse_args(argv)


def build_inputs(args: argparse.Namespace) -> Dict[str, List[str]]:
    """Collect chain -> [addresses] mapping from CLI arguments."""

    chains: MutableMapping[str, List[str]] = {}

    def _add(chain: str, address: str) -> None:
        chains.setdefault(chain, []).append(address.lower())

    if args.address:
        for entry in args.address:
            chain, addr = entry.split(":", 1)
            _add(chain, addr)

    if args.address_file:
        for entry in args.address_file:
            chain, file_path = entry.split(":", 1)
            for line in Path(file_path).read_text().splitlines():
                if line.strip():
                    _add(chain, line.strip())

    if args.address_json:
        data = json.loads(Path(args.address_json).read_text())
        for chain, addresses in data.items():
            for addr in addresses:
                _add(chain, addr)

    return dict(chains)


def map_keys(entries: Optional[Iterable[str]]) -> Dict[str, str]:
    if not entries:
        return {}
    result: Dict[str, str] = {}
    for entry in entries:
        chain, value = entry.split(":", 1)
        result[chain] = value
    return result


def scan(argv: Optional[Iterable[str]] = None) -> None:
    args = parse_args(argv)

    chains = build_inputs(args)
    if not chains:
        print("[!] No addresses supplied. Use --address/--address-file/--address-json.")
        return

    api_keys = map_keys(args.api_key)
    base_urls = dict(DEFAULT_BASE_URLS)
    base_urls.update(map_keys(args.base_url))

    findings: List[Dict[str, object]] = []

    for chain, addresses in chains.items():
        if chain not in base_urls:
            print(f"[!] No base URL configured for {chain}. Skipping addresses {addresses}.")
            continue

        for address in addresses:
            print(f"🔍 Scanning {chain}:{address}...")
            src = get_source_code(address, base_urls[chain], api_keys.get(chain))
            if not src:
                continue

            indicators = {
                "selectors": [],
                "events": [],
                "keywords": [],
                "matched": False,
            }

            source_code = src.get("SourceCode", "")
            if not source_code:
                if args.include_non_matches:
                    findings.append(
                        {
                            "address": address,
                            "chain": chain,
                            "contractName": src.get("ContractName"),
                            "compilerVersion": src.get("CompilerVersion"),
                            "proxy": src.get("Proxy"),
                            "implementation": src.get("Implementation"),
                            "sourceLastVerified": src.get("LastVerified"),
                            "indicators": indicators,
                        }
                    )
                continue

            code_text = source_code if isinstance(source_code, str) else json.dumps(source_code)

            for selector in MATCH_SELECTORS:
                name = selector.split("(")[0]
                if re.search(rf"\b{re.escape(name)}\b", code_text):
                    if selector.startswith("ProofVerified"):
                        indicators["events"].append(selector)
                    else:
                        indicators["selectors"].append(selector)

            for keyword in KEYWORDS:
                if re.search(rf"\b{re.escape(keyword)}\b", code_text, re.IGNORECASE):
                    indicators["keywords"].append(keyword)

            indicators["matched"] = bool(indicators["selectors"] or indicators["events"] or indicators["keywords"])

            if indicators["matched"] or args.include_non_matches:
                findings.append(
                    {
                        "address": address,
                        "chain": chain,
                        "contractName": src.get("ContractName"),
                        "compilerVersion": src.get("CompilerVersion"),
                        "proxy": src.get("Proxy"),
                        "implementation": src.get("Implementation"),
                        "sourceLastVerified": src.get("LastVerified"),
                        "indicators": indicators,
                    }
                )

    Path(args.output).write_text(json.dumps(findings, indent=2))
    print(f"\n✅ Done. {len(findings)} contracts saved to {args.output}")


if __name__ == "__main__":
    scan()
