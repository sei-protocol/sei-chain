#!/usr/bin/env python3
"""Compare the HashLogger output of several Sei nodes at the chain tip.

Name each node by a pod name (optionally CLUSTER/POD), an IP address, a DNS
name, or user@host. Pods are read through kubectl exec and hosts through ssh.
The nodes need only sh and coreutils; the file cursors and comparison state
stay on the machine that runs this script.

  hashlog-watch.py --prod prod/rpc-node-0-0-0 --reserve memiavl-rpc-0-0-0
  hashlog-watch.py --chain atlantic-2 --prod node-wave-0-2-0 --reserve 3.68.111.103

The script exits with status 2 on the first alarm.
"""

from __future__ import annotations

import argparse
import collections
import concurrent.futures
import csv
import dataclasses
import datetime as dt
import hashlib
import json
import os
import pathlib
import queue
import re
import shlex
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request
from typing import Any, Dict, List, Optional, Tuple

KUBE_CONTEXTS = ("prod", "prod-euw1", "prod-use2")
KUBE_CONTAINER = "seid"
KUBE_HASHLOG_DIR = "/home/nonroot/.sei/data/hash.log"
SSH_DEFAULT_USER = "ec2-user"
# Relative to the login home directory of the ssh user.
SSH_HASHLOG_DIR = ".sei/data/hash.log"
SSH_OPTIONS = (
    "-o", "BatchMode=yes",
    "-o", "StrictHostKeyChecking=accept-new",
    "-o", "ConnectTimeout=10",
    "-o", "ControlMaster=auto",
    "-o", "ControlPersist=120",
    "-o", "ControlPath=/tmp/hashlog-watch-%C",
)
COMMAND_TIMEOUT_SECONDS = 30
READ_LIMIT_BYTES = 4 << 20
TIP_BACKTRACK_BYTES = 16 << 10
MATCHED_HISTORY = 10_000
STATE_DIR = pathlib.Path.home() / ".local" / "state" / "hashlog-watch"

# Runs on the node: list the archive, then print one file from a byte offset.
# Arguments: directory, file index (-1 lists only), offset, byte limit.
READ_SCRIPT = r"""
cd "$1" 2>/dev/null || { echo "hash log directory not found: $1" >&2; exit 3; }
ls -1
[ "$2" -ge 0 ] || exit 0
for f in "$2"-*.hlog "$2"-*.hlog.u; do
  [ -f "$f" ] || continue
  printf '@@FILE %s\n@@HEAD\n' "$f"
  head -n 1 "$f" 2>/dev/null
  printf '\n@@SIZE %s\n@@DATA\n' "$(wc -c < "$f" 2>/dev/null)"
  tail -c +"$(($3 + 1))" "$f" 2>/dev/null | head -c "$4"
  exit 0
done
"""

SEALED_RE = re.compile(r"^(\d+)-(\d+)-(\d+)-[A-Za-z0-9._]+\.hlog$")
ACTIVE_RE = re.compile(r"^(\d+)-[A-Za-z0-9._]+\.hlog\.u$")


class TransportError(RuntimeError):
    """A node could not be reached or the read command failed."""


class CoverageGap(RuntimeError):
    """A node no longer holds the hash-log rows the comparison needs."""


@dataclasses.dataclass(frozen=True)
class Target:
    label: str
    role: str
    transport: str
    address: Tuple[str, ...]

    def command(self, *args: str) -> List[str]:
        if self.transport == "kubectl":
            context, namespace, pod = self.address
            return [
                "kubectl", "--context", context, "-n", namespace,
                "exec", pod, "-c", KUBE_CONTAINER, "--",
                "sh", "-c", READ_SCRIPT, "sh", KUBE_HASHLOG_DIR, *args,
            ]
        if self.transport == "ssh":
            (host,) = self.address
            remote = shlex.join(
                ["sh", "-c", READ_SCRIPT, "sh", SSH_HASHLOG_DIR, *args]
            )
            return ["ssh", *SSH_OPTIONS, host, remote]
        (directory,) = self.address
        return ["sh", "-c", READ_SCRIPT, "sh", directory, *args]

    def describe(self) -> str:
        if self.transport == "kubectl":
            context, namespace, pod = self.address
            return f"kubectl {context}/{namespace}/{pod}"
        return f"{self.transport} {self.address[0]}"


