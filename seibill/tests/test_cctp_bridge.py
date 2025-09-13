# Copyright 2024 The Sei Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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
