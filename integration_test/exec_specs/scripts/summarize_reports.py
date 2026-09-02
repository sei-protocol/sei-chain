#!/usr/bin/env python3
"""Summarize one or more EEST JUnit reports."""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path
from xml.etree import ElementTree

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


def render(
    reports: list[ReportTotals],
    *,
    missing_reports: list[str] | None = None,
    duplicate_reports: list[str] | None = None,
    unexpected_reports: list[str] | None = None,
) -> str:
    lines = ["## Execution specs summary", ""]
    if not reports:
        lines.append("No JUnit reports were found.")
    else:
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

    inventory_errors = (
        ("Missing expected reports", missing_reports or []),
        ("Duplicate reports", duplicate_reports or []),
        ("Unexpected reports", unexpected_reports or []),
    )
    if any(names for _, names in inventory_errors):
        lines += ["", "### Report inventory errors", ""]
        for label, names in inventory_errors:
            if names:
                lines.append(f"- {label}: {', '.join(f'`{name}`' for name in names)}")

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
    parser.add_argument(
        "--expected-reports-json",
        help="JSON array of exact JUnit report filenames expected from the matrix.",
    )
    arguments = parser.parse_args()

    expected_reports: list[str] = []
    if arguments.expected_reports_json:
        try:
            parsed_expected = json.loads(arguments.expected_reports_json)
        except json.JSONDecodeError as error:
            parser.error(f"invalid --expected-reports-json: {error}")
        if not isinstance(parsed_expected, list) or not all(
            isinstance(name, str) and name for name in parsed_expected
        ):
            parser.error("--expected-reports-json must be a JSON array of filenames.")
        expected_reports = parsed_expected
        if len(set(expected_reports)) != len(expected_reports):
            parser.error("--expected-reports-json contains duplicate filenames.")

    report_paths = collect(arguments.paths)
    actual_counts = Counter(path.name for path in report_paths)
    expected_names = set(expected_reports)
    actual_names = set(actual_counts)
    missing_reports = sorted(expected_names - actual_names)
    unexpected_reports = (
        sorted(actual_names - expected_names) if expected_reports else []
    )
    duplicate_reports = sorted(
        name for name, count in actual_counts.items() if count > 1
    )

    reports = [read_report(path) for path in report_paths]
    summary = render(
        reports,
        missing_reports=missing_reports,
        duplicate_reports=duplicate_reports,
        unexpected_reports=unexpected_reports,
    )
    print(summary)

    step_summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if step_summary:
        with open(step_summary, "a", encoding="utf-8") as handle:
            handle.write(summary + "\n")

    if not reports or missing_reports or duplicate_reports or unexpected_reports:
        return 1
    return 1 if any(report.failed for report in reports) else 0


if __name__ == "__main__":
    sys.exit(main())
