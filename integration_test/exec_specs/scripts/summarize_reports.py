#!/usr/bin/env python3
"""Summarize one or more EEST JUnit reports."""

from __future__ import annotations

import argparse
import os
import sys
import xml.etree.ElementTree as ElementTree
from dataclasses import dataclass, field
from pathlib import Path

MAX_LISTED_FAILURES = 50


@dataclass
class ReportTotals:
    name: str
    passed: int = 0
    failed: int = 0
    skipped: int = 0
    xfailed: int = 0
    duration: float = 0.0
    failures: list[str] = field(default_factory=list)


def read_report(path: Path) -> ReportTotals:
    totals = ReportTotals(name=path.stem)
    root = ElementTree.parse(path).getroot()

    for suite in root.iter("testsuite"):
        totals.duration += float(suite.get("time") or 0.0)

    for case in root.iter("testcase"):
        identifier = f"{case.get('classname', '')}::{case.get('name', '')}"
        outcomes = {child.tag: child for child in case}
        if "failure" in outcomes or "error" in outcomes:
            totals.failed += 1
            if len(totals.failures) < MAX_LISTED_FAILURES:
                totals.failures.append(identifier)
        elif "skipped" in outcomes:
            skipped = outcomes["skipped"]
            if (skipped.get("type") or "") == "pytest.xfail":
                totals.xfailed += 1
            else:
                totals.skipped += 1
        else:
            totals.passed += 1

    return totals


def render(reports: list[ReportTotals]) -> str:
    lines = ["## Execution specs summary", ""]
    if not reports:
        lines.append("No JUnit reports were found.")
        return "\n".join(lines)

    lines += [
        "| Chain | Passed | Failed | Skipped | XFailed | Duration |",
        "| --- | --- | --- | --- | --- | --- |",
    ]
    for report in sorted(reports, key=lambda item: item.name):
        lines.append(
            f"| {report.name} | {report.passed} | {report.failed} "
            f"| {report.skipped} | {report.xfailed} "
            f"| {report.duration / 60:.1f} min |"
        )

    totals = ReportTotals(name="total")
    for report in reports:
        totals.passed += report.passed
        totals.failed += report.failed
        totals.skipped += report.skipped
        totals.xfailed += report.xfailed
    lines.append(
        f"| **all {len(reports)} chains** | **{totals.passed}** "
        f"| **{totals.failed}** | **{totals.skipped}** "
        f"| **{totals.xfailed}** | |"
    )

    failing = [report for report in reports if report.failures]
    if failing:
        lines += ["", "### Failures", ""]
        for report in sorted(failing, key=lambda item: item.name):
            lines.append(f"**{report.name}**")
            lines += [f"- `{identifier}`" for identifier in report.failures]
            lines.append("")

    return "\n".join(lines)


def collect(paths: list[Path]) -> list[Path]:
    found: set[Path] = set()
    for path in paths:
        if path.is_dir():
            found.update(path.rglob("*.xml"))
        elif path.exists():
            found.add(path)
    return sorted(found)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "paths",
        type=Path,
        nargs="+",
        help="JUnit XML files, or directories searched recursively for them.",
    )
    arguments = parser.parse_args()

    reports = [read_report(path) for path in collect(arguments.paths)]
    summary = render(reports)
    print(summary)

    step_summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if step_summary:
        with open(step_summary, "a", encoding="utf-8") as handle:
            handle.write(summary + "\n")

    if not reports:
        return 1
    return 1 if any(report.failed for report in reports) else 0


if __name__ == "__main__":
    sys.exit(main())
