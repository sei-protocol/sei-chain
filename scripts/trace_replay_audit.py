#!/usr/bin/env python3
"""Find EVM transactions whose debug_trace* output from a pre-v6.7.4 node is wrong.

Before v6.7.4 and v6.8.0-rc3, trace replay rebuilt a block's start state from only
the first page (30) of the parent height's validator set. With a larger set
(pacific-1 has 40), every validator's reward allocation in the replay differs from
the real one, so a trace that reads distribution, staking reward or slashing state
can differ from what really executed, including internal calls that never happened.

trace mode (default) replays each block in the range on --rpc, an archive node
with debug_* enabled, and reports every EVM transaction that

  receipt_mismatch     disagrees with its receipt on gas used, status, or logs
                       (precompile logs excluded, replay does not report them):
                       the trace is wrong;
  reads_reward_state   calls the staking, distribution or slashing precompile at
                       any depth: the trace may be wrong even when gas and logs
                       agree;
  calls_wasm           calls the wasmd precompile (only with --include-wasm);
  same_block_followup  comes later in the same block and touches an address a
                       transaction reported above touched;
  receipt_missing      has no receipt on --rpc, so it could not be checked.

With --fixed-rpc, a node running v6.7.4+ that serves no trace cached by an earlier
release, every reported transaction is traced there as well and its internal
value transfers are diffed (phantom: only in the --rpc trace; missing: only in
the fixed trace).

logs mode lists transactions that emitted staking or distribution precompile
events, using eth_getLogs only. It works against pruned nodes, but those events
exist only from v6.4.0 and read-only precompile calls emit none, so it is a lower
bound on the affected set, not the set.

Output is one JSON object per reported transaction (JSON Lines), ordered by block
and index. Progress and a final summary go to stderr.

Examples:
  scripts/trace_replay_audit.py --rpc http://archive:8545 \\
      --tendermint-rpc http://archive:26657 --from 79123881 --to 242700000 \\
      --workers 16 --checkpoint audit.ckpt --out affected.jsonl
  scripts/trace_replay_audit.py --rpc http://archive:8545 --rescan affected.jsonl \\
      --out affected.jsonl
  scripts/trace_replay_audit.py --rpc http://archive-v673:8545 \\
      --fixed-rpc http://archive-v674:8545 --from 241565186 --to 241565189
  scripts/trace_replay_audit.py --mode logs --rpc https://evm-rpc.sei-apis.com \\
      --from 242200000 --to 242700000
"""

import argparse
import json
import os
import sys
import threading
import time
import urllib.error
import urllib.request
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
from decimal import Decimal

STAKING = "0x0000000000000000000000000000000000001005"
DISTRIBUTION = "0x0000000000000000000000000000000000001007"
SLASHING = "0x0000000000000000000000000000000000001014"
WASMD = "0x0000000000000000000000000000000000001002"

PRECOMPILE_NAMES = {
    "0x0000000000000000000000000000000000001001": "bank",
    WASMD: "wasmd",
    "0x0000000000000000000000000000000000001003": "json",
    "0x0000000000000000000000000000000000001004": "addr",
    STAKING: "staking",
    "0x0000000000000000000000000000000000001006": "gov",
    DISTRIBUTION: "distribution",
    "0x0000000000000000000000000000000000001008": "oracle",
    "0x0000000000000000000000000000000000001009": "ibc",
    "0x000000000000000000000000000000000000100a": "pointerview",
    "0x000000000000000000000000000000000000100b": "pointer",
    "0x000000000000000000000000000000000000100c": "solo",
    "0x000000000000000000000000000000000000100d": "auth",
    "0x000000000000000000000000000000000000100e": "authz",
    "0x000000000000000000000000000000000000100f": "evidence",
    "0x0000000000000000000000000000000000001011": "p256",
    "0x0000000000000000000000000000000000001012": "mint",
    "0x0000000000000000000000000000000000001013": "params",
    SLASHING: "slashing",
    "0x0000000000000000000000000000000000001015": "upgrade",
}

# Precompiles that read state the faulty BeginBlock allocated differently.
REWARD_STATE_PRECOMPILES = {"staking", "distribution", "slashing"}