def run_kubectl_lookup(context: str, name: str) -> List[str]:
    """Return the namespaces in which a pod called name exists."""
    result = subprocess.run(  # noqa: S603 - fixed argument vector
        [
            "kubectl", "--context", context, "get", "pods", "-A",
            "--field-selector", f"metadata.name={name}",
            "-o", "jsonpath={.items[*].metadata.namespace}",
        ],
        capture_output=True,
        text=True,
        timeout=COMMAND_TIMEOUT_SECONDS,
        check=False,
    )
    if result.returncode != 0:
        raise TransportError(last_line(result.stderr) or "kubectl failed")
    return result.stdout.split()


def find_pods(
    name: str, chain: Optional[str], contexts: Tuple[str, ...] = KUBE_CONTEXTS
) -> List[Tuple[str, str, str]]:
    """Return (context, namespace, pod) for a pod or SeiNode name."""
    errors = {}
    for pod in (name, f"{name}-0"):
        matches = []
        with concurrent.futures.ThreadPoolExecutor(len(contexts)) as pool:
            lookups = {
                context: pool.submit(run_kubectl_lookup, context, pod)
                for context in contexts
            }
            for context, lookup in lookups.items():
                try:
                    namespaces = lookup.result()
                except (TransportError, OSError, subprocess.TimeoutExpired) as error:
                    errors[context] = str(error)
                    continue
                matches.extend(
                    (context, namespace, pod)
                    for namespace in namespaces
                    if chain is None or namespace == chain
                )
        if matches:
            return matches
    if errors:
        details = "; ".join(f"{ctx}: {err}" for ctx, err in errors.items())
        raise ValueError(
            f"cannot look up pod {name} ({details}); check the kube login"
        )
    return []


def resolve_target(value: str, role: str, chain: Optional[str]) -> Target:
    """Choose kubectl, ssh, or a local read for one node argument."""
    if os.path.isdir(value):
        return Target(value, role, "local", (value,))
    if "@" in value:
        return Target(value, role, "ssh", (value,))
    if "." in value or ":" in value:
        return Target(value, role, "ssh", (f"{SSH_DEFAULT_USER}@{value}",))
    context, _, name = value.rpartition("/")
    if context and context not in KUBE_CONTEXTS:
        raise ValueError(
            f"unknown cluster {context}; use one of {', '.join(KUBE_CONTEXTS)}"
        )
    pods = find_pods(name, chain, (context,) if context else KUBE_CONTEXTS)
    if len(pods) > 1:
        found = ", ".join("/".join(pod) for pod in pods)
        if len({namespace for _, namespace, _ in pods}) > 1:
            hint = "pass --chain"
        else:
            hint = f"name the cluster, for example {pods[0][0]}/{name}"
        raise ValueError(f"{value} matches several pods ({found}); {hint}")
    if pods:
        return Target(value, role, "kubectl", pods[0])
    if context:
        raise ValueError(f"no pod {name} in cluster {context}")
    # Not a pod: treat it as a host alias from the ssh configuration.
    return Target(value, role, "ssh", (value,))


@dataclasses.dataclass(frozen=True)
class ArchiveFile:
    name: str
    index: int
    sealed: bool
    first_block: int = 0
    last_block: int = 0


@dataclasses.dataclass
class Snapshot:
    files: List[ArchiveFile]
    name: Optional[str] = None
    header: Optional[bytes] = None
    size: int = 0
    data: bytes = b""


def parse_archive_name(name: str) -> Optional[ArchiveFile]:
    sealed = SEALED_RE.match(name)
    if sealed:
        return ArchiveFile(
            name,
            int(sealed.group(1)),
            True,
            int(sealed.group(2)),
            int(sealed.group(3)),
        )
    active = ACTIVE_RE.match(name)
    if active:
        return ArchiveFile(name, int(active.group(1)), False)
    return None


