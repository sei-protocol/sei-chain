"""UserProofHub contract scanner.

This script scans verified contract sources across multiple EVM networks looking for
Zendity/Ava Labs "UserProofHub" logic by matching specific function selectors,
 event signature hashes, and keywords.

The script can fetch verified source code from Etherscan-compatible APIs or read
pre-fetched contract metadata from local JSON files. Results are reported as JSON
with detail about which indicators matched for each contract.

Example usage::

    python scripts/userproofhub_scanner.py \
        --address ethereum:0x1234... \
        --address-file avalanche:addresses.txt \
        --api-key ethereum:$ETHERSCAN_API_KEY \
        --output findings.json

"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Dict, Iterable, List, Optional, Sequence, Tuple

# ------------------------------
# Detection constants
# ------------------------------
FUNCTION_SELECTORS = {
    "verify(address,bytes32)": "0x8df6929f",
    "getUserProofHash(address)": "0x8231cdd1",
    "isUserVerified(address)": "0x04e94d4a",
    "transportProof(address,bytes32,string)": "0xdca75e17",
}

EVENT_SIGNATURES = {
    "ProofVerified(address,bytes32)": "0xfbc7ef77a3a7e737c4c9575fc45cfb8cc30b2ea9a68b78b9b0067ff7c7f36796",
    "MessageSent(bytes32,address,bytes32,string)": "0x297dcf12a6d9df0214f2c2388d7a4bcd6a83d4378962e4a739e9ddce3cb7a901",
}

KEYWORDS = [
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

DEFAULT_NETWORKS = {
    "avalanche": "https://api.snowtrace.io/api",
    "ethereum": "https://api.etherscan.io/api",
    "base": "https://api.basescan.org/api",
    "arbitrum": "https://api.arbiscan.io/api",
    "optimism": "https://api-optimistic.etherscan.io/api",
    # The Sei EVM explorer currently exposes an Etherscan-compatible API via Blockscout.
    # Users can override this endpoint with --base-url if needed.
    "sei": "https://sei-evm.blockscout.com/api",
}

NETWORK_ENV_KEYS = {
    "avalanche": ["SNOWTRACE_API_KEY", "AVALANCHE_API_KEY"],
    "ethereum": ["ETHERSCAN_API_KEY"],
    "base": ["BASESCAN_API_KEY"],
    "arbitrum": ["ARBISCAN_API_KEY", "ARBITRUM_API_KEY"],
    "optimism": ["OPTIMISM_API_KEY", "OPTIMISTIC_ETHERSCAN_API_KEY"],
    "sei": ["SEI_API_KEY", "SEISCAN_API_KEY"],
}

# Rotation offsets for the Keccak-f[1600] permutation.
_ROTATION_OFFSETS: Tuple[Tuple[int, ...], ...] = (
    (0, 36, 3, 41, 18),
    (1, 44, 10, 45, 2),
    (62, 6, 43, 15, 61),
    (28, 55, 25, 21, 56),
    (27, 20, 39, 8, 14),
)

# Round constants for Keccak-f[1600].
_ROUND_CONSTANTS: Tuple[int, ...] = (
    0x0000000000000001,
    0x0000000000008082,
    0x800000000000808a,
    0x8000000080008000,
    0x000000000000808b,
    0x0000000080000001,
    0x8000000080008081,
    0x8000000000008009,
    0x000000000000008a,
    0x0000000000000088,
    0x0000000080008009,
    0x000000008000000a,
    0x000000008000808b,
    0x800000000000008b,
    0x8000000000008089,
    0x8000000000008003,
    0x8000000000008002,
    0x8000000000000080,
    0x000000000000800a,
    0x800000008000000a,
    0x8000000080008081,
    0x8000000000008080,
    0x0000000080000001,
    0x8000000080008008,
)


def _rotl(value: int, shift: int) -> int:
    return ((value << shift) | (value >> (64 - shift))) & 0xFFFFFFFFFFFFFFFF


def keccak256(data: bytes) -> bytes:
    """Pure Python Keccak-256 implementation.

    The implementation follows the reference Keccak-f[1600] permutation and
    absorbs 136-byte (1088-bit) blocks with the SHA3 padding (multi-rate padding
    with domain separator 0x01).
    """

    rate = 136  # bytes
    capacity = 64  # bytes
    assert rate + capacity == 200

    # Initialize 5x5 lane matrix with zeros.
    state = [0] * 25

    def keccak_f() -> None:
        for rc in _ROUND_CONSTANTS:
            # Theta step
            c = [state[x] ^ state[x + 5] ^ state[x + 10] ^ state[x + 15] ^ state[x + 20] for x in range(5)]
            d = [c[(x - 1) % 5] ^ _rotl(c[(x + 1) % 5], 1) for x in range(5)]
            for x in range(5):
                for y in range(0, 25, 5):
                    state[x + y] ^= d[x]

            # Rho and Pi steps
            b = [0] * 25
            for x in range(5):
                for y in range(5):
                    idx = x + 5 * y
                    rot = _ROTATION_OFFSETS[x][y]
                    new_x = y
                    new_y = (2 * x + 3 * y) % 5
                    b[new_x + 5 * new_y] = _rotl(state[idx], rot)

            # Chi step
            for x in range(5):
                for y in range(5):
                    idx = x + 5 * y
                    state[idx] = b[idx] ^ ((~b[((x + 1) % 5) + 5 * y]) & b[((x + 2) % 5) + 5 * y])

            # Iota step
            state[0] ^= rc

    # Absorb input blocks with padding.
    offset = 0
    while offset < len(data):
        block = data[offset : offset + rate]
        if len(block) < rate:
            block = bytearray(block)
            block.append(0x01)
            block.extend(b"\x00" * (rate - len(block) - 1))
            block.append(0x80)
        for i in range(0, len(block), 8):
            lane = int.from_bytes(block[i : i + 8], "little")
            state[i // 8] ^= lane
        keccak_f()
        offset += rate

    if len(data) % rate == 0:
        # Need to absorb an extra padded block when input length is multiple of rate.
        block = bytearray(rate)
        block[0] = 0x01
        block[-1] = 0x80
        for i in range(0, rate, 8):
            lane = int.from_bytes(block[i : i + 8], "little")
            state[i // 8] ^= lane
        keccak_f()

    # Squeeze output.
    output = bytearray()
    while len(output) < 32:
        for i in range(0, rate, 8):
            output.extend(state[i // 8].to_bytes(8, "little"))
            if len(output) >= 32:
                return bytes(output[:32])
        keccak_f()
    return bytes(output[:32])


@dataclass
class NetworkConfig:
    name: str
    base_url: str
    api_key: Optional[str] = None
    extra_params: Dict[str, str] = field(default_factory=dict)


def canonical_type(param: Dict) -> str:
    """Build the canonical type string for an ABI input/output."""

    type_name = param.get("type", "")
    if not type_name:
        return ""

    if type_name.startswith("tuple"):
        components = param.get("components", [])
        tuple_types = ",".join(canonical_type(c) for c in components)
        array_suffix = type_name[5:]
        return f"({tuple_types}){array_suffix}"
    return type_name


def selector_from_abi_entry(entry: Dict) -> Optional[str]:
    if entry.get("type") != "function":
        return None
    name = entry.get("name")
    if not name:
        return None
    inputs = entry.get("inputs", [])
    signature = f"{name}({','.join(canonical_type(i) for i in inputs)})"
    digest = keccak256(signature.encode("utf-8"))
    return "0x" + digest[:4].hex()


def event_hash_from_abi_entry(entry: Dict) -> Optional[str]:
    if entry.get("type") != "event":
        return None
    name = entry.get("name")
    if not name:
        return None
    inputs = entry.get("inputs", [])
    signature = f"{name}({','.join(canonical_type(i) for i in inputs)})"
    digest = keccak256(signature.encode("utf-8"))
    return "0x" + digest.hex()


def _normalize_source_field(source: str) -> List[str]:
    if not source:
        return []

    source = source.strip()
    if not source:
        return []

    # Some explorers wrap metadata in {{{ }}} for multi-file contracts.
    if source.startswith("{{") and source.endswith("}}"):
        source = source[1:-1]
    try:
        parsed = json.loads(source)
    except json.JSONDecodeError:
        return [source]

    if isinstance(parsed, dict):
        if "sources" in parsed and isinstance(parsed["sources"], dict):
            contents = []
            for meta in parsed["sources"].values():
                content = meta.get("content") if isinstance(meta, dict) else None
                if isinstance(content, str):
                    contents.append(content)
            return contents
        if "source" in parsed and isinstance(parsed["source"], str):
            return [parsed["source"]]
    return [source]


def detect_indicators(contract_meta: Dict) -> Dict:
    abi_raw = contract_meta.get("ABI", "")
    sources = _normalize_source_field(contract_meta.get("SourceCode", ""))
    checks = {
        "selectors": [],
        "events": [],
        "keywords": [],
    }

    abi_entries: Sequence[Dict] = []
    if abi_raw and abi_raw not in ("", "Contract source code not verified"):
        try:
            abi_entries = json.loads(abi_raw)
        except json.JSONDecodeError:
            abi_entries = []

    found_selectors = set()
    for entry in abi_entries:
        selector = selector_from_abi_entry(entry)
        if selector is None:
            continue
        for signature, target_selector in FUNCTION_SELECTORS.items():
            if selector == target_selector:
                found_selectors.add(signature)
    checks["selectors"] = sorted(found_selectors)

    found_events = set()
    for entry in abi_entries:
        event_hash = event_hash_from_abi_entry(entry)
        if event_hash is None:
            continue
        for signature, target_hash in EVENT_SIGNATURES.items():
            if event_hash == target_hash:
                found_events.add(signature)
    checks["events"] = sorted(found_events)

    text_blob = "\n".join(sources + [contract_meta.get("ContractName", ""), abi_raw])
    lower_blob = text_blob.lower()
    found_keywords = sorted({k for k in KEYWORDS if k.lower() in lower_blob})
    checks["keywords"] = found_keywords

    checks["matched"] = bool(found_selectors or found_events or found_keywords)
    return checks


def http_get(url: str, params: Dict[str, str], timeout: float = 20.0) -> Dict:
    query = urllib.parse.urlencode(params)
    full_url = f"{url}?{query}"
    with urllib.request.urlopen(full_url, timeout=timeout) as response:
        content = response.read()
    return json.loads(content.decode("utf-8"))


def fetch_contract_metadata(config: NetworkConfig, address: str, timeout: float, retries: int, backoff: float) -> Optional[Dict]:
    params = {
        "module": "contract",
        "action": "getsourcecode",
        "address": address,
    }
    params.update(config.extra_params)
    if config.api_key:
        params["apikey"] = config.api_key

    attempt = 0
    while True:
        try:
            data = http_get(config.base_url, params, timeout=timeout)
        except (urllib.error.URLError, TimeoutError) as exc:
            attempt += 1
            if attempt > retries:
                print(f"[!] Failed to fetch {address} on {config.name}: {exc}", file=sys.stderr)
                return None
            sleep = backoff * (2 ** (attempt - 1))
            time.sleep(sleep)
            continue
        except json.JSONDecodeError as exc:
            print(f"[!] Invalid JSON for {address} on {config.name}: {exc}", file=sys.stderr)
            return None
        else:
            break

    status = data.get("status")
    if status != "1":
        result = data.get("result")
        print(f"[!] Explorer error for {address} on {config.name}: {result}", file=sys.stderr)
        return None

    result = data.get("result")
    if not isinstance(result, list) or not result:
        return None
    return result[0]


def parse_address_argument(value: str) -> Tuple[str, str]:
    if ":" not in value:
        raise argparse.ArgumentTypeError("Address must be provided as network:address")
    network, address = value.split(":", 1)
    network = network.strip().lower()
    address = address.strip()
    if not network or not address:
        raise argparse.ArgumentTypeError("Network and address must be non-empty")
    return network, address


def load_addresses(args: argparse.Namespace) -> Dict[str, List[str]]:
    addresses: Dict[str, List[str]] = {}

    if args.address:
        for network, address in map(parse_address_argument, args.address):
            addresses.setdefault(network, []).append(address)

    if args.address_file:
        for value in args.address_file:
            if ":" not in value:
                raise argparse.ArgumentTypeError("Address file must be network:path")
            network, path = value.split(":", 1)
            network = network.strip().lower()
            path = path.strip()
            with open(path, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith("#"):
                        continue
                    addresses.setdefault(network, []).append(line)

    if args.address_json:
        with open(args.address_json, "r", encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, dict):
            raise argparse.ArgumentTypeError("Address JSON must map network to list of addresses")
        for network, items in data.items():
            if isinstance(items, str) or not isinstance(items, Iterable):
                raise argparse.ArgumentTypeError("Address JSON values must be lists of addresses")
            addresses.setdefault(network.lower(), []).extend(str(item) for item in items)

    return addresses


def resolve_api_key(network: str, cli_keys: Dict[str, str]) -> Optional[str]:
    if network in cli_keys:
        return cli_keys[network]
    for env_key in NETWORK_ENV_KEYS.get(network, []):
        if env_key in os.environ and os.environ[env_key]:
            return os.environ[env_key]
    return None


def build_network_configs(args: argparse.Namespace, addresses: Dict[str, List[str]]) -> Dict[str, NetworkConfig]:
    cli_keys = {}
    if args.api_key:
        for pair in args.api_key:
            network, key = parse_address_argument(pair)
            cli_keys[network] = key

    base_overrides: Dict[str, str] = {}
    if args.base_url:
        for pair in args.base_url:
            if ":" not in pair:
                raise ValueError("Base URL override must be formatted as network:url")
            n, url = pair.split(":", 1)
            base_overrides[n.strip().lower()] = url.strip()

    configs: Dict[str, NetworkConfig] = {}
    for network in addresses:
        base_url = base_overrides.get(network, DEFAULT_NETWORKS.get(network))
        if not base_url:
            raise ValueError(f"No base URL configured for network '{network}'. Use --base-url to provide one.")
        api_key = resolve_api_key(network, cli_keys)
        configs[network] = NetworkConfig(name=network, base_url=base_url, api_key=api_key)
    return configs


def collect_findings(args: argparse.Namespace) -> Dict[str, List[Dict]]:
    addresses = load_addresses(args)
    if not addresses:
        raise SystemExit("No contract addresses provided. Use --address/--address-file/--address-json.")

    configs = build_network_configs(args, addresses)
    findings: Dict[str, List[Dict]] = {network: [] for network in addresses}

    for network, addr_list in addresses.items():
        config = configs[network]
        for address in addr_list:
            metadata = fetch_contract_metadata(
                config,
                address,
                timeout=args.timeout,
                retries=args.retries,
                backoff=args.backoff,
            )
            if metadata is None:
                continue
            indicators = detect_indicators(metadata)
            if not args.include_non_matches and not indicators["matched"]:
                continue
            findings[network].append(
                {
                    "address": address,
                    "contractName": metadata.get("ContractName"),
                    "compilerVersion": metadata.get("CompilerVersion"),
                    "proxy": metadata.get("Proxy"),
                    "implementation": metadata.get("Implementation"),
                    "sourceLastVerified": metadata.get("LastVerified"),
                    "indicators": indicators,
                }
            )
    return findings


def output_results(findings: Dict[str, List[Dict]], args: argparse.Namespace) -> None:
    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            json.dump(findings, f, indent=2)
    else:
        print(json.dumps(findings, indent=2))


def parse_arguments(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Scan verified contracts for UserProofHub indicators.")
    parser.add_argument(
        "--address",
        action="append",
        metavar="NETWORK:ADDRESS",
        help="Contract address to scan. Can be repeated.",
    )
    parser.add_argument(
        "--address-file",
        action="append",
        metavar="NETWORK:PATH",
        help="File containing addresses (one per line) for the given network.",
    )
    parser.add_argument(
        "--address-json",
        help="JSON file mapping network names to lists of addresses.",
    )
    parser.add_argument(
        "--api-key",
        action="append",
        metavar="NETWORK:KEY",
        help="API key to use for a specific network.",
    )
    parser.add_argument(
        "--base-url",
        action="append",
        metavar="NETWORK:URL",
        help="Override the explorer base URL for a network.",
    )
    parser.add_argument("--timeout", type=float, default=15.0, help="HTTP timeout in seconds (default: 15)")
    parser.add_argument("--retries", type=int, default=2, help="Number of retries for failed requests.")
    parser.add_argument(
        "--backoff",
        type=float,
        default=0.5,
        help="Initial backoff delay in seconds for retry attempts (default: 0.5).",
    )
    parser.add_argument(
        "--include-non-matches",
        action="store_true",
        help="Include contracts that do not match any indicators in the output.",
    )
    parser.add_argument("--output", help="Path to write the findings JSON. Defaults to stdout.")
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_arguments(argv)
    try:
        findings = collect_findings(args)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    except SystemExit as exc:
        print(exc, file=sys.stderr)
        return 1

    output_results(findings, args)
    return 0


if __name__ == "__main__":
    sys.exit(main())
