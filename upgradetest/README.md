# Minor upgrade tests

Version-specific upgrade tests stay beside the existing app upgrade tests. The
v6.7 test is `app/upgrade_v67_test.go`; its build tag is `upgrade_v67`.

## Define the next upgrade

From the repository root:

```bash
make new-upgrade-test FROM=v6.7 TO=v6.8
```

This creates `app/upgrade_v68_test.go` with:

- the `upgrade_v68` build tag;
- the same `newV68Chain` / `applyV68` structure used by the v6.7 tests;
- before/after callbacks for the real two-binary boundary;
- a failing TODO test so an empty scaffold cannot pass CI.

Replace the TODO with ordinary Go tests in that file. Keep generic upgrade
tests, such as orphaned module-version checks, in the existing untagged
`app/upgrade_test.go` and `app/upgrade_orphan_test.go`.

When `v6.8` is appended to `app/tags`, CI derives `upgrade_v68` automatically.
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
make upgrade-test-cross-version \
  FROM_REF=release/v6.6 TO_REF=release/v6.7
```

The runner checks out both refs into detached worktrees and builds one `seid`
from each. Four validators create the tagged test's fixtures with the source
binary, pass the governance upgrade height, halt, and restart with the target
binary against the same homes. The same `upgrade_v67` tag then selects the
after-upgrade assertions.

The source callback must prove its fixture worked before recording it. The
target callback must test the transition, not merely repeat a behavior of the
target binary. `upgradetest.CrossVersion.Record` and `Replay` carry fixture
identities and observations between the two test processes; chain state itself
must stay in the validator database.
