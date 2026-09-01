#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path
from typing import Any


class AuditError(Exception):
    pass


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise AuditError(f"Unable to load {path}: {error}") from error


def checkout_revision(checkout: Path) -> str:
    try:
        result = subprocess.run(
            ["git", "-C", str(checkout), "rev-parse", "HEAD"],
            check=True,
            capture_output=True,
            text=True,
        )
    except (OSError, subprocess.CalledProcessError) as error:
        raise AuditError(
            f"Unable to resolve ethereum/tests revision: {error}"
        ) from error
    return result.stdout.strip()


def audit_transaction_tests(
    checkout: Path, fork: str, skipped_files: set[str]
) -> dict[str, int]:
    root = checkout / "TransactionTests"
    fixture_files = sorted(root.rglob("*.json"))
    discovered_files = {str(path.relative_to(root)) for path in fixture_files}
    missing_skips = skipped_files - discovered_files
    if missing_skips:
        raise AuditError(
            "Configured transaction skips are missing upstream: "
            + ", ".join(sorted(missing_skips))
        )

    counts = {
        "files": len(fixture_files),
        "cases": 0,
        "validCases": 0,
        "invalidCases": 0,
        "skippedFiles": len(skipped_files),
        "skippedCases": 0,
        "runnableCases": 0,
    }
    errors: list[str] = []
    for path in fixture_files:
        relative = str(path.relative_to(root))
        data = load_json(path)
        if not isinstance(data, dict):
            errors.append(f"{relative}: root is not an object")
            continue
        for name, case in data.items():
            counts["cases"] += 1
            if relative in skipped_files:
                counts["skippedCases"] += 1
            else:
                counts["runnableCases"] += 1
            result = case.get("result", {}) if isinstance(case, dict) else {}
            expectation = result.get(fork) if isinstance(result, dict) else None
            if not isinstance(expectation, dict):
                errors.append(f"{relative}::{name}: missing {fork} result")
                continue
            if expectation.get("hash"):
                counts["validCases"] += 1
            elif expectation.get("exception"):
                counts["invalidCases"] += 1
            else:
                errors.append(
                    f"{relative}::{name}: {fork} result is neither valid nor invalid"
                )

    if errors:
        raise AuditError("Unclassified transaction fixtures:\n" + "\n".join(errors))
    return counts


def audit_rlp_tests(checkout: Path, skipped_files: set[str]) -> dict[str, int]:
    root = checkout / "RLPTests"
    fixture_files = sorted(root.rglob("*.json"))
    discovered_files = {str(path.relative_to(root)) for path in fixture_files}
    missing_skips = skipped_files - discovered_files
    if missing_skips:
        raise AuditError(
            "Configured RLP skips are missing upstream: "
            + ", ".join(sorted(missing_skips))
        )

    counts = {
        "files": len(fixture_files),
        "cases": 0,
        "validCases": 0,
        "invalidCases": 0,
        "skippedFiles": len(skipped_files),
        "skippedCases": 0,
        "runnableCases": 0,
    }
    errors: list[str] = []
    for path in fixture_files:
        relative = str(path.relative_to(root))
        data = load_json(path)
        if not isinstance(data, dict):
            errors.append(f"{relative}: root is not an object")
            continue
        for name, case in data.items():
            counts["cases"] += 1
            if relative in skipped_files:
                counts["skippedCases"] += 1
            else:
                counts["runnableCases"] += 1
            if not isinstance(case, dict) or not isinstance(case.get("out"), str):
                errors.append(f"{relative}::{name}: malformed RLP fixture")
                continue
            if case.get("in") == "INVALID":
                counts["invalidCases"] += 1
            else:
                counts["validCases"] += 1

    if errors:
        raise AuditError("Unclassified RLP fixtures:\n" + "\n".join(errors))
    return counts