def parse_snapshot(output: bytes) -> Snapshot:
    """Parse the output of READ_SCRIPT."""
    listing, marker, rest = output.partition(b"@@FILE ")
    files = sorted(
        filter(None, map(parse_archive_name, listing.decode().splitlines())),
        key=lambda archive_file: archive_file.index,
    )
    if not marker:
        return Snapshot(files)
    name, _, rest = rest.partition(b"\n@@HEAD\n")
    header, _, rest = rest.partition(b"\n@@SIZE ")
    size, separator, data = rest.partition(b"\n@@DATA\n")
    if not separator or not size.strip():
        # The file was renamed while it was read.
        return Snapshot(files)
    return Snapshot(
        files,
        name.decode(),
        header if header.endswith(b"\n") else None,
        int(size.strip()),
        data,
    )


def parse_header(line: bytes) -> List[str]:
    fields = next(csv.reader([line.decode().rstrip("\n")]))
    if not fields or fields[0] != "block_number":
        raise ValueError(f"unexpected hash-log header: {fields}")
    return fields


class NodeReader:
    """Read complete hash-log rows from one node in archive order."""

    def __init__(self, target: Target, from_height: int = 0) -> None:
        self.target = target
        self.from_height = from_height
        self.index: Optional[int] = None
        self.offset = 0
        self.header: Optional[List[str]] = None
        self.skip_partial_line = False
        self.started = False

    def read(self, index: int, offset: int, limit: int) -> Snapshot:
        command = self.target.command(str(index), str(offset), str(limit))
        try:
            result = subprocess.run(  # noqa: S603 - fixed argument vector
                command,
                capture_output=True,
                timeout=COMMAND_TIMEOUT_SECONDS,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired) as error:
            raise TransportError(str(error)) from error
        if result.returncode != 0:
            message = last_line(result.stderr.decode(errors="replace"))
            raise TransportError(message or f"exit status {result.returncode}")
        return parse_snapshot(result.stdout)

    def poll(self) -> Tuple[List[Tuple[int, Dict[str, str]]], bool]:
        """Return new rows as (height, hashes), and whether more are ready."""
        if self.index is None:
            self._start()
            return [], self.index is not None
        snapshot = self.read(self.index, self.offset, READ_LIMIT_BYTES)
        if snapshot.name is None:
            return [], self._handle_missing_file(snapshot.files)

        data = snapshot.data
        if self.header is None:
            if snapshot.header is None:
                return [], self._finish_file(snapshot)
            self.header = parse_header(snapshot.header)
        if self.offset == 0:
            data = data[len(snapshot.header or b""):]
            self.offset = len(snapshot.header or b"")
        if self.skip_partial_line:
            newline = data.find(b"\n")
            if newline < 0:
                self.offset += len(data)
                return [], False
            self.offset += newline + 1
            data = data[newline + 1:]
            self.skip_partial_line = False

        complete = data[: data.rfind(b"\n") + 1]
        self.offset += len(complete)
        rows = self._parse_rows(complete)
        if len(snapshot.data) >= READ_LIMIT_BYTES:
            return rows, True
        return rows, self._finish_file(snapshot)

    def _start(self) -> None:
        files = self.read(-1, 0, 0).files
        if not files:
            return
        if self.from_height:
            first = files[0]
            if first.sealed and first.first_block > self.from_height:
                raise CoverageGap(
                    f"height {self.from_height} predates retained height "
                    f"{first.first_block}"
                )
            start = next(
                (
                    archive_file
                    for archive_file in files
                    if not archive_file.sealed
                    or archive_file.last_block >= self.from_height
                ),
                files[-1],
            )
            self.index = start.index
            return
        latest = self.read(files[-1].index, 0, 0)
        if latest.name is None:
            return
        self.index = files[-1].index
        if latest.header is None:
            return
        self.header = parse_header(latest.header)
        header_end = len(latest.header)
        self.offset = max(header_end, latest.size - TIP_BACKTRACK_BYTES)
        self.skip_partial_line = self.offset > header_end

    def _handle_missing_file(self, files: List[ArchiveFile]) -> bool:
        assert self.index is not None
        if any(archive_file.index == self.index for archive_file in files):
            return True
        if self.offset > 0:
            raise CoverageGap(
                f"hash-log file index {self.index} was removed while it was read"
            )
        # HashLogger removes a file that never received a row.
        return self._move_to_next_file(files)

    def _finish_file(self, snapshot: Snapshot) -> bool:
        """Move past a sealed file that was read to its end."""
        if snapshot.name is None or not snapshot.name.endswith(".hlog"):
            return False
        return self._move_to_next_file(snapshot.files)

    def _move_to_next_file(self, files: List[ArchiveFile]) -> bool:
        assert self.index is not None
        later = [f for f in files if f.index > self.index]
        if not later:
            return False
        self.index = later[0].index
        self.offset = 0
        self.header = None
        self.skip_partial_line = False
        return True

    def _parse_rows(self, data: bytes) -> List[Tuple[int, Dict[str, str]]]:
        assert self.header is not None
        rows = []
        for line in data.splitlines():
            fields = next(csv.reader([line.decode()]))
            if len(fields) != len(self.header):
                raise ValueError(
                    f"expected {len(self.header)} fields, got {len(fields)}"
                )
            height = int(fields[0])
            if self.from_height and not self.started and height > self.from_height:
                raise CoverageGap(
                    f"height {self.from_height} predates first retained "
                    f"height {height}"
                )
            self.started = True
            if height < self.from_height:
                continue
            rows.append((height, dict(zip(self.header[1:], fields[1:]))))
        return rows


