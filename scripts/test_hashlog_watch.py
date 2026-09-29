#!/usr/bin/env python3

import importlib.util
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

SCRIPT = pathlib.Path(__file__).with_name("hashlog-watch.py")
SPEC = importlib.util.spec_from_file_location("hashlog_watch", SCRIPT)
assert SPEC and SPEC.loader
watch = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = watch
SPEC.loader.exec_module(watch)

HEADER = "block_number,blockHash,changeset,resultHash,memIAVL/mod/bank,appHash\n"


def row(height: int, change: str = "change", app: str = "app") -> str:
    return f"{height},block{height},{change},result{height},bank,{app}\n"


def rows(first: int, last: int, **kwargs: str) -> str:
    return "".join(row(height, **kwargs) for height in range(first, last + 1))


def local_reader(directory: pathlib.Path, from_height: int = 0):
    target = watch.Target(str(directory), "prod", "local", (str(directory),))
    return watch.NodeReader(target, from_height)


def drain(reader) -> list:
    heights = []
    while True:
        new_rows, more = reader.poll()
        heights.extend(height for height, _ in new_rows)
        if not more:
            return heights


class NodeReaderTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self.tmp.name)

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def append(self, name: str, text: str) -> None:
        with (self.dir / name).open("a") as output:
            output.write(text)

    def test_tip_start_skips_old_history_and_follows_new_rows(self) -> None:
        self.append("0-v1.hlog.u", HEADER + rows(1, 2000))
        reader = local_reader(self.dir)
        tip = drain(reader)
        self.assertTrue(tip)
        self.assertGreater(tip[0], 1000)
        self.assertEqual(2000, tip[-1])
        self.assertEqual(list(range(tip[0], 2001)), tip)

        self.append("0-v1.hlog.u", row(2001))
        self.assertEqual([2001], drain(reader))

    def test_partial_line_waits_for_newline(self) -> None:
        self.append("0-v1.hlog.u", HEADER)
        reader = local_reader(self.dir)
        self.assertEqual([], drain(reader))
        complete = row(1)
        self.append("0-v1.hlog.u", complete[:7])
        self.assertEqual([], drain(reader))
        self.append("0-v1.hlog.u", complete[7:])
        self.assertEqual([1], drain(reader))

    def test_waits_for_first_file_and_lazy_header(self) -> None:
        reader = local_reader(self.dir)
        self.assertEqual([], drain(reader))
        (self.dir / "0-v1.hlog.u").touch()
        self.assertEqual([], drain(reader))
        self.append("0-v1.hlog.u", HEADER[:10])
        self.assertEqual([], drain(reader))
        self.append("0-v1.hlog.u", HEADER[10:] + row(1))
        self.assertEqual([1], drain(reader))

    def test_backfill_follows_seal_and_rotation(self) -> None:
        self.append("0-1-3-v1.hlog", HEADER + rows(1, 3))
        self.append("1-v1.hlog.u", HEADER + rows(4, 5))
        reader = local_reader(self.dir, from_height=2)
        self.assertEqual([2, 3, 4, 5], drain(reader))

        self.append("1-v1.hlog.u", row(6))
        (self.dir / "1-v1.hlog.u").rename(self.dir / "1-4-6-v1.hlog")
        self.append("2-v1.hlog.u", HEADER + row(7))
        self.assertEqual([6, 7], drain(reader))

    def test_new_file_may_change_columns(self) -> None:
        self.append("0-1-1-v1.hlog", HEADER + row(1))
        wider = HEADER.rstrip("\n") + ",memIAVL/mod/acc\n"
        self.append("1-v1.hlog.u", wider + row(2).rstrip("\n") + ",acc\n")
        reader = local_reader(self.dir, from_height=1)
        new_rows, _ = reader.poll()
        new_rows += reader.poll()[0] + reader.poll()[0] + reader.poll()[0]
        self.assertEqual([1, 2], [height for height, _ in new_rows])
        self.assertEqual("acc", new_rows[1][1]["memIAVL/mod/acc"])

    def test_crash_sealed_file_with_torn_tail_moves_on(self) -> None:
        self.append("0-1-2-v1.hlog", HEADER + rows(1, 2) + "3,blo")
        self.append("1-v1.hlog.u", HEADER + rows(3, 4))
        reader = local_reader(self.dir, from_height=1)
        self.assertEqual([1, 2, 3, 4], drain(reader))

    def test_removed_empty_file_moves_on(self) -> None:
        self.append("0-v1.hlog.u", "")
        reader = local_reader(self.dir)
        self.assertEqual([], drain(reader))
        (self.dir / "0-v1.hlog.u").unlink()
        self.append("1-v1.hlog.u", HEADER + row(1))
        self.assertEqual([1], drain(reader))

    def test_large_backfill_reports_more(self) -> None:
        self.append("0-v1.hlog.u", HEADER + rows(1, 200))
        reader = local_reader(self.dir, from_height=1)
        with mock.patch.object(watch, "READ_LIMIT_BYTES", 1024):
            self.assertEqual(list(range(1, 201)), drain(reader))

    def test_requested_height_before_sealed_retention(self) -> None:
        self.append("2-20-21-v1.hlog", HEADER + rows(20, 21))
        with self.assertRaises(watch.CoverageGap):
            drain(local_reader(self.dir, from_height=10))

    def test_requested_height_before_active_file_history(self) -> None:
        self.append("2-v1.hlog.u", HEADER + row(20))
        with self.assertRaises(watch.CoverageGap):
            drain(local_reader(self.dir, from_height=10))

    def test_missing_directory_is_a_transport_error(self) -> None:
        reader = local_reader(self.dir / "missing")
        with self.assertRaisesRegex(watch.TransportError, "not found"):
            reader.poll()


