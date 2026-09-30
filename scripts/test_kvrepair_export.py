#!/usr/bin/env python3
import http.server
import importlib.util
import json
import pathlib
import sys
import threading
import unittest

_path = pathlib.Path(__file__).with_name("kvrepair-export.py")
_spec = importlib.util.spec_from_file_location("kvrepair_export", _path)
kx = importlib.util.module_from_spec(_spec)
sys.modules["kvrepair_export"] = kx
_spec.loader.exec_module(kx)


class FakeNode:
    def __init__(self, values):
        self.values = values
        self.reads = []

    def get(self, store, key, height):
        self.reads.append((store, key, height))
        return self.values.get((store, key))


class ParseKeysTest(unittest.TestCase):
    def test_parses_expectations_and_comments(self):
        keys = kx.parse_keys([
            "# header\n",
            "evm 0x0102\n",
            "evm 0304 dead  # damaged\n",
            "\n",
            "bank 05 absent\n",
        ])
        self.assertEqual(keys, [
            ("evm", b"\x01\x02", None),
            ("evm", b"\x03\x04", b"\xde\xad"),
            ("bank", b"\x05", kx.ABSENT),
        ])

    def test_rejects_bad_lines(self):
        for line, message in (
            ("evm", "want 'store key"),
            ("evm zz", "key is not hex"),
            ("evm 01 zz", "expect is not hex"),
            ("evm 0x", "key is empty"),
            ("evm 01 02 03", "want 'store key"),
        ):
            with self.subTest(line=line), self.assertRaisesRegex(ValueError, message):
                kx.parse_keys([line])


class BuildRepairTest(unittest.TestCase):
    def build(self, keys, reserve, prod=None):
        warnings = []
        repair = kx.build_repair("r", "c", 100, 90, "src", reserve, prod, keys, warnings.append)
        return repair, warnings

    def test_values_come_from_reserve_at_height(self):
        reserve = FakeNode({("evm", b"\x01"): b"\xaa"})
        repair, warnings = self.build([("evm", b"\x01", b"\xde"), ("evm", b"\x02", b"\xde")], reserve)
        self.assertEqual(repair, {
            "name": "r", "chain_id": "c", "height": 100, "source": "src",
            "entries": [
                {"store": "evm", "key": "01", "value": "aa", "expect": "de"},
                {"store": "evm", "key": "02", "value": None, "expect": "de"},
            ],
        })
        self.assertEqual({h for _, _, h in reserve.reads}, {90})
        self.assertEqual(warnings, [])

    def test_prod_supplies_missing_expectations(self):
        reserve = FakeNode({("evm", b"\x01"): b"\xaa", ("evm", b"\x03"): b"\xcc"})
        prod = FakeNode({("evm", b"\x01"): b"\xde", ("evm", b"\x02"): b"\xde", ("evm", b"\x03"): b"\xcc"})
        repair, warnings = self.build(
            [("evm", b"\x01", None), ("evm", b"\x02", None), ("evm", b"\x03", None), ("evm", b"\x04", b"\x01")],
            reserve, prod,
        )
        self.assertEqual(repair["entries"], [
            {"store": "evm", "key": "01", "value": "aa", "expect": "de"},
            {"store": "evm", "key": "02", "value": None, "expect": "de"},
            {"store": "evm", "key": "03", "value": "cc"},
            {"store": "evm", "key": "04", "value": None, "expect": "01"},
        ])
        self.assertEqual(len(warnings), 1)
        self.assertIn("key 03: production state store agrees", warnings[0])
        self.assertNotIn(("evm", b"\x04", 90), prod.reads)

    def test_prod_absent_becomes_expect_absent(self):
        reserve = FakeNode({("evm", b"\x01"): b"\xaa"})
        repair, _ = self.build([("evm", b"\x01", None)], reserve, FakeNode({}))
        self.assertEqual(repair["entries"], [{"store": "evm", "key": "01", "value": "aa", "expect_absent": True}])

    def test_warns_when_expectation_equals_target(self):
        reserve = FakeNode({("evm", b"\x01"): b"\xaa"})
        _, warnings = self.build([("evm", b"\x01", b"\xaa"), ("evm", b"\x02", kx.ABSENT)], reserve)
        self.assertEqual(len(warnings), 2)
        self.assertTrue(all("no-op" in w for w in warnings))


class RPCTest(unittest.TestCase):
    def setUp(self):
        self.requests = []
        responses = self.responses = {}
        requests = self.requests

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                requests.append(body)
                payload = json.dumps({"jsonrpc": "2.0", "id": body["id"], **responses[body["method"]]}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *args):
                pass

        self.server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.rpc = kx.RPC(f"http://127.0.0.1:{self.server.server_port}/")

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()

    def test_get_sends_hex_key_without_prefix(self):
        self.responses["abci_query"] = {"result": {"response": {"code": 0, "value": "qg=="}}}
        self.assertEqual(self.rpc.get("evm", b"\x03\xa8", 90), b"\xaa")
        self.assertEqual(self.requests[0]["params"], {
            "path": "/store/evm/key", "data": "03a8", "height": "90", "prove": False,
        })

    def test_get_returns_none_when_absent(self):
        self.responses["abci_query"] = {"result": {"response": {"code": 0, "value": None}}}
        self.assertIsNone(self.rpc.get("evm", b"\x03", 90))

    def test_get_raises_on_query_error(self):
        self.responses["abci_query"] = {"result": {"response": {"code": 26, "log": "height too low"}}}
        with self.assertRaisesRegex(kx.RPCError, "height too low"):
            self.rpc.get("evm", b"\x03", 90)

    def test_rpc_error(self):
        self.responses["abci_query"] = {"error": {"code": -32602, "message": "Invalid params"}}
        with self.assertRaisesRegex(kx.RPCError, "Invalid params"):
            self.rpc.get("evm", b"\x03", 90)

    def test_network(self):
        self.responses["status"] = {"result": {"node_info": {"network": "c"}}}
        self.assertEqual(self.rpc.network(), "c")


if __name__ == "__main__":
    unittest.main()
