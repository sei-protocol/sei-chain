#!/usr/bin/env python3
"""Write a kvrepair file from the values a reserve node holds at one height.

The key list is one entry per line: a store name, a hex key, and an optional
expected current value (hex, or "absent"). Lines starting with # are ignored.

    evm 03<address><slot>
    evm 03<address><slot> de...ad
    evm 03<address><slot> absent

For each key the script reads the reserve value at --height and writes it as
the entry's target. When a line has no expected value and --prod is given, the
production node's value at --height becomes the expectation. That read goes to
the production state store, so it can differ from what the production commit
store holds; give the expectation on the line when the investigation read it
from the commit store.

Example:

    scripts/kvrepair-export.py --chain-id arctic-1 --name arctic-1-evm-187000000 \\
        --reserve http://localhost:26657 --prod http://localhost:36657 \\
        --height 186999000 --repair-height 187000000 --keys keys.txt \\
        -o app/upgrades/kvrepair/repairs/arctic-1-evm-187000000.json
"""

import argparse
import base64
import json
import sys
import urllib.request

REQUEST_TIMEOUT_SECONDS = 30
ABSENT = "absent"


class RPCError(Exception):
    pass


class RPC:
    def __init__(self, url):
        self.url = url.rstrip("/")

    def _call(self, method, params):
        request = urllib.request.Request(
            self.url,
            data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode(),
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(request, timeout=REQUEST_TIMEOUT_SECONDS) as resp:
            body = json.load(resp)
        if body.get("error"):
            raise RPCError(f"{self.url} {method}: {body['error']}")
        return body["result"]

    def network(self):
        return self._call("status", {})["node_info"]["network"]

    def get(self, store, key, height):
        """Returns the value of key in store at height, or None when absent."""
        result = self._call("abci_query", {
            "path": f"/store/{store}/key",
            "data": key.hex(),
            "height": str(height),
            "prove": False,
        })
        response = result["response"]
        if int(response.get("code", 0)) != 0:
            raise RPCError(f"{self.url} store {store} key {key.hex()} at {height}: {response.get('log')}")
        value = response.get("value")
        return base64.b64decode(value) if value else None


def parse_hex(text, what, lineno):
    try:
        return bytes.fromhex(text.removeprefix("0x"))
    except ValueError:
        raise ValueError(f"line {lineno}: {what} is not hex: {text}") from None


def parse_keys(lines):
    """Returns (store, key, expect) tuples; expect is None, ABSENT, or bytes."""
    keys = []
    for lineno, raw in enumerate(lines, 1):
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        fields = line.split()
        if len(fields) not in (2, 3):
            raise ValueError(f"line {lineno}: want 'store key [expect]', got {raw.strip()!r}")
        key = parse_hex(fields[1], "key", lineno)
        if not key:
            raise ValueError(f"line {lineno}: key is empty")
        expect = None
        if len(fields) == 3:
            expect = ABSENT if fields[2] == ABSENT else parse_hex(fields[2], "expect", lineno)
        keys.append((fields[0], key, expect))
    return keys


def build_repair(name, chain_id, repair_height, height, source, reserve, prod, keys, warn):
    entries = []
    for store, key, expect in keys:
        value = reserve.get(store, key, height)
        if expect is None and prod is not None:
            current = prod.get(store, key, height)
            if current == value:
                warn(f"store {store} key {key.hex()}: production state store agrees with the reserve; "
                     "entry has no expectation")
            else:
                expect = ABSENT if current is None else current
        if expect is not None and expect == (ABSENT if value is None else value):
            warn(f"store {store} key {key.hex()}: expected value equals the reserve value; entry is a no-op")
        entry = {"store": store, "key": key.hex(), "value": None if value is None else value.hex()}
        if expect == ABSENT:
            entry["expect_absent"] = True
        elif expect is not None:
            entry["expect"] = expect.hex()
        entries.append(entry)
    return {
        "name": name,
        "chain_id": chain_id,
        "height": repair_height,
        "source": source,
        "entries": entries,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--chain-id", required=True)
    parser.add_argument("--name", required=True, help="repair name; the handler is kvrepair-<name>")
    parser.add_argument("--reserve", required=True, help="reserve node Tendermint RPC URL")
    parser.add_argument("--prod", help="production node Tendermint RPC URL, for expected values")
    parser.add_argument("--height", type=int, required=True, help="height to read the values at")
    parser.add_argument("--repair-height", type=int, required=True, help="height the repair runs at")
    parser.add_argument("--keys", required=True, help="key list file, or - for stdin")
    parser.add_argument("--source", help="text for the source field (default: the reserve URL and height)")
    parser.add_argument("-o", "--output", default="-", help="output file, or - for stdout")
    args = parser.parse_args(argv)

    if args.repair_height <= args.height:
        parser.error("--repair-height must be above --height")

    def warn(message):
        print(f"warning: {message}", file=sys.stderr)

    try:
        with (sys.stdin if args.keys == "-" else open(args.keys)) as f:
            keys = parse_keys(f)
        if not keys:
            parser.error("the key list is empty")
        reserve = RPC(args.reserve)
        prod = RPC(args.prod) if args.prod else None
        for node in filter(None, (reserve, prod)):
            network = node.network()
            if network != args.chain_id:
                raise RPCError(f"{node.url} is on chain {network}, not {args.chain_id}")
        repair = build_repair(
            args.name, args.chain_id, args.repair_height, args.height,
            args.source or f"{args.reserve} at {args.height}",
            reserve, prod, keys, warn,
        )
    except (OSError, ValueError, RPCError) as e:
        print(f"error: {e}", file=sys.stderr)
        return 1

    text = json.dumps(repair, indent=2) + "\n"
    if args.output == "-":
        sys.stdout.write(text)
    else:
        with open(args.output, "w") as f:
            f.write(text)
    print(f"wrote {len(repair['entries'])} entries for height {args.repair_height}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