EVENT_NAMES = {
    "0x00e1ef0aa5cbcdc2ae3ce2840984590ab429a9579c9a4b89ca93d77105d40e4a": "Delegate",
    "0xe9a2d1cd042608da4ebdf1fafb4a4658dfc6954ce121138e784b3f9372505d11": "DelegationRewardsWithdrawn",
    "0x1b26c9b9360749ef957cd7ebe494c66814ab181ada52377ae3036fa24e9befa7": "Redelegate",
    "0xc5c77bd61c771d282f85e9d57c074cc5da688158db5f4422a376569a2c69a573": "Undelegate",
    "0xb07ac1a8f86c67f4f2d948b6d93c8cedb3a821e5ee79475a9bbc1269a8753f37": "ValidatorCreated",
    "0x3ac1616ab72092b243fb318e94d11451de40a59822d4afc8bbb58f74710da225": "ValidatorEdited",
    "0xded60ee1fd7214fb7c36e0d0a674961f0b3a146cee8b2a5dfac9e524da628439": "MultipleDelegationRewardsWithdrawn",
    "0xe40546e817dc4728b4185567545ed4559d2f8df7d691e4e9275c4668144701f7": "ValidatorCommissionWithdrawn",
    "0x81ef977572555fac5ac57a0e17fdf5feaf98f544e9af11713615da4e1f34ff0b": "WithdrawAddressSet",
}

# Page size of the single validators page the faulty replay loaded.
FAULTY_VALIDATOR_PAGE = 30

VALUE_FRAME_TYPES = {"CALL", "CALLCODE", "CREATE", "CREATE2", "SELFDESTRUCT"}
CALL_TRACER = {"tracer": "callTracer", "tracerConfig": {"withLog": True}}
WEI_PER_SEI = Decimal(10) ** 18
ZERO_ADDRESS = "0x" + "0" * 40


class RPCError(Exception):
    """A JSON-RPC or HTTP failure that retrying will not fix."""


class RateLimiter:
    """Spaces calls so that at most rps start per second; 0 disables it."""

    def __init__(self, rps):
        self.interval = 1.0 / rps if rps > 0 else 0.0
        self.lock = threading.Lock()
        self.next_at = time.monotonic()

    def wait(self):
        if not self.interval:
            return
        with self.lock:
            now = time.monotonic()
            start = max(now, self.next_at)
            self.next_at = start + self.interval
        if start > now:
            time.sleep(start - now)


