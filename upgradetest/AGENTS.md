# Version-specific upgrade tests

The tests themselves stay in `app`, following the existing naming and helper
structure:

- `app/upgrade_test.go` and `app/upgrade_orphan_test.go` hold generic checks.
- `app/upgrade_v67_test.go` holds checks specific to the v6.6 -> v6.7 change.
- A version-specific file has the matching build tag, such as `upgrade_v67`.

Do not move app upgrade tests into this package. This package only provides the
small amount of automation needed to select, validate and scaffold those files.

## Adding a minor upgrade

Run:

```bash
make new-upgrade-test FROM=v6.6 TO=v6.7
```

The command creates `app/upgrade_v67_test.go` plus separately compiled
`upgrade_v67_offline_source_test.go` and
`upgrade_v67_offline_target_test.go`. The main file has the matching
`upgrade_v67` constraint, the `newV67Chain` / `applyV67` shape, and
`TestV67CrossVersion` callbacks for the live two-binary path.
The source file also has a reopen TODO so the runner's third phase has a
test to select. Its TODOs fail in the layer that reaches them until real
assertions replace them.

Appending `v6.7` to `app/tags` makes that pair the current boundary.
`make upgrade-test` derives the build tag from the embedded list and runs the
app package with the file enabled. The workflow must not name a version.

`make upgrade-test-vet` compiles every version-specific app test with the tag
implied by its filename. This is required because ordinary untagged tests and
`golangci-lint run` do not type-check build-tagged files.

Run a real branch boundary with:

```bash
make upgrade-test-offline \
  FROM_REF=release/v6.6 TO_REF=release/v6.7

make upgrade-test-cross-version \
  FROM_REF=release/v6.6 TO_REF=release/v6.7
```

Both runners build from detached worktrees pinned to resolved commits.
`upgrade-test-offline` injects the tagged source test into the disposable old
worktree, writes a committed application database, injects the target test into
the new worktree to apply the handler, then runs a reopen phase that compiles
the source test again against the migrated database. `upgrade-test-cross-version`
starts validators on the source binary, runs the tagged test's `before`
callback, executes the governance halt and binary replacement, then runs its
`after` callback against the same node homes.

## Scope

Keep the checks in the same direct style as the existing v6.7 file. Use
`testutil/processblock` for transaction and state assertions, and call
`ApplyUpgrade` through a version-specific helper.

`make upgrade-test` is the fast, in-process layer and uses the current checkout
on both sides. `make upgrade-test-offline` is the persisted, three-phase Go
layer: it reaches no consensus or node lifecycle code. Its source and target
files may use APIs available only on their respective branch because each is
compiled separately. The reopen phase compiles the source file against the
migrated database the target phase left behind. The target phase also accepts
`UPGRADE_TEST_SNAPSHOT_HOME` pointing at a node home: when set,
`TestV67OfflineUpgradeTarget/snapshot` opens that database, applies the
upgrade, and runs the retained-store and version-map assertions against it.
When unset the subtest skips; a path that is not a usable node home fails.
`make upgrade-test-cross-version` owns the full node lifecycle. Keep all
version-specific definitions in tagged app test files; `upgradetest` only
provides selection and coordination.
