"""Sei selection policy and chain partitioning for upstream EEST vectors."""

from __future__ import annotations

import hashlib
import os
from collections.abc import Callable
from dataclasses import dataclass

import pytest

EIP2930_FLOOR_GAS_CASES = {
    "access_list_empty-data_1_zero_byte",
    "access_list_empty-data_4_zero_byte",
    "access_list_empty-data_1_non_zero_byte",
    "access_list_empty-data_1_zero_byte_1_non_zero_byte",
    "access_list_empty-data_1_zero_byte_1_non_zero_byte_reversed",
    "access_list_empty-data_set_1",
    "access_list_empty-data_set_2",
    "access_list_empty-data_set_3",
    "access_list_empty-data_set_31_bytes",
    "access_list_empty-data_set_32_bytes",
    "access_list_empty-data_set_33_bytes",
    "access_list_empty-data_set_33_empty_bytes",
    "access_list_empty-data_set_66_bytes_half_zeros",
    "access_list_1_address_empty_keys-data_set_66_bytes_half_zeros",
}

EIP6780_REPEATED_SELFDESTRUCT_CASES = {
    "multiple_calls_single_self_recipient",
    "multiple_calls_multiple_sendall_recipients_including_self_last",
    "multiple_calls_multiple_repeating_sendall_recipients_including_self",
    "multiple_calls_multiple_repeating_sendall_recipients_including_self_last",
}

UNAVAILABLE_SYSTEM_CONTRACTS = (
    "system_contract_0x0000f90827f1c53a10cb7a02335b175320002935",
    "system_contract_0x000f3df6d732807ef1319fb7b8bb8522d0beac02",
)

PRECOMPILE_ONE = "precompile_0x0000000000000000000000000000000000000001"
PRECOMPILE_FOUR = "precompile_0x0000000000000000000000000000000000000004"


@dataclass(frozen=True)
class SkipRule:
    """A named, reviewable reason to skip a vector on a live Sei chain."""

    id: str
    reason: str
    matches: Callable[[pytest.Item], bool]


def _eip7623_admission(item: pytest.Item) -> bool:
    return (
        "tests/prague/eip7623_increase_calldata_cost/test_transaction_validity.py"
        in item.nodeid
        and "insufficient_gas-floor_gas_greater_than_intrinsic_gas" in item.name
        and "-unprotected-" not in item.name
    )


def _eip7623_floor_data_gas(item: pytest.Item) -> bool:
    return (
        "tests/berlin/eip2930_access_list/test_tx_intrinsic_gas.py" in item.nodeid
        and "fork_Prague-" in item.name
        and "below_intrinsic_True" in item.name
        and any(case in item.name for case in EIP2930_FLOOR_GAS_CASES)
    )


def _eip6780_repeated_selfdestruct(item: pytest.Item) -> bool:
    return (
        "tests/cancun/eip6780_selfdestruct/test_selfdestruct.py" in item.nodeid
        and getattr(item, "originalname", None) == "test_create_selfdestruct_same_tx"
        and any(case in item.name for case in EIP6780_REPEATED_SELFDESTRUCT_CASES)
    )


def _selfdestruct_precompile_balance(item: pytest.Item) -> bool:
    return (
        "tests/tangerine_whistle/eip150_operation_gas_costs/test_eip150_selfdestruct.py"
        in item.nodeid
        and getattr(item, "originalname", None)
        in {
            "test_selfdestruct_to_precompile",
            "test_selfdestruct_to_precompile_state_access_boundary",
        }
        and (PRECOMPILE_ONE in item.name or PRECOMPILE_FOUR in item.name)
        and "dead_beneficiary-no_balance" in item.name
        and item.name.endswith("-exact_gas]")
    )


def _extcodehash_precompile_balance(item: pytest.Item) -> bool:
    if (
        "tests/constantinople/eip1052_extcodehash/test_extcodehash.py"
        not in item.nodeid
    ):
        return False
    originalname = getattr(item, "originalname", None)
    if originalname == "test_extcodehash_precompile":
        return PRECOMPILE_ONE in item.name or PRECOMPILE_FOUR in item.name
    if originalname == "test_extcodehash_dynamic_argument":
        return "target_type_precompile]" in item.name
    return False