def build_report(checkout: Path, policy_path: Path) -> dict[str, Any]:
    policy = load_json(policy_path)
    if policy.get("schemaVersion") != 1:
        raise AuditError("Applicability policy must use schemaVersion 1.")
    revision = checkout_revision(checkout)
    if revision != policy.get("fixtureRevision"):
        raise AuditError(
            f"Fixture revision {revision} does not match reviewed policy "
            f"{policy.get('fixtureRevision')}."
        )

    suites = policy.get("suites", {})
    transaction_policy = suites.get("TransactionTests", {})
    rlp_policy = suites.get("RLPTests", {})
    if transaction_policy.get("status") != "applicable":
        raise AuditError("TransactionTests must have an applicable policy.")
    if rlp_policy.get("status") != "applicable":
        raise AuditError("RLPTests must have an applicable policy.")

    transaction = audit_transaction_tests(
        checkout,
        policy["transactionFork"],
        set(transaction_policy.get("builtInSkippedFiles", [])),
    )
    rlp = audit_rlp_tests(checkout, set(rlp_policy.get("builtInSkippedFiles", [])))
    return {
        "schemaVersion": 1,
        "status": "classified",
        "fixtureRevision": revision,
        "goEthereumVersion": policy["goEthereumVersion"],
        "transactionFork": policy["transactionFork"],
        "suites": {
            "TransactionTests": transaction,
            "RLPTests": rlp,
        },
        "summary": {
            "cases": transaction["cases"] + rlp["cases"],
            "validCases": transaction["validCases"] + rlp["validCases"],
            "invalidCases": transaction["invalidCases"] + rlp["invalidCases"],
            "skippedCases": transaction["skippedCases"] + rlp["skippedCases"],
            "runnableCases": transaction["runnableCases"] + rlp["runnableCases"],
        },
    }


def render_markdown(report: dict[str, Any]) -> str:
    summary = report["summary"]
    lines = [
        "# Ethereum Transaction and RLP Test Audit",
        "",
        f"- Fixture revision: `{report['fixtureRevision']}`",
        f"- Sei go-ethereum fork: `{report['goEthereumVersion']}`",
        f"- Transaction expectation fork: `{report['transactionFork']}`",
        f"- Total cases: {summary['cases']}",
        f"- Runnable cases: {summary['runnableCases']}",
        f"- Expected-valid cases: {summary['validCases']}",
        f"- Expected-invalid cases: {summary['invalidCases']}",
        f"- Skipped by the pinned geth runner: {summary['skippedCases']}",
        "",
        "Invalid fixtures are runnable tests: they pass only when decoding or "
        "validation rejects the input as specified.",
        "",
        "| Suite | Files | Cases | Runnable | Valid | Invalid | Skipped |",
        "| --- | ---: | ---: | ---: | ---: | ---: | ---: |",
    ]
    for name in ("TransactionTests", "RLPTests"):
        suite = report["suites"][name]
        lines.append(
            f"| {name} | {suite['files']} | {suite['cases']} | "
            f"{suite['runnableCases']} | {suite['validCases']} | "
            f"{suite['invalidCases']} | {suite['skippedCases']} |"
        )
    lines.append("")
    return "\n".join(lines)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    suite_root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(
        description="Audit applicable ethereum/tests Transaction and RLP fixtures."
    )
    parser.add_argument(
        "--ethereum-tests-dir",
        type=Path,
        default=suite_root / ".cache" / "ethereum-tests",
    )
    parser.add_argument(
        "--policy",
        type=Path,
        default=suite_root / "legacy" / "applicability.json",
    )
    parser.add_argument(
        "--json-out",
        type=Path,
        default=suite_root / "reports" / "legacy" / "applicability.json",
    )
    parser.add_argument(
        "--markdown-out",
        type=Path,
        default=suite_root / "reports" / "legacy" / "applicability.md",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        report = build_report(args.ethereum_tests_dir, args.policy)
        args.json_out.parent.mkdir(parents=True, exist_ok=True)
        args.markdown_out.parent.mkdir(parents=True, exist_ok=True)
        args.json_out.write_text(
            json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
        args.markdown_out.write_text(render_markdown(report), encoding="utf-8")
    except AuditError as error:
        print(f"Ethereum legacy test audit failed: {error}", file=sys.stderr)
        return 1

    summary = report["summary"]
    print(
        f"Ethereum legacy test audit: {summary['runnableCases']} runnable, "
        f"{summary['validCases']} expected-valid, "
        f"{summary['invalidCases']} expected-invalid, "
        f"{summary['skippedCases']} skipped."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