class ResolveTargetTest(unittest.TestCase):
    def lookup(self, found: dict):
        def fake(context: str, name: str) -> list:
            result = found.get((context, name), [])
            if isinstance(result, Exception):
                raise result
            return result

        return mock.patch.object(watch, "run_kubectl_lookup", side_effect=fake)

    def test_ip_and_dns_use_ssh_with_default_user(self) -> None:
        for value in ("3.68.111.103", "archive-0.atlantic-2.example.io"):
            target = watch.resolve_target(value, "reserve", None)
            self.assertEqual("ssh", target.transport)
            self.assertEqual((f"ec2-user@{value}",), target.address)

    def test_explicit_user_is_kept(self) -> None:
        target = watch.resolve_target("ubuntu@10.0.0.1", "reserve", None)
        self.assertEqual(("ubuntu@10.0.0.1",), target.address)

    def test_pod_name_uses_kubectl_in_the_cluster_that_has_it(self) -> None:
        with self.lookup({("prod-use2", "rpc-node-0-0-0"): ["arctic-1"]}):
            target = watch.resolve_target("rpc-node-0-0-0", "prod", None)
        self.assertEqual("kubectl", target.transport)
        self.assertEqual(("prod-use2", "arctic-1", "rpc-node-0-0-0"), target.address)
        command = target.command("-1", "0", "0")
        self.assertEqual(["kubectl", "--context", "prod-use2", "-n", "arctic-1"],
                         command[:5])

    def test_seinode_name_finds_its_pod(self) -> None:
        with self.lookup({("prod", "rpc-node-1-0-0"): ["arctic-1"]}):
            target = watch.resolve_target("rpc-node-1-0", "prod", None)
        self.assertEqual(("prod", "arctic-1", "rpc-node-1-0-0"), target.address)

    def test_chain_selects_between_namespaces(self) -> None:
        found = {("prod", "rpc-node-0-0-0"): ["arctic-1", "atlantic-2"]}
        with self.lookup(found):
            with self.assertRaisesRegex(ValueError, "--chain"):
                watch.resolve_target("rpc-node-0-0-0", "prod", None)
            target = watch.resolve_target("rpc-node-0-0-0", "prod", "atlantic-2")
        self.assertEqual("atlantic-2", target.address[1])

    def test_same_pod_in_several_clusters_asks_for_the_cluster(self) -> None:
        found = {
            (context, "rpc-node-0-0-0"): ["arctic-1"]
            for context in watch.KUBE_CONTEXTS
        }
        with self.lookup(found):
            with self.assertRaisesRegex(ValueError, "prod/rpc-node-0-0-0"):
                watch.resolve_target("rpc-node-0-0-0", "prod", "arctic-1")
            target = watch.resolve_target("prod-euw1/rpc-node-0-0-0", "prod", None)
        self.assertEqual(("prod-euw1", "arctic-1", "rpc-node-0-0-0"), target.address)

    def test_unknown_cluster_prefix_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "unknown cluster"):
            watch.resolve_target("staging/rpc-node-0-0-0", "prod", None)

    def test_lookup_failure_is_reported_instead_of_guessing_ssh(self) -> None:
        failure = watch.TransportError("token expired")
        found = {(context, "rpc-node-0-0-0"): failure for context in watch.KUBE_CONTEXTS}
        with self.lookup(found):
            with self.assertRaisesRegex(ValueError, "token expired"):
                watch.resolve_target("rpc-node-0-0-0", "prod", None)

    def test_unknown_short_name_is_an_ssh_alias(self) -> None:
        with self.lookup({}):
            target = watch.resolve_target("archive-0", "reserve", None)
        self.assertEqual(("ssh", ("archive-0",)), (target.transport, target.address))

    def test_ssh_command_quotes_the_remote_script(self) -> None:
        target = watch.resolve_target("3.68.111.103", "reserve", None)
        command = target.command("-1", "0", "0")
        self.assertEqual("ec2-user@3.68.111.103", command[-2])
        self.assertTrue(command[-1].startswith("sh -c '"))
        self.assertTrue(command[-1].endswith("sh .sei/data/hash.log -1 0 0"))


class CompareTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.tmp.name)
        self.prod = self.root / "prod"
        self.reserve = self.root / "reserve"
        self.prod.mkdir()
        self.reserve.mkdir()
        self.alarms = self.root / "alarms.jsonl"
        state = mock.patch.object(watch, "STATE_DIR", self.root / "state")
        state.start()
        self.addCleanup(state.stop)

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def write(self, directory: pathlib.Path, text: str) -> None:
        with (directory / "0-v1.hlog.u").open("a") as output:
            output.write(text)

    def compare(self, *extra: str) -> int:
        argv = [
            "--chain", "arctic-1",
            "--prod", str(self.prod),
            "--reserve", str(self.reserve),
            "--poll-interval", "0.05",
            "--mismatch-file", str(self.alarms),
            *extra,
        ]
        with mock.patch("sys.stdout"), mock.patch("sys.stderr"):
            return watch.main(argv)

    def alarm(self) -> dict:
        return json.loads(self.alarms.read_text().splitlines()[-1])

    def test_matches_while_ignoring_backend_columns(self) -> None:
        self.write(self.prod, HEADER + rows(1, 3, app="flatkv"))
        self.write(self.reserve, HEADER + rows(1, 3, app="memiavl"))
        self.assertEqual(0, self.compare("--from-height", "1", "--max-heights", "3"))
        self.assertFalse(self.alarms.exists())

    def test_changeset_mismatch_alarms_with_the_columns(self) -> None:
        self.write(self.prod, HEADER + rows(1, 2) + row(3, change="bad"))
        self.write(self.reserve, HEADER + rows(1, 3))
        self.assertEqual(2, self.compare("--from-height", "1"))
        alarm = self.alarm()
        self.assertEqual("hash_mismatch", alarm["kind"])
        self.assertEqual(3, alarm["height"])
        self.assertEqual(
            {"bad": [str(self.prod)], "change": [str(self.reserve)]},
            alarm["details"]["columns"]["changeset"],
        )

    def test_tip_start_aligns_nodes_at_different_heights(self) -> None:
        self.write(self.prod, HEADER + rows(1, 12))
        self.write(self.reserve, HEADER + rows(1, 10))
        self.assertEqual(0, self.compare("--max-heights", "1"))

    def test_height_gap_alarms(self) -> None:
        self.write(self.prod, HEADER + rows(1, 2) + rows(4, 5))
        self.write(self.reserve, HEADER + rows(1, 5))
        self.assertEqual(2, self.compare("--from-height", "1"))
        self.assertEqual("height_gap", self.alarm()["kind"])

    def test_stale_node_alarms(self) -> None:
        self.write(self.prod, HEADER + rows(1, 2))
        self.write(self.reserve, HEADER + rows(1, 2))
        self.assertEqual(2, self.compare("--source-timeout", "0.5"))
        self.assertEqual("node_stale", self.alarm()["kind"])

    def test_resumes_after_the_saved_height(self) -> None:
        self.write(self.prod, HEADER + rows(1, 2))
        self.write(self.reserve, HEADER + rows(1, 2))
        self.assertEqual(0, self.compare("--from-height", "1", "--max-heights", "2"))
        self.write(self.prod, row(3, change="bad"))
        self.write(self.reserve, row(3))
        self.assertEqual(2, self.compare())
        self.assertEqual(3, self.alarm()["height"])

    def test_rejects_a_single_node(self) -> None:
        with mock.patch("sys.stderr"), self.assertRaises(SystemExit):
            watch.parse_args(["--prod", "rpc-node-0-0-0"])


if __name__ == "__main__":
    unittest.main()