def _eip7702_system_contract(item: pytest.Item) -> bool:
    return getattr(
        item, "originalname", None
    ) == "test_set_code_to_system_contract" and any(
        contract in item.name for contract in UNAVAILABLE_SYSTEM_CONTRACTS
    )


SKIP_RULES: tuple[SkipRule, ...] = (
    SkipRule(
        id="eip7623-admission",
        reason=(
            "Known Sei EIP-7623 transaction-admission issue "
            "(https://github.com/sei-protocol/sei-chain/issues/4068)."
        ),
        matches=_eip7623_admission,
    ),
    SkipRule(
        id="eip7623-floor-data-gas",
        reason=(
            "Known Sei EIP-7623 floor-data-gas admission issue "
            "(https://github.com/sei-protocol/sei-chain/issues/4068)."
        ),
        matches=_eip7623_floor_data_gas,
    ),
    SkipRule(
        id="eip6780-repeated-selfdestruct",
        reason=(
            "Known Sei EIP-6780 repeated-SELFDESTRUCT issue "
            "(https://github.com/sei-protocol/sei-chain/issues/4069)."
        ),
        matches=_eip6780_repeated_selfdestruct,
    ),
    SkipRule(
        id="selfdestruct-precompile-balance",
        reason="Persistent remote chains cannot restore precompile balances to zero.",
        matches=_selfdestruct_precompile_balance,
    ),
    SkipRule(
        id="extcodehash-precompile-balance",
        reason="Persistent precompile balances change EXTCODEHASH results.",
        matches=_extcodehash_precompile_balance,
    ),
    SkipRule(
        id="eip7702-system-contract",
        reason="Sei genesis does not contain Ethereum system-contract bytecode.",
        matches=_eip7702_system_contract,
    ),
)


def matching_rule(item: pytest.Item) -> SkipRule | None:
    return next((rule for rule in SKIP_RULES if rule.matches(item)), None)


def shard_for_nodeid(nodeid: str, shard_count: int) -> int:
    digest = hashlib.sha256(nodeid.encode()).digest()
    return int.from_bytes(digest[:8], byteorder="big") % shard_count


def shard_configuration() -> tuple[int, int]:
    try:
        shard_count = int(os.environ.get("EEST_SHARD_COUNT", "1"))
        shard_index = int(os.environ.get("EEST_SHARD_INDEX", "0"))
    except ValueError as error:
        raise pytest.UsageError(
            "EEST_SHARD_COUNT and EEST_SHARD_INDEX must be integers."
        ) from error

    if shard_count < 1:
        raise pytest.UsageError("EEST_SHARD_COUNT must be positive.")
    if not 0 <= shard_index < shard_count:
        raise pytest.UsageError(
            "EEST_SHARD_INDEX must be between 0 and EEST_SHARD_COUNT - 1."
        )
    return shard_count, shard_index


def partition(
    items: list[pytest.Item],
    shard_count: int,
    shard_index: int,
) -> tuple[list[pytest.Item], list[pytest.Item]]:
    """Split collected vectors into selected and deselected partitions."""
    if shard_count == 1:
        return list(items), []

    selected = []
    deselected = []
    for item in items:
        keep = shard_for_nodeid(item.nodeid, shard_count) == shard_index
        (selected if keep else deselected).append(item)
    return selected, deselected


def pytest_report_header() -> str | None:
    shard_count, shard_index = shard_configuration()
    if shard_count > 1:
        return f"EEST chain {shard_index + 1}/{shard_count}"
    return None


@pytest.hookimpl(trylast=True)
def pytest_collection_modifyitems(
    config: pytest.Config,
    items: list[pytest.Item],
) -> None:
    shard_count, shard_index = shard_configuration()
    selected, deselected = partition(items, shard_count, shard_index)

    for item in selected:
        rule = matching_rule(item)
        if rule is not None:
            item.add_marker(pytest.mark.skip(reason=rule.reason))

    if deselected:
        config.hook.pytest_deselected(items=deselected)
    items[:] = selected
