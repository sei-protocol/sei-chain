#!/usr/bin/env python3
"""End-to-end checks for sei-chain giga-1's minimal EVM RPC. Sends real test transactions."""
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import time
import urllib.request

RPC = os.environ.get("EVM_RPC", "http://127.0.0.1:18546")
KEY = os.environ["EVM_KEY"]
FROM = os.environ["EVM_FROM"].lower()
CHAIN = int(os.environ["EVM_CHAIN_ID"], 0)
TIMEOUT = int(os.environ.get("EVM_CHECK_TIMEOUT", "90"))
OUT = Path("giga-check-" + time.strftime("%Y%m%d-%H%M%S") + "-" + secrets.token_hex(2))
OUT.mkdir()


def check(condition, message):
    if not condition:
        raise AssertionError(message)


def rpc_response(method, *params):
    body = json.dumps(dict(jsonrpc="2.0", id=1, method=method, params=params)).encode()
    req = urllib.request.Request(RPC, body, {"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=15) as response:
        return json.load(response)


def rpc(method, *params):
    response = rpc_response(method, *params)
    check("error" not in response, f"{method}: {response.get('error')}")
    return response["result"]


def number(value):
    return int(value, 16)


def balance(address):
    return number(rpc("eth_getBalance", address, "latest"))


def nonce():
    return number(rpc("eth_getTransactionCount", FROM, "latest"))


def cast(*args):
    result = subprocess.run(["rtk", "proxy", "cast", *map(str, args)],
                            capture_output=True, text=True, timeout=TIMEOUT)
    check(result.returncode == 0, result.stderr.replace(KEY, "[REDACTED]"))
    return result.stdout.strip()


def transaction_hash(output):
    # Alloy may write background provider diagnostics to stdout before the
    # successful --async result. Only accept a standalone hash line.
    hashes = re.findall(r"^\s*(0x[0-9a-fA-F]{64})\s*$", output, re.MULTILINE)
    check(len(hashes) == 1, f"Expected one transaction hash in cast output: {output}")
    return hashes[0]


def eventually(predicate, message):
    deadline = time.monotonic() + TIMEOUT
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.5)
    raise AssertionError(message)


def receipt(txhash):
    deadline = time.monotonic() + TIMEOUT
    while time.monotonic() < deadline:
        result = rpc("eth_getTransactionReceipt", txhash)
        if result is not None:
            return result
        time.sleep(0.5)
    raise AssertionError(f"Receipt timeout: {txhash}; inspect before retrying")


def send(label, to=None, value=0, gas=100000, data="0x", legacy=False,
         status=1, expected_gas=None, counter=None, init=None):
    before_nonce, before_balance = nonce(), balance(FROM)
    to_balance = balance(to) if to and to.lower() != FROM else None
    args = ["send", "--rpc-url", RPC, "--private-key", KEY,
            "--chain", CHAIN, "--nonce", before_nonce, "--value", value,
            "--gas-limit", gas, "--gas-price", "2gwei", "--async"]
    if legacy:
        args += ["--legacy"]
    else:
        args += ["--priority-gas-price", "1gwei"]
    args += ["--create", init] if init else [to, data]
    output = cast(*args)
    (OUT / (label + "-send.txt")).write_text(output.replace(KEY, "[REDACTED]") + "\n")
    txhash = transaction_hash(output)
    print(f"SENT {label}: {txhash}", flush=True)
    r = receipt(txhash)
    (OUT / (label + ".json")).write_text(json.dumps(r, indent=2) + "\n")
    check(r["transactionHash"].lower() == txhash.lower(), "transactionHash mismatch")
    check(r["from"].lower() == FROM, "from mismatch")
    check(number(r["status"]) == status, f"{label}: unexpected status")
    check(number(r["type"]) == (0 if legacy else 2), "transaction type mismatch")
    used, price = number(r["gasUsed"]), number(r["effectiveGasPrice"])
    check(21000 <= used <= gas, "gasUsed outside bounds")
    if expected_gas is not None:
        check(used == expected_gas, f"gasUsed {used} != {expected_gas}")
    check(price == 2000000000 if legacy else 1000000000 <= price <= 2000000000,
          f"Unexpected effectiveGasPrice: {price}")
    check(number(r["cumulativeGasUsed"]) >= used, "cumulativeGasUsed < gasUsed")
    check(number(r["blockNumber"]) > 0 and number(r["blockHash"]) != 0, "Invalid block reference")
    check(number(r["transactionIndex"]) >= 0, "Negative transaction index")
    if init:
        check(r["to"] is None and r["contractAddress"] is not None, "Invalid creation receipt")
    else:
        check(r["to"].lower() == to.lower() and r["contractAddress"] is None, "Invalid call receipt")
    if counter is None:
        check(r["logs"] == [] and number(r["logsBloom"]) == 0, "Unexpected logs/bloom")
    else:
        check(len(r["logs"]) == 1, "Expected one counter log")
        log = r["logs"][0]
        check(log["address"].lower() == to.lower() and log["topics"] == [], "Wrong log emitter/topics")
        check(log["data"].lower() == "0x" + counter.to_bytes(32, "big").hex(), "Counter storage mismatch")
        for field in ("transactionHash", "transactionIndex", "blockNumber", "blockHash"):
            check(log[field] == r[field], f"Log {field} mismatch")
        check(log["removed"] is False, "Log marked removed")
        # LOG0 bloom contains the emitting address, despite having no topics.
        digest = bytes.fromhex(cast("keccak", to)[2:])
        bloom = sum(1 << bit for bit in set(
            int.from_bytes(digest[i:i+2], "big") & 2047 for i in (0, 2, 4)))
        check(number(r["logsBloom"]) == bloom, "Incorrect logs bloom")
    transferred = value if status == 1 and (to is None or to.lower() != FROM) else 0
    expected_balance = before_balance - used * price - transferred
    eventually(lambda: nonce() == before_nonce + 1 and balance(FROM) == expected_balance,
               f"{label}: nonce/balance mismatch; expected nonce={before_nonce + 1}, balance={expected_balance}; "
               f"another writer or fee recipient using this account invalidates this check")
    if to_balance is not None:
        eventually(lambda: balance(to) == to_balance + (value if status else 0),
                   f"{label}: recipient balance mismatch")
    eventually(lambda: number(rpc("eth_blockNumber")) >= number(r["blockNumber"]), "Head behind receipt")
    check(rpc("eth_getTransactionReceipt", txhash) == r, "Receipt changed on repeated read")
    print(f"PASS {label}: gas={used}, price={price}, nonce={before_nonce + 1}", flush=True)
    return r


check(cast("wallet", "address", "--private-key", KEY).lower() == FROM, "EVM_FROM/key mismatch")
check(number(rpc("eth_chainId")) == CHAIN, "Chain ID mismatch")
check(balance(FROM) >= 2000000000000000, "Fund this dedicated account with at least 0.002 native tokens")
print(f"Evidence: {OUT.resolve()}", flush=True)

# Giga's load-test app gives missing accounts 2**200 wei. Check balance
# deltas, not a zero starting balance, for this random recipient.
recipient = "0x" + secrets.token_hex(20)
print(f"Recipient: {recipient}; starting balance={balance(recipient)} wei", flush=True)
send("01-self-type2", to=FROM, value=1, gas=21000, expected_gas=21000)
send("02-transfer-type2", to=recipient, value=12345, gas=21000, expected_gas=21000)
send("03-transfer-legacy", to=recipient, value=67890, gas=21000, legacy=True, expected_gas=21000)

# Runtime increments slot 0 and emits its 32-byte value via LOG0.
# Empty calldata commits; nonempty calldata REVERTs after both SSTORE and LOG0.
# No Solidity compiler, eth_call, eth_getCode or eth_getStorageAt required.
runtime = "6000546001018060005560005260206000a03615601c5760006000fd5b00"
size = len(bytes.fromhex(runtime))
init = "0x" + f"60{size:02x}600c60003960{size:02x}6000f3" + runtime
deployed = send("04-deploy-counter", init=init, gas=200000)
contract = deployed["contractAddress"]
print(f"Counter: {contract}", flush=True)
contract_start_balance = balance(contract)
print(f"Contract starting balance={contract_start_balance} wei", flush=True)
send("05-counter-one", to=contract, value=11, counter=1)
send("06-revert", to=contract, value=17, data="0x01", status=0)
send("07-counter-two", to=contract, counter=2)
send("08-out-of-gas", to=contract, value=19, gas=21000, status=0, expected_gas=21000)
send("09-counter-three", to=contract, counter=3)

# Current tags are aliases for committed state, including pending.
for tag in ("latest", "safe", "finalized", "pending"):
    check(number(rpc("eth_getBalance", contract, tag)) == contract_start_balance + 11,
          f"Wrong balance for {tag}")
    check(number(rpc("eth_getTransactionCount", FROM, tag)) == nonce(), f"Wrong nonce for {tag}")
for method in ("eth_getBalance", "eth_getTransactionCount"):
    response = rpc_response(method, FROM, "0x1")
    check("historical state is not supported" in response.get("error", {}).get("message", ""),
          f"{method}: expected historical-state rejection: {response}")
unknown = "0x" + secrets.token_hex(32)
check(rpc("eth_getTransactionReceipt", unknown) is None, "Unknown receipt should be null")
check("error" in rpc_response("eth_sendRawTransaction", "0x1234"), "Malformed transaction accepted")
print("PASS current tags, historical rejection, unknown receipt, malformed transaction")
print(f"ALL CHECKS PASSED. Nine transaction receipts saved in {OUT.resolve()}")