def comparable_hashes(hashes: Dict[str, Any]) -> Dict[str, Any]:
    """Return the hashes that do not depend on the storage backend."""
    base = {"blockHash", "changeset", "resultHash"}
    missing = sorted(base - hashes.keys())
    if missing:
        raise ValueError(
            "row is missing required comparable hashes: " + ", ".join(missing)
        )
    comparable = {
        name: value
        for name, value in hashes.items()
        if name in base
        or (name.startswith("memIAVL/mod/") and name != "memIAVL/mod/evm")
    }
    if len(comparable) == len(base):
        raise ValueError("row has no comparable non-EVM module hashes")
    return comparable


def differing_columns(
    records: Dict[str, Dict[str, Any]],
) -> Dict[str, Dict[str, List[str]]]:
    """Group node labels by value for each column that is not unanimous."""
    columns = sorted(set().union(*(hashes.keys() for hashes in records.values())))
    result = {}
    for column in columns:
        by_value: Dict[str, List[str]] = collections.defaultdict(list)
        for label, hashes in records.items():
            by_value[str(hashes.get(column))].append(label)
        if len(by_value) > 1:
            result[column] = dict(by_value)
    return result


def last_line(text: str) -> str:
    lines = [line for line in text.strip().splitlines() if line.strip()]
    return lines[-1] if lines else ""


