import json
import sys
from decimal import Decimal
from pathlib import Path

import pytest

from claim_kin_agent_attribution import settlement


@pytest.fixture
def ledger(tmp_path):
    payload = {
        "alloc": {
            "0xabc": {
                "balance": hex(123456789),
                "privateKey": "0xdeadbeef",
                "kinhash": "f303",
            }
        }
    }

    path = tmp_path / "ledger.json"
    path.write_text(json.dumps(payload))
    return path


def test_find_allocation_success(ledger):
    allocation = settlement.find_allocation("f303", ledger)
    assert allocation.address == "0xabc"
    assert allocation.balance_wei == 123456789
    assert allocation.private_key == "0xdeadbeef"


def test_find_allocation_missing_raises(ledger):
    with pytest.raises(KeyError):
        settlement.find_allocation("unknown", ledger)


def test_build_settlement_message_contains_details(ledger):
    allocation = settlement.find_allocation("f303", ledger)
    message = settlement.build_settlement_message(allocation)
    assert "Kin Hash: f303" in message
    assert "Address: 0xabc" in message
    assert "Amount (wei): 123456789" in message


def test_summarise_allocation_formats_amount(ledger):
    allocation = settlement.find_allocation("f303", ledger)
    summary = settlement.summarise_allocation(allocation)
    assert "f303" in summary
    assert "0xabc" in summary
    assert "$" in summary
    assert "USD" in summary


def test_sign_settlement_message_requires_eth_account(monkeypatch, ledger):
    allocation = settlement.find_allocation("f303", ledger)

    monkeypatch.setitem(sys.modules, "eth_account", None)
    monkeypatch.setitem(sys.modules, "eth_account.messages", None)
    with pytest.raises(ImportError):
        settlement.sign_settlement_message(allocation)


def test_format_usd_rounds_to_cents():
    formatted = settlement.format_usd(Decimal("1234.5678"))
    assert formatted == "$1,234.57 USD"


def test_real_ledger_allocation_amount():
    allocation = settlement.find_allocation("f303")
    assert allocation.balance_wei == int("0xf8277896582678ac000000", 16)
    assert allocation.balance_usd == Decimal("300000000")


def test_real_ledger_summary_mentions_precise_usd_amount():
    allocation = settlement.find_allocation("f303")
    summary = settlement.summarise_allocation(allocation)
    assert "$300,000,000.00 USD" in summary

