# UNLICENSED
#
# All rights reserved. This software and its source code may not be copied,
# modified, distributed, or used without prior written permission from the authors.

"""Tests for Circle CCTP bridge helper."""

import argparse
import json
from urllib import request

"""Tests for Circle CCTP bridge helper."""

import json
import unittest
from unittest.mock import patch

from seibill.scripts import cctp_bridge


class MockHTTPResponse:
    def __init__(self, payload):
        self._payload = json.dumps(payload).encode()

    def read(self):
        return self._payload

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        return False


class MockChain:
    def __init__(self, balance):
        self.balance = balance


class TestCCTPBridge(unittest.TestCase):
    @patch("seibill.scripts.cctp_bridge.request.urlopen")
    def test_transfer_updates_balances_and_receipt(self, mock_urlopen):
        def side_effect(req, timeout=30):
            url = req.full_url
            if url.endswith("/burns"):
                return MockHTTPResponse({"burnTxId": "burn123"})
            if url.endswith("/mints"):
                return MockHTTPResponse({"mintTxId": "mint456"})
            raise AssertionError("Unexpected URL" + url)

        mock_urlopen.side_effect = side_effect

        src = MockChain(1000)
        dst = MockChain(0)
        amount = 100

        burn, mint, receipt = cctp_bridge.transfer(
            "key", "1", "2", "0xabc", amount
        )

        src.balance -= amount
        dst.balance += amount

        self.assertEqual(src.balance, 900)
        self.assertEqual(dst.balance, 100)
        self.assertEqual(burn["burnTxId"], "burn123")
        self.assertEqual(mint["mintTxId"], "mint456")
        self.assertTrue(receipt["x402"].startswith("x402-receipt-"))


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
