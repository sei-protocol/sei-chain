"""Minimal YAML loader supporting the subset used by Nova configs."""
from __future__ import annotations

from typing import Any, List, Tuple


class YamlError(RuntimeError):
    pass


def safe_load(text: str) -> Any:
    lines = text.splitlines()
    root: dict[str, Any] = {}
    stack: List[Tuple[int, Any]] = [(-1, root)]
    idx = 0
    while idx < len(lines):
        raw = lines[idx]
        stripped = raw.strip()
        if not stripped or stripped.startswith("#"):
            idx += 1
            continue

        indent = len(raw) - len(raw.lstrip(" "))
        while len(stack) > 1 and indent <= stack[-1][0]:
            stack.pop()
        container = stack[-1][1]

        if stripped.startswith("- "):
            if not isinstance(container, list):
                raise YamlError("List item found but container is not a list")
            value = _parse_scalar(stripped[2:].strip())
            container.append(value)
            idx += 1
            continue

        if ":" not in stripped:
            raise YamlError(f"Invalid line: {raw}")
        key, _, value_part = stripped.partition(":")
        key = key.strip()
        value_part = value_part.strip()
        if not isinstance(container, dict):
            raise YamlError("Cannot assign key/value to non-dict container")

        if value_part == "":
            next_container = _infer_container(lines, idx + 1, indent)
            container[key] = next_container
            stack.append((indent, next_container))
        else:
            value = _parse_scalar(value_part)
            container[key] = value
        idx += 1

    return root


def _infer_container(lines: List[str], start_idx: int, parent_indent: int) -> Any:
    idx = start_idx
    while idx < len(lines):
        stripped = lines[idx].strip()
        if not stripped or stripped.startswith("#"):
            idx += 1
            continue
        indent = len(lines[idx]) - len(lines[idx].lstrip(" "))
        if indent <= parent_indent:
            break
        if stripped.startswith("- "):
            return []
        return {}
    return {}


def _parse_scalar(value: str) -> Any:
    if not value:
        return {}
    if value[0] == value[-1] and value[0] in {'"', "'"}:
        return value[1:-1]
    lowered = value.lower()
    if lowered in {"true", "false"}:
        return lowered == "true"
    try:
        if value.startswith("0") and len(value) > 1:
            raise ValueError
        return int(value)
    except ValueError:
        try:
            return float(value)
        except ValueError:
            return value
