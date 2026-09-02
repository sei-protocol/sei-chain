# Minor upgrade tests

Version-specific upgrade tests stay beside the existing app upgrade tests. The
v6.7 test is `app/upgrade_v67_test.go`; its build tag is `upgrade_v67`.

## Define the v6.7 upgrade

From the repository root:

```bash
make new-upgrade-test FROM=v6.6 TO=v6.7
```

This creates `app/upgrade_v67_test.go` and its offline source/target files with:

- the `upgrade_v67` build tag;
- the `newV67Chain` / `applyV67` structure;
- before/after callbacks for the real two-binary boundary;
- separately compiled source/target TODOs for the persisted Go boundary, plus a
  reopen TODO in the source file so the runner's third phase has a test to
  select;
- a failing TODO test so an empty scaffold cannot pass CI.

Replace the TODO with ordinary Go tests in that file. Keep generic upgrade
tests, such as orphaned module-version checks, in the existing untagged
`app/upgrade_test.go` and `app/upgrade_orphan_test.go`.

When `v6.7` is appended to `app/tags`, CI derives `upgrade_v67` automatically.
No version string is hard-coded in the Makefile or workflow.

## Run it

```bash
make upgrade-test
make upgrade-test-vet
```

`upgrade-test` reads the last two minor versions from `app/tags`, derives the
current build tag, and runs the app tests with that file enabled.

`upgrade-test-vet` type-checks every `app/upgrade_v*_test.go` under the tag
implied by its filename, including tests for upgrades that already shipped.

## Run the real boundary

```bash
make upgrade-test-offline \
  FROM_REF=release/v6.6 TO_REF=release/v6.7

make upgrade-test-cross-version \
  FROM_REF=release/v6.6 TO_REF=release/v6.7
```

`upgrade-test-offline` compiles three Go test processes. The source process uses
the old branch's app code to write a committed database; the target process
uses the new branch's app code to reopen that database, apply the handler, and
verify the persisted result; the reopen process compiles the source file again
against the migrated database and records what the old binary does with it. The
runner copies the target branch's tagged test definitions into disposable
worktrees while each phase still links against its own branch.

To point the target phase at a real node home instead of the synthetic
fixture, set `UPGRADE_TEST_SNAPSHOT_HOME` to that directory and run only the
snapshot subtest (the home is written in place):

```bash
UPGRADE_TEST_SNAPSHOT_HOME=/path/to/node/home \
UPGRADE_VERSION_LIST=v6.7 \
go test -tags=upgrade_v67,offline_upgrade,upgrade_target \
  -run 'TestV67OfflineUpgradeTarget/snapshot' -count=1 ./app
```

The path must be a node home: a `config/genesis.json` plus the state
commitment store, at whichever of the two layouts the node was created with.
An unusable path fails; an unset variable skips that subtest and leaves the
fixture path unchanged.
The snapshot must still carry the retired modules in its version map
(pre-v6.7 state).

`upgrade-test-cross-version` builds one `seid` from each ref. Four validators
create fixtures with the source binary, pass the governance upgrade height,
halt, and restart with the target binary against the same homes. The same
`upgrade_v67` tag selects its before- and after-upgrade assertions.

The source callback must prove its fixture worked before recording it. The
target callback must test the transition, not merely repeat a behavior of the
target binary. `upgradetest.CrossVersion.Record` and `Replay` carry fixture
identities and observations between the two test processes; chain state itself
must stay in the validator database.