def atomic_write(path: pathlib.Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as temporary:
            temporary.write(content)
            temporary.flush()
            os.fsync(temporary.fileno())
        os.replace(temporary_name, path)
    finally:
        try:
            os.unlink(temporary_name)
        except FileNotFoundError:
            pass


def write_status(path: Optional[pathlib.Path], ok: bool, height: int) -> None:
    if path is None:
        return
    content = (
        "# HELP hashlog_compare_ok Whether the latest comparison matched.\n"
        "# TYPE hashlog_compare_ok gauge\n"
        f"hashlog_compare_ok {1 if ok else 0}\n"
        "# HELP hashlog_compare_last_height Last fully compared height.\n"
        "# TYPE hashlog_compare_last_height gauge\n"
        f"hashlog_compare_last_height {height}\n"
        "# HELP hashlog_compare_last_success_timestamp_seconds "
        "Unix time of the latest matching comparison.\n"
        "# TYPE hashlog_compare_last_success_timestamp_seconds gauge\n"
        "hashlog_compare_last_success_timestamp_seconds "
        f"{time.time() if ok else 0:.3f}\n"
    )
    atomic_write(path, content)


def state_path(chain: str, targets: List[Target]) -> pathlib.Path:
    key = ",".join(sorted(f"{t.role}={t.label}" for t in targets))
    digest = hashlib.sha256(key.encode()).hexdigest()[:12]
    return STATE_DIR / f"{chain}-{digest}.json"


def load_resume_height(path: pathlib.Path) -> int:
    try:
        return int(json.loads(path.read_text())["last_height"]) + 1
    except FileNotFoundError:
        return 0


class Comparator:
    """Align rows from all nodes by height and compare them."""

    def __init__(
        self,
        args: argparse.Namespace,
        targets: List[Target],
        state_file: pathlib.Path,
        from_height: int,
    ) -> None:
        self.args = args
        self.targets = targets
        self.state_file = state_file
        self.from_height = from_height
        self.labels = [target.label for target in targets]
        self.baseline = next(
            (t.label for t in targets if t.role == "reserve"), self.labels[0]
        )
        self.buffered: Dict[str, Dict[int, Dict[str, Any]]] = {
            label: {} for label in self.labels
        }
        self.first_seen: Dict[str, int] = {}
        self.last_seen: Dict[str, int] = {}
        self.caught_up = {label: False for label in self.labels}
        self.last_progress = {label: time.monotonic() for label in self.labels}
        self.last_error: Dict[str, str] = {}
        self.matched: "collections.OrderedDict[int, Dict[str, Any]]" = (
            collections.OrderedDict()
        )
        self.next_height: Optional[int] = None
        self.matched_count = 0
        self.state_saved_at = 0.0

    def run(self) -> int:
        events: "queue.Queue[Tuple[str, str, Any, bool]]" = queue.Queue()
        stop = threading.Event()
        for target in self.targets:
            threading.Thread(
                target=poll_node,
                args=(target, self.from_height, self.args.poll_interval, events, stop),
                daemon=True,
            ).start()
        try:
            while True:
                try:
                    kind, label, value, more = events.get(timeout=1)
                except queue.Empty:
                    kind = ""
                result = self._handle(kind, label, value, more) if kind else None
                if result is None:
                    result = self._check_stale()
                if result is not None:
                    return result
        finally:
            stop.set()
            self._save_state(force=True)

    def _handle(self, kind: str, label: str, value: Any, more: bool) -> Optional[int]:
        if kind == "fatal":
            return self.alarm("source_error", {"node": label, "error": value})
        if kind == "error":
            if self.last_error.get(label) != value:
                print(f"{label}: {value}", file=sys.stderr, flush=True)
            self.last_error[label] = value
            return None
        self.last_error.pop(label, None)
        self.caught_up[label] = not more
        for height, hashes in value:
            result = self._add_row(label, height, hashes)
            if result is not None:
                return result
        return self._advance()

    def _add_row(self, label: str, height: int, hashes: Dict[str, str]) -> Optional[int]:
        try:
            comparable = comparable_hashes(hashes)
        except ValueError as error:
            return self.alarm(
                "invalid_row", {"node": label, "error": str(error)}, height
            )
        self.last_progress[label] = time.monotonic()
        self.first_seen.setdefault(label, height)
        self.last_seen[label] = max(self.last_seen.get(label, 0), height)
        earlier = self.buffered[label].get(height) or self.matched.get(height)
        if earlier is not None:
            if earlier != comparable:
                return self.alarm(
                    "reexecution_mismatch",
                    {"node": label, "columns": differing_columns(
                        {"earlier": earlier, label: comparable}
                    )},
                    height,
                )
            return None
        if self.next_height is None or height >= self.next_height:
            self.buffered[label][height] = comparable
        return None

    def _advance(self) -> Optional[int]:
        if self.next_height is None:
            if len(self.first_seen) < len(self.labels):
                return None
            self.next_height = self.from_height or max(self.first_seen.values())
            for rows in self.buffered.values():
                for height in [h for h in rows if h < self.next_height]:
                    del rows[height]

        while all(self.next_height in rows for rows in self.buffered.values()):
            height = self.next_height
            records = {label: self.buffered[label].pop(height) for label in self.labels}
            if any(hashes != records[self.baseline] for hashes in records.values()):
                return self.alarm(
                    "hash_mismatch",
                    {"baseline": self.baseline, "columns": differing_columns(records)},
                    height,
                )
            self._record_match(height, records[self.baseline])
            if self.args.max_heights and self.matched_count >= self.args.max_heights:
                return 0

        skipped = [
            label
            for label, seen in self.last_seen.items()
            if seen > self.next_height and self.next_height not in self.buffered[label]
        ]
        if skipped:
            return self.alarm("height_gap", {"nodes": skipped})
        if all(self.caught_up.values()) and len(self.last_seen) == len(self.labels):
            lag = max(self.last_seen.values()) - min(self.last_seen.values())
            if lag > self.args.max_lag_blocks:
                return self.alarm(
                    "node_lag", {"last_seen": self.last_seen, "lag_blocks": lag}
                )
        return None

    def _record_match(self, height: int, hashes: Dict[str, Any]) -> None:
        self.matched[height] = hashes
        while len(self.matched) > MATCHED_HISTORY:
            self.matched.popitem(last=False)
        self.next_height = height + 1
        self.matched_count += 1
        write_status(self.args.status_file, True, height)
        self._save_state()
        if self.matched_count == 1 or self.matched_count % 100 == 0:
            print(
                f"height {height}: {len(self.labels)} nodes agree "
                f"({self.matched_count} heights compared)",
                flush=True,
            )

    def _save_state(self, force: bool = False) -> None:
        if not self.matched:
            return
        now = time.monotonic()
        if not force and now - self.state_saved_at < 2:
            return
        self.state_saved_at = now
        content = json.dumps(
            {
                "chain": self.args.chain,
                "nodes": {t.label: t.role for t in self.targets},
                "last_height": next(reversed(self.matched)),
            },
            sort_keys=True,
        )
        atomic_write(self.state_file, content + "\n")

    def _check_stale(self) -> Optional[int]:
        now = time.monotonic()
        stale = {
            label: self.last_error.get(label, "no new rows")
            for label, seen in self.last_progress.items()
            if now - seen > self.args.source_timeout
        }
        if stale:
            return self.alarm("node_stale", {"nodes": stale})
        return None

    def alarm(self, kind: str, details: Dict[str, Any], height: Optional[int] = None) -> int:
        if height is None:
            height = self.next_height or 0
        return send_alarm(self.args, kind, height, details)


def poll_node(
    target: Target,
    from_height: int,
    interval: float,
    events: "queue.Queue[Tuple[str, str, Any, bool]]",
    stop: threading.Event,
) -> None:
    reader = NodeReader(target, from_height)
    while not stop.is_set():
        try:
            rows, more = reader.poll()
        except TransportError as error:
            events.put(("error", target.label, str(error), False))
        except CoverageGap as error:
            message = f"{error}; restart with --from-tip to skip the gap"
            events.put(("fatal", target.label, message, False))
            return
        except ValueError as error:
            events.put(("fatal", target.label, str(error), False))
            return
        else:
            events.put(("rows", target.label, rows, more))
            if more:
                continue
        stop.wait(interval)


def send_alarm(
    args: argparse.Namespace, kind: str, height: int, details: Dict[str, Any]
) -> int:
    line = json.dumps(
        {
            "kind": kind,
            "chain_id": args.chain,
            "height": height,
            "details": details,
            "observed_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        },
        sort_keys=True,
    )
    print(line, file=sys.stderr, flush=True)
    if args.mismatch_file:
        args.mismatch_file.parent.mkdir(parents=True, exist_ok=True)
        with args.mismatch_file.open("a") as output:
            output.write(line + "\n")
            output.flush()
            os.fsync(output.fileno())
    if args.alarm_webhook:
        request = urllib.request.Request(
            args.alarm_webhook,
            data=line.encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                if response.status < 200 or response.status >= 300:
                    raise RuntimeError(f"alarm webhook returned HTTP {response.status}")
        except Exception as error:  # noqa: BLE001 - alert path must report all
            print(f"alarm webhook failed: {error}", file=sys.stderr)
    write_status(args.status_file, False, height)
    return 2


def parse_args(argv: Optional[List[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument(
        "--prod", action="append", default=[], metavar="NODE",
        help="production node: [CLUSTER/]POD, IP, DNS name, or user@host",
    )
    parser.add_argument(
        "--reserve", action="append", default=[], metavar="NODE",
        help="reserve node: [CLUSTER/]POD, IP, DNS name, or user@host",
    )
    parser.add_argument(
        "--chain", help="chain id; also the pod namespace (default: from the pods)"
    )
    start = parser.add_mutually_exclusive_group()
    start.add_argument(
        "--from-height", type=int, default=0,
        help="compare from this height instead of the saved position",
    )
    start.add_argument(
        "--from-tip", action="store_true",
        help="ignore the saved position and start at the chain tip",
    )
    parser.add_argument("--status-file", type=pathlib.Path,
                        help="Prometheus textfile for the comparison status")
    parser.add_argument("--mismatch-file", type=pathlib.Path,
                        help="append each alarm as one JSON line")
    parser.add_argument("--alarm-webhook", help="POST each alarm to this URL")
    parser.add_argument("--max-lag-blocks", type=int, default=50)
    parser.add_argument("--source-timeout", type=float, default=60,
                        help="seconds without new rows before a node_stale alarm")
    parser.add_argument("--poll-interval", type=float, default=2)
    parser.add_argument("--max-heights", type=int, default=0,
                        help="exit 0 after this many matched heights")
    args = parser.parse_args(argv)
    if len(args.prod) + len(args.reserve) < 2:
        parser.error("give at least two nodes with --prod and --reserve")
    if args.from_height < 0 or args.max_heights < 0 or args.max_lag_blocks < 0:
        parser.error("heights and block counts cannot be negative")
    if args.source_timeout <= 0 or args.poll_interval <= 0:
        parser.error("--source-timeout and --poll-interval must be positive")
    return args


def resolve_targets(args: argparse.Namespace) -> List[Target]:
    nodes = [(v, "prod") for v in args.prod] + [(v, "reserve") for v in args.reserve]
    labels = [value for value, _ in nodes]
    if len(set(labels)) != len(labels):
        raise ValueError("each node can be given only once")
    targets = [resolve_target(value, role, args.chain) for value, role in nodes]
    if args.chain is None:
        namespaces = {t.address[1] for t in targets if t.transport == "kubectl"}
        if len(namespaces) != 1:
            raise ValueError("cannot infer the chain id; pass --chain")
        args.chain = namespaces.pop()
    return targets


def main(argv: Optional[List[str]] = None) -> int:
    args = parse_args(argv)
    try:
        targets = resolve_targets(args)
        for target in targets:
            NodeReader(target).read(-1, 0, 0)
    except (ValueError, TransportError) as error:
        print(f"hashlog-watch: {error}", file=sys.stderr)
        return 1

    state_file = state_path(args.chain, targets)
    from_height = args.from_height
    if not from_height and not args.from_tip:
        from_height = load_resume_height(state_file)
    for target in targets:
        print(f"{target.role:8} {target.label:24} {target.describe()}", file=sys.stderr)
    start = f"height {from_height}" if from_height else "the chain tip"
    print(f"chain {args.chain}, start at {start}, state {state_file}", file=sys.stderr)

    try:
        return Comparator(args, targets, state_file, from_height).run()
    except KeyboardInterrupt:
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
