"""EEST families whose vectors need dedicated chain partitions."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class IsolatedFamily:
    """A family partitioned by a stable parameter rather than node-ID hash."""

    name: str
    path: str
    test_name: str
    param: str
    chains: int
    run_remote: bool = True

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
        """Return the chain partition for a family member."""
        case_index = self.case_index(item)
        if case_index is None:
            return None
        return case_index % self.chains


ISOLATED_FAMILIES: dict[str, IsolatedFamily] = {
    # These vectors assert warm/cold gas deltas against precompile account
    # existence and are not stable on persistent remote chains. Keep the
    # family registered so general state runs exclude it, but do not schedule
    # a dedicated remote runner.
    "eip2929-precompiles": IsolatedFamily(
        name="eip2929-precompiles",
        path=(
            "tests/ported_static/stPreCompiledContracts/test_precomps_eip2929_cancun.py"
        ),
        test_name="test_precomps_eip2929_cancun",
        param="d",
        chains=1,
        run_remote=False,
    ),
}
