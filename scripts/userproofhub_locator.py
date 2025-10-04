"""UserProofHub deployment locator.

This utility scans JSON-RPC endpoints for the `ProofVerified(address,bytes32)`
event that is emitted by the Zendity/Ava Labs "UserProofHub" contract. Any
contract address that emits this event is collected. Optionally, the script can
hydrate metadata for each contract using the same explorer lookups that power
``userproofhub_scanner.py`` so analysts can immediately review names, compiler
versions, and indicator matches.

Example usage::

    python scripts/userproofhub_locator.py \
        --rpc avalanche:https://api.avax.network/ext/bc/C/rpc \
        --from-block 29300000 --to-block latest \
        --chunk-size 250000 \
        --include-metadata \
        --output ava_userproofhub_deployments.json

Multiple ``--rpc`` flags can be supplied to scan several networks in a single
invocation. When ``--include-metadata`` is enabled the script will reach out to
the configured explorer API (the defaults mirror ``userproofhub_scanner.py``)
and enrich the results with contract metadata and indicator matches.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Dict, Iterable, List, Mapping, Optional, Sequence, Set, Tuple

import importlib.util


def _load_scanner_module():
    """Dynamically import ``userproofhub_scanner.py`` for shared helpers."""

    scanner_path = Path(__file__).with_name("userproofhub_scanner.py")
    spec = importlib.util.spec_from_file_location("userproofhub_scanner", scanner_path)
    if spec is None or spec.loader is None:
        raise ImportError(f"Unable to locate userproofhub_scanner.py at {scanner_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


_scanner = _load_scanner_module()


# Re-export commonly used pieces from the main scanner implementation.
EVENT_SIGNATURES = _scanner.EVENT_SIGNATURES
DEFAULT_EXPLORERS = _scanner.DEFAULT_NETWORKS
NetworkConfig = _scanner.NetworkConfig
detect_indicators = _scanner.detect_indicators
fetch_contract_metadata = _scanner.fetch_contract_metadata


EVENT_TOPIC = EVENT_SIGNATURES["ProofVerified(address,bytes32)"]


class LocatorError(RuntimeError):
    """Base exception for locator failures."""


def _parse_network_mapping(entries: Optional[Iterable[str]]) -> Dict[str, str]:
    mapping: Dict[str, str] = {}
    if not entries:
        return mapping
    for entry in entries:
        if ":" not in entry:
            raise argparse.ArgumentTypeError("Entry must be formatted as network:value")
        network, value = entry.split(":", 1)
        mapping[network.strip().lower()] = value.strip()
    return mapping


def _hex_block(value: int) -> str:
    return hex(int(value))


def _rpc_request(url: str, method: str, params: Sequence, timeout: float) -> Mapping:
    payload = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": list(params)}).encode("utf-8")
    request = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            data = json.loads(response.read().decode("utf-8"))
    except (urllib.error.URLError, TimeoutError) as exc:
        raise LocatorError(f"RPC request to {url} failed: {exc}") from exc

    if "error" in data and data["error"]:
        raise LocatorError(f"RPC error from {url}: {data['error']}")
    return data


def _rpc_result(url: str, method: str, params: Sequence, timeout: float) -> Mapping:
    data = _rpc_request(url, method, params, timeout)
    result = data.get("result")
    if result is None:
        raise LocatorError(f"RPC response from {url} missing 'result': {data}")
    return result


def _resolve_block_height(rpc_url: str, target: str | int, timeout: float) -> int:
    if isinstance(target, int):
        return target
    normalized = str(target).lower()
    if normalized == "latest":
        result = _rpc_result(rpc_url, "eth_blockNumber", [], timeout)
        return int(result, 16)
    base = 16 if normalized.startswith("0x") else 10
    return int(normalized, base)


def _chunk_ranges(start: int, end: int, chunk: int) -> Iterable[Tuple[int, int]]:
    current = start
    while current <= end:
        upper = min(current + chunk - 1, end)
        yield current, upper
        current = upper + 1


def _collect_logs(
    rpc_url: str,
    from_block: int,
    to_block: int,
    chunk_size: int,
    timeout: float,
    delay: float,
    max_logs: Optional[int],
) -> Tuple[List[Mapping], Set[str]]:
    logs: List[Mapping] = []
    addresses: Set[str] = set()

    for lower, upper in _chunk_ranges(from_block, to_block, chunk_size):
        params = [
            {
                "fromBlock": _hex_block(lower),
                "toBlock": _hex_block(upper),
                "topics": [EVENT_TOPIC],
            }
        ]
        result = _rpc_result(rpc_url, "eth_getLogs", params, timeout)
        if not isinstance(result, list):
            raise LocatorError(f"Unexpected eth_getLogs response: {result}")
        for entry in result:
            address = entry.get("address")
            if isinstance(address, str):
                addresses.add(address.lower())
            logs.append(entry)
            if max_logs is not None and len(logs) >= max_logs:
                return logs, addresses
        if delay:
            time.sleep(delay)
    return logs, addresses


def _fetch_metadata(
    network: str,
    addresses: Iterable[str],
    explorer_url: str,
    api_key: Optional[str],
    timeout: float,
    retries: int,
    backoff: float,
) -> List[Mapping[str, object]]:
    config = NetworkConfig(name=network, base_url=explorer_url, api_key=api_key)
    enriched: List[Mapping[str, object]] = []
    for address in sorted({addr.lower() for addr in addresses}):
        metadata = fetch_contract_metadata(config, address, timeout=timeout, retries=retries, backoff=backoff)
        if metadata is None:
            continue
        indicators = detect_indicators(metadata)
        enriched.append(
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
    return enriched


def parse_arguments(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Locate UserProofHub deployments via ProofVerified logs.")
    parser.add_argument(
        "--rpc",
        action="append",
        required=True,
        metavar="NETWORK:URL",
        help="JSON-RPC endpoint to scan (can be supplied multiple times).",
    )
    parser.add_argument("--from-block", default=0, help="Starting block number (decimal or hex). Default: 0")
    parser.add_argument(
        "--to-block",
        default="latest",
        help="Ending block number (decimal, hex, or 'latest'). Default: latest",
    )
    parser.add_argument(
        "--chunk-size",
        type=int,
        default=250_000,
        help="Number of blocks per eth_getLogs request (default: 250000).",
    )
    parser.add_argument(
        "--rpc-timeout",
        type=float,
        default=20.0,
        help="Timeout in seconds for RPC requests (default: 20).",
    )
    parser.add_argument(
        "--chunk-delay",
        type=float,
        default=0.0,
        help="Optional delay in seconds between chunked requests.",
    )
    parser.add_argument(
        "--max-logs",
        type=int,
        help="Stop after collecting this many logs per network (useful for sampling).",
    )
    parser.add_argument(
        "--include-logs",
        action="store_true",
        help="Retain the raw log entries in the JSON output.",
    )
    parser.add_argument(
        "--include-metadata",
        action="store_true",
        help="Fetch explorer metadata and indicator matches for discovered addresses.",
    )
    parser.add_argument(
        "--explorer",
        action="append",
        metavar="NETWORK:URL",
        help="Override the explorer base URL used for metadata hydration.",
    )
    parser.add_argument(
        "--api-key",
        action="append",
        metavar="NETWORK:KEY",
        help="Explorer API key for metadata hydration.",
    )
    parser.add_argument(
        "--explorer-timeout",
        type=float,
        default=15.0,
        help="Explorer HTTP timeout in seconds (default: 15).",
    )
    parser.add_argument(
        "--explorer-retries",
        type=int,
        default=2,
        help="Number of retries for explorer metadata fetches (default: 2).",
    )
    parser.add_argument(
        "--explorer-backoff",
        type=float,
        default=0.5,
        help="Initial backoff (seconds) between explorer retries (default: 0.5).",
    )
    parser.add_argument(
        "--output",
        default="userproofhub_deployments.json",
        help="Destination file for the JSON report (default: userproofhub_deployments.json).",
    )
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_arguments(argv)

    rpc_urls = _parse_network_mapping(args.rpc)
    explorer_overrides = _parse_network_mapping(args.explorer)
    explorer_keys = _parse_network_mapping(args.api_key)

    try:
        _resolve_block_height(next(iter(rpc_urls.values())), args.from_block, args.rpc_timeout)
    except (LocatorError, StopIteration, ValueError) as exc:
        print(f"error: unable to resolve from-block: {exc}", file=sys.stderr)
        return 1

    results: Dict[str, Dict[str, object]] = {
        "eventTopic": EVENT_TOPIC,
        "networks": {},
    }

    for network, rpc_url in rpc_urls.items():
        try:
            network_from = _resolve_block_height(rpc_url, args.from_block, args.rpc_timeout)
            network_to = _resolve_block_height(rpc_url, args.to_block, args.rpc_timeout)
        except (LocatorError, ValueError) as exc:
            print(f"error: failed to resolve block bounds for {network}: {exc}", file=sys.stderr)
            continue

        if network_to < network_from:
            print(f"warning: to-block < from-block for {network}; skipping.", file=sys.stderr)
            continue

        try:
            logs, addresses = _collect_logs(
                rpc_url,
                network_from,
                network_to,
                args.chunk_size,
                args.rpc_timeout,
                args.chunk_delay,
                args.max_logs,
            )
        except LocatorError as exc:
            print(f"error: failed to collect logs for {network}: {exc}", file=sys.stderr)
            continue

        network_result: Dict[str, object] = {
            "rpc": rpc_url,
            "fromBlock": network_from,
            "toBlock": network_to,
            "addresses": sorted(addresses),
        }

        if args.include_logs:
            network_result["logs"] = logs

        if args.include_metadata and addresses:
            explorer_url = explorer_overrides.get(network, DEFAULT_EXPLORERS.get(network))
            if not explorer_url:
                print(
                    f"warning: no explorer configured for {network}; skipping metadata hydration.",
                    file=sys.stderr,
                )
            else:
                api_key = explorer_keys.get(network)
                enriched = _fetch_metadata(
                    network,
                    addresses,
                    explorer_url,
                    api_key,
                    timeout=args.explorer_timeout,
                    retries=args.explorer_retries,
                    backoff=args.explorer_backoff,
                )
                network_result["contracts"] = enriched

        results["networks"][network] = network_result

    Path(args.output).write_text(json.dumps(results, indent=2))
    print(f"✅ Saved locator report to {args.output}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