class Endpoint:
    """JSON-RPC (POST) and Tendermint (GET) client with retry and rate limiting."""

    RETRYABLE_HTTP = {408, 425, 429, 500, 502, 503, 504}
    RETRYABLE_RPC_CODES = {-32005}
    RETRYABLE_RPC_MESSAGES = (
        "server busy", "concurrency limit", "saturated", "rejecting new request", "rate limit", "too many requests", "try again",
    )

    def __init__(self, url, rps, timeout, retries):
        self.url = url.rstrip("/")
        self.limiter = RateLimiter(rps)
        self.timeout = timeout
        self.retries = retries

    def call(self, method, params):
        payload = {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
        reply = self._request(self.url, json.dumps(payload).encode())
        if "error" in reply:
            self._raise(method, reply["error"])
        return reply.get("result")

    def _raise(self, method, err):
        message = str(err.get("message", err))
        if err.get("code") in self.RETRYABLE_RPC_CODES or any(m in message.lower() for m in self.RETRYABLE_RPC_MESSAGES):
            raise _Retry(f"{method}: {message}")
        raise RPCError(f"{method}: {message}")

    def call_with_retry(self, method, params):
        return self._retry(lambda: self.call(method, params))

    def batch_with_retry(self, method, params_list):
        """Returns results in order; raises RPCError if the endpoint rejects batches."""

        def once():
            payload = [{"jsonrpc": "2.0", "id": i, "method": method, "params": p} for i, p in enumerate(params_list)]
            reply = self._request(self.url, json.dumps(payload).encode())
            if not isinstance(reply, list) or len(reply) != len(params_list):
                raise RPCError(f"{method}: batch not supported")
            reply.sort(key=lambda r: r.get("id", 0))
            out = []
            for r in reply:
                if "error" in r:
                    self._raise(method, r["error"])
                out.append(r.get("result"))
            return out

        return self._retry(once)

    def get_with_retry(self, path):
        """GETs a Tendermint RPC path and returns its result object."""

        def once():
            reply = self._request(self.url + path, None)
            if not isinstance(reply, dict):
                raise RPCError(f"GET {path}: unexpected reply {str(reply)[:200]}")
            err = reply.get("error") or (reply if "code" in reply and "message" in reply else None)
            if err:
                err = err if isinstance(err, dict) else {"message": err}
                self._raise(f"GET {path}", {**err, "message": f"{err.get('message')} {err.get('data') or ''}".strip()})
            return reply.get("result", reply)

        return self._retry(once)

    def _request(self, url, body):
        self.limiter.wait()
        headers = {"content-type": "application/json", "user-agent": "sei-trace-replay-audit/1"}
        req = urllib.request.Request(url, data=body, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                return json.load(resp)
        except urllib.error.HTTPError as e:
            if e.code in self.RETRYABLE_HTTP:
                raise _Retry(f"HTTP {e.code} from {url}")
            raise RPCError(f"HTTP {e.code} from {url}")
        except (urllib.error.URLError, TimeoutError, ConnectionError, json.JSONDecodeError) as e:
            raise _Retry(f"{type(e).__name__} from {url}: {e}")

    def _retry(self, fn):
        delay = 1.0
        for attempt in range(self.retries + 1):
            try:
                return fn()
            except _Retry as e:
                if attempt == self.retries:
                    raise RPCError(str(e))
                time.sleep(delay)
                delay = min(delay * 2, 30.0)
        raise AssertionError("unreachable")


class _Retry(Exception):
    pass


def lower(addr):
    return (addr or "").lower()


def precompile_name(addr):
    return PRECOMPILE_NAMES.get(lower(addr))


def walk(frame, depth=0):
    yield depth, frame
    for child in frame.get("calls") or []:
        yield from walk(child, depth + 1)


def hex_int(value):
    return int(value or "0x0", 16)


def log_key(log):
    return (lower(log.get("address")), tuple(lower(t) for t in log.get("topics") or []), lower(log.get("data")))


def trace_logs(frame):
    return [log for _, f in walk(frame) for log in f.get("logs") or []]


def internal_transfers(frame):
    """Value-carrying frames below the top call, i.e. what explorers list as internal transactions."""
    out = []
    for depth, f in walk(frame):
        value = hex_int(f.get("value"))
        if depth == 0 or not value or f.get("type") not in VALUE_FRAME_TYPES:
            continue
        out.append({
            "depth": depth,
            "type": f.get("type"),
            "from": lower(f.get("from")),
            "to": lower(f.get("to")),
            "value_wei": str(value),
            "value_sei": str(Decimal(value) / WEI_PER_SEI),
            "error": f.get("error"),
        })
    return out


def transfer_key(t):
    return (t["type"], t["from"], t["to"], t["value_wei"], bool(t["error"]))


def call_tree(frame):
    return [
        (d, f.get("type"), lower(f.get("from")), lower(f.get("to")), hex_int(f.get("value")),
         lower(f.get("input")), lower(f.get("output")), bool(f.get("error")))
        for d, f in walk(frame)
    ]


def touched(frame):
    """Returns (precompile names called, non-precompile addresses seen) for a call tree."""
    precompiles, addresses = set(), set()
    for _, f in walk(frame):
        for addr in (lower(f.get("from")), lower(f.get("to"))):
            if not addr or addr == ZERO_ADDRESS:
                continue
            name = precompile_name(addr)
            if name:
                precompiles.add(name)
            else:
                addresses.add(addr)
    return precompiles, addresses


def compare_with_receipt(frame, receipt):
    """Returns how a replayed top call disagrees with the receipt of the real execution."""
    if receipt is None:
        return {"receipt": "missing"}
    out = {}
    trace_gas, receipt_gas = hex_int(frame.get("gasUsed")), hex_int(receipt.get("gasUsed"))
    if trace_gas != receipt_gas:
        out["gas_used"] = {"trace": trace_gas, "receipt": receipt_gas}
    trace_ok = not frame.get("error")
    receipt_ok = hex_int(receipt.get("status", "0x1")) == 1
    if trace_ok != receipt_ok:
        out["status"] = {"trace": frame.get("error") or "success", "receipt": "success" if receipt_ok else "reverted"}
    in_trace = Counter(log_key(log) for log in trace_logs(frame) if not precompile_name(log.get("address")))
    in_receipt = Counter(log_key(log) for log in receipt.get("logs") or [] if not precompile_name(log.get("address")))
    if in_trace != in_receipt:
        out["logs"] = {
            "only_in_trace": [_log_json(k) for k in (in_trace - in_receipt).elements()][:5],
            "only_in_receipt": [_log_json(k) for k in (in_receipt - in_trace).elements()][:5],
        }
    return out


def _log_json(key):
    address, topics, data = key
    return {"address": address, "topics": list(topics), "data": data}


def diff_transfers(scanned, fixed):
    a, b = Counter(map(transfer_key, scanned)), Counter(map(transfer_key, fixed))
    by_key = {transfer_key(t): t for t in scanned + fixed}
    return [by_key[k] for k in (a - b).elements()], [by_key[k] for k in (b - a).elements()]


class Audit:
    """Shared state for one run: endpoints, options and the summary counters."""

    def __init__(self, args):
        self.args = args
        self.rpc = Endpoint(args.rpc, args.rps, args.timeout, args.retries)
        self.fixed = Endpoint(args.fixed_rpc, args.rps, args.timeout, args.retries) if args.fixed_rpc else None
        self.tendermint = Endpoint(args.tendermint_rpc, args.rps, args.timeout, args.retries) if args.tendermint_rpc else None
        self.lock = threading.Lock()
        self.summary = Counter()

    def count(self, key, n=1):
        with self.lock:
            self.summary[key] += n

    # trace mode

    def nonempty_blocks(self, heights):
        """Drops heights whose block holds no EVM transaction; keeps any it cannot count."""
        params = [[hex(h)] for h in heights]
        try:
            counts = []
            for i in range(0, len(params), 100):
                counts += self.rpc.batch_with_retry("eth_getBlockTransactionCountByNumber", params[i:i + 100])
        except RPCError:
            counts = [self.tx_count(p) for p in params]
        keep = [h for h, c in zip(heights, counts) if c is None or hex_int(c) > 0]
        self.count("blocks_without_evm_txs", len(heights) - len(keep))
        return keep

    def tx_count(self, params):
        try:
            return self.rpc.call_with_retry("eth_getBlockTransactionCountByNumber", params)
        except RPCError:
            return None

    def parent_set_fits_one_page(self, height):
        result = self.tendermint.get_with_retry(f"/validators?height={height - 1}&per_page=1")
        if "total" not in result:
            raise RPCError(f"validators at {height - 1}: no total in {str(result)[:200]}")
        return int(result["total"]) <= FAULTY_VALIDATOR_PAGE

    def scan_block(self, height):
        """Returns the report rows for one block, in transaction order."""
        try:
            if self.tendermint and self.parent_set_fits_one_page(height):
                self.count("blocks_skipped_validator_set_fits_one_page")
                return []
            traces = self.rpc.call_with_retry("debug_traceBlockByNumber", [hex(height), CALL_TRACER]) or []
            receipts = []
            if traces:
                receipts = self.rpc.call_with_retry("eth_getBlockReceipts", [hex(height)]) or []
        except (RPCError, ValueError, KeyError, TypeError) as e:
            # Recorded rather than raised so a long scan carries on; --rescan retries these blocks.
            self.count("blocks_failed")
            return [{"block": height, "classes": ["block_failed"], "error": f"{type(e).__name__}: {e}"}]
        self.count("blocks_traced")
        self.count("txs_traced", len(traces))
        by_hash = {lower(r.get("transactionHash")): r for r in receipts}
        rows, tainted = [], set()
        for position, item in enumerate(traces):
            row = self.classify(height, position, item, by_hash, tainted)
            if row:
                rows.append(row)
        return rows

    def classify(self, height, position, item, receipts_by_hash, tainted):
        tx_hash = lower(item.get("txHash"))
        frame = item.get("result")
        receipt = receipts_by_hash.get(tx_hash)
        index = hex_int(receipt["transactionIndex"]) if receipt else position
        if not frame or item.get("error"):
            self.count("txs_trace_failed")
            return {"block": height, "index": index, "hash": tx_hash, "classes": ["trace_failed"], "error": item.get("error")}
        precompiles, addresses = touched(frame)
        mismatch = compare_with_receipt(frame, receipt)
        classes = []
        if receipt is None:
            classes.append("receipt_missing")
        elif mismatch:
            classes.append("receipt_mismatch")
        if precompiles & REWARD_STATE_PRECOMPILES:
            classes.append("reads_reward_state")
        if self.args.include_wasm and "wasmd" in precompiles:
            classes.append("calls_wasm")
        if not classes and addresses & tainted:
            classes.append("same_block_followup")
        if not classes:
            return None
        tainted |= addresses
        for c in classes:
            self.count(c)
        row = {
            "block": height,
            "index": index,
            "hash": tx_hash,
            "from": lower(frame.get("from")),
            "to": lower(frame.get("to")),
            "classes": classes,
            "precompiles": sorted(precompiles),
            "internal_transfers": internal_transfers(frame),
        }
        if mismatch:
            row["mismatch"] = mismatch
        if self.fixed:
            row["fixed"] = self.compare_with_fixed(tx_hash, frame, receipt, row["internal_transfers"])
        return row

    def compare_with_fixed(self, tx_hash, frame, receipt, scanned_transfers):
        try:
            fixed = self.fixed.call_with_retry("debug_traceTransaction", [tx_hash, CALL_TRACER])
        except RPCError as e:
            self.count("fixed_trace_failed")
            return {"error": str(e)}
        if not fixed:
            self.count("fixed_trace_failed")
            return {"error": "no trace returned"}
        fixed_transfers = internal_transfers(fixed)
        phantom, missing = diff_transfers(scanned_transfers, fixed_transfers)
        out = {
            "trace_changed": call_tree(frame) != call_tree(fixed),
            "transfers_changed": bool(phantom or missing),
            "phantom_transfers": phantom,
            "missing_transfers": missing,
        }
        fixed_mismatch = compare_with_receipt(fixed, receipt)
        if fixed_mismatch:
            out["receipt_mismatch"] = fixed_mismatch
        self.count("fixed_trace_changed", int(out["trace_changed"]))
        self.count("fixed_transfers_changed", int(out["transfers_changed"]))
        return out

    def run_trace_chunk(self, pool, heights):
        heights = self.nonempty_blocks(heights)
        rows = []
        for block_rows in pool.map(self.scan_block, heights):
            rows += block_rows
        return rows

    # logs mode

    def scan_log_window(self, window):
        start, end = window
        flt = {"fromBlock": hex(start), "toBlock": hex(end), "address": [STAKING, DISTRIBUTION, SLASHING]}
        try:
            logs = self.rpc.call_with_retry("eth_getLogs", [flt]) or []
        except RPCError as e:
            self.count("windows_failed")
            return [{"block": start, "to_block": end, "classes": ["window_failed"], "error": str(e)}]
        self.count("blocks_scanned", end - start + 1)
        by_tx = {}
        for log in logs:
            tx_hash = lower(log.get("transactionHash"))
            row = by_tx.setdefault(tx_hash, {
                "block": hex_int(log.get("blockNumber")),
                "index": hex_int(log.get("transactionIndex")),
                "hash": tx_hash,
                "classes": ["precompile_event"],
                "events": [],
            })
            topic0 = lower((log.get("topics") or [""])[0])
            row["events"].append({"precompile": precompile_name(log.get("address")), "event": EVENT_NAMES.get(topic0, topic0)})
        rows = list(by_tx.values())
        for row in rows:
            row["to"], row["entry"] = self.entry_point(row)
        self.count("precompile_event_txs", len(rows))
        return rows

    def entry_point(self, row):
        """Returns the transaction's to address and whether it called the precompile directly."""
        lookups = (
            ("eth_getTransactionByBlockNumberAndIndex", [hex(row["block"]), hex(row["index"])]),
            ("eth_getTransactionByHash", [row["hash"]]),
        )
        for method, params in lookups:
            try:
                tx = self.rpc.call_with_retry(method, params)
            except RPCError:
                continue
            if tx and lower(tx.get("hash")) == row["hash"]:
                to = lower(tx.get("to"))
                return to, "direct" if precompile_name(to) else "via_contract"
        self.count("entry_point_unknown")
        return None, "unknown"

    def run_logs_chunk(self, pool, heights):
        rows = []
        for window_rows in pool.map(self.scan_log_window, log_windows(heights, self.args.log_window)):
            rows += window_rows
        return rows


def log_windows(heights, size):
    """Splits sorted heights into runs of consecutive blocks, each at most size long."""
    windows = []
    for h in heights:
        if windows and windows[-1][1] == h - 1 and h - windows[-1][0] < size:
            windows[-1][1] = h
        else:
            windows.append([h, h])
    return [tuple(w) for w in windows]


def failed_heights(path):
    """Returns the blocks a previous run's output reported as failed, sorted."""
    heights = set()
    with open(path) as f:
        for line in f:
            row = json.loads(line)
            if "block_failed" in row["classes"]:
                heights.add(row["block"])
            elif "window_failed" in row["classes"]:
                heights.update(range(row["block"], row["to_block"] + 1))
    return sorted(heights)


def read_checkpoint(path):
    try:
        with open(path) as f:
            return int(json.load(f)["next_block"])
    except FileNotFoundError:
        return None


def write_checkpoint(path, next_block):
    tmp = path + ".tmp"
    with open(tmp, "w") as f:
        json.dump({"next_block": next_block}, f)
    os.replace(tmp, path)


def parse_args(argv):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--mode", choices=("trace", "logs"), default="trace")
    p.add_argument("--rpc", required=True, help="EVM JSON-RPC URL (trace mode: archive with debug_* enabled)")
    p.add_argument("--from", dest="start", type=int, help="first block, inclusive")
    p.add_argument("--to", dest="end", type=int, help="last block, inclusive")
    p.add_argument("--rescan", help="instead of --from/--to, scan the blocks a previous output file reports as failed")
    p.add_argument("--fixed-rpc", help="trace mode: EVM JSON-RPC of a v6.7.4+ node to diff reported transactions against")
    p.add_argument("--tendermint-rpc", help="trace mode: Tendermint RPC used to skip blocks whose parent set has <= 30 validators")
    p.add_argument("--include-wasm", action="store_true", help="trace mode: also report transactions that call the wasmd precompile")
    p.add_argument("--workers", type=int, default=8)
    p.add_argument("--rps", type=float, default=0, help="max requests per second per endpoint (0: unlimited)")
    p.add_argument("--timeout", type=float, default=180, help="seconds per request")
    p.add_argument("--retries", type=int, default=5)
    p.add_argument("--chunk", type=int, default=500, help="blocks per checkpoint")
    p.add_argument("--log-window", type=int, default=2000, help="logs mode: blocks per eth_getLogs call")
    p.add_argument("--checkpoint", help="file recording the next block to scan; resumes from it when present")
    p.add_argument("--out", help="JSON Lines output file (appended); default stdout")
    args = p.parse_args(argv)
    if args.rescan:
        if args.start is not None or args.end is not None or args.checkpoint:
            p.error("--rescan replaces --from, --to and --checkpoint")
    elif args.start is None or args.end is None:
        p.error("--from and --to are required unless --rescan is given")
    elif args.end < args.start:
        p.error("--to is below --from")
    if args.mode == "logs" and (args.fixed_rpc or args.tendermint_rpc or args.include_wasm):
        p.error("--fixed-rpc, --tendermint-rpc and --include-wasm apply to trace mode only")
    return args


def heights_to_scan(args):
    if args.rescan:
        return failed_heights(args.rescan)
    start = args.start
    if args.checkpoint:
        start = max(start, read_checkpoint(args.checkpoint) or start)
    return range(start, args.end + 1)


def main(argv):
    args = parse_args(argv)
    audit = Audit(args)
    heights = heights_to_scan(args)
    out = open(args.out, "a") if args.out else sys.stdout
    run_chunk = audit.run_trace_chunk if args.mode == "trace" else audit.run_logs_chunk
    began = time.monotonic()
    try:
        with ThreadPoolExecutor(max_workers=args.workers) as pool:
            for i in range(0, len(heights), args.chunk):
                chunk = list(heights[i:i + args.chunk])
                rows = run_chunk(pool, chunk)
                rows.sort(key=lambda r: (r["block"], r.get("index", -1)))
                for row in rows:
                    out.write(json.dumps(row) + "\n")
                out.flush()
                if args.checkpoint:
                    write_checkpoint(args.checkpoint, chunk[-1] + 1)
                done = i + len(chunk)
                rate = done / max(time.monotonic() - began, 1e-9)
                counts = dict(sorted(audit.summary.items()))
                print(f"[{args.mode}] through block {chunk[-1]} ({done}/{len(heights)}, {rate:.1f} blocks/s) {counts}", file=sys.stderr)
    finally:
        if args.out:
            out.close()
    print(json.dumps({"mode": args.mode, "from": args.start, "to": args.end, "rescan": args.rescan,
                      "summary": dict(audit.summary)}, indent=2), file=sys.stderr)
    return 1 if audit.summary["blocks_failed"] or audit.summary["windows_failed"] else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
