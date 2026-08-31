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
make new-upgrade-test FROM=v6.7 TO=v6.8
```

The command creates `app/upgrade_v68_test.go` with a matching `upgrade_v68`
constraint, the same `newV68Chain` / `applyV68` shape as the current test, and
`TestV68CrossVersion` callbacks for the two-binary path. Its TODOs fail in the
layer that reaches them until real assertions replace them.

Appending `v6.8` to `app/tags` makes that pair the current boundary.
`make upgrade-test` derives the build tag from the embedded list and runs the
app package with the file enabled. The workflow must not name a version.

`make upgrade-test-vet` compiles every version-specific app test with the tag
implied by its filename. This is required because ordinary untagged tests and
`golangci-lint run` do not type-check build-tagged files.

Run a real branch boundary with:

```bash
make upgrade-test-cross-version \
  FROM_REF=release/v6.7 TO_REF=release/v6.8
```

The runner builds both refs in detached worktrees. It starts the validators on
the source binary, runs the tagged test's `before` callback, executes the
governance halt and binary replacement, then runs its `after` callback against
the same database. The JSON artifact carried between callbacks is the only
state the test process may carry; application state must travel through the
chain.

## Scope

Keep the checks in the same direct style as the existing v6.7 file. Use
`testutil/processblock` for transaction and state assertions, and call
`ApplyUpgrade` through a version-specific helper.

`make upgrade-test` is the fast, in-process layer and uses the current checkout
on both sides. `make upgrade-test-cross-version` owns the persistent,
two-binary layer. Keep its version-specific setup and assertions in the same
tagged app test; `upgradetest.CrossVersion` only provides process coordination,
validator commands and the phase artifact.
