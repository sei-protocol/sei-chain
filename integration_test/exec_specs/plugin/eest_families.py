"""EEST families whose vectors need dedicated chain partitions."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable


@dataclass(frozen=True)
class IsolatedFamily:
    """A family partitioned by a stable parameter rather than node-ID hash."""

    name: str
    path: str
    test_name: str
    param: str
    chains: int
    partitioner: Callable[[int], int] | None = None

    def case_index(self, item: Any) -> int | None:
        if self.path not in item.nodeid:
            return None
        if getattr(item, "originalname", None) != self.test_name:
            return None
        callspec = getattr(item, "callspec", None)
        if callspec is None:
            return None
        value = callspec.params.get(self.param)
        return value if isinstance(value, int) else None

    def partition_index(self, item: Any) -> int | None:
        """Return the semantic chain partition for a family member."""
        case_index = self.case_index(item)
        if case_index is None:
            return None
        if self.partitioner is not None:
            return self.partitioner(case_index)
        return case_index % self.chains


# The pinned upstream vector lays out 21 addresses for each action in this
# order. Only CALL with value (F101/F103/F105) can permanently fund a
# precompile. Give each mutating action its own fresh chain; all remaining
# actions are read-only with respect to global precompile state and can safely
# share one.
EIP2929_CASES_PER_ACTION = 21
EIP2929_ACTIONS = (
    "F100",
    "F101",
    "F102",
    "F103",
    "F104",
    "F105",
    "F200",
    "F201",
    "F202",
    "F203",
    "F204",
    "F205",
    "F400",
    "F402",
    "F404",
    "FA00",
    "FA02",
    "FA04",
    "31",
    "3B",
    "3C",
    "3F",
)
EIP2929_MUTATING_ACTIONS = ("F101", "F103", "F105")


def eip2929_partition(case_index: int) -> int:
    action_group = case_index // EIP2929_CASES_PER_ACTION
    if case_index < 0 or action_group >= len(EIP2929_ACTIONS):
        raise ValueError(f"Unknown EIP-2929 case index: {case_index}")
    action = EIP2929_ACTIONS[action_group]
    try:
        return EIP2929_MUTATING_ACTIONS.index(action) + 1
    except ValueError:
        return 0


ISOLATED_FAMILIES: dict[str, IsolatedFamily] = {
    # These vectors assert warm/cold gas deltas against precompile account
    # existence. Value-bearing CALL groups mutate precompile balances, so
    # partition by operation semantics rather than arbitrary case-index modulo.
    "eip2929-precompiles": IsolatedFamily(
        name="eip2929-precompiles",
        path=(
            "tests/ported_static/stPreCompiledContracts/test_precomps_eip2929_cancun.py"
        ),
        test_name="test_precomps_eip2929_cancun",
        param="d",
        chains=4,
        partitioner=eip2929_partition,
    ),
}
