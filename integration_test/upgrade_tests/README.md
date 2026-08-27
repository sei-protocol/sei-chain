# Upgrade tests

Records how a live network answers for the modules an upgrade retires, then
verifies the answers once the upgrade has landed.

Because the upgrade happens when the network upgrades, this is **two separately
invoked phases** with an artifact carried between them.

```
                  record  ──── artifact ────►  verify
             (before upgrade)              (after upgrade)
```

## Running it

```bash
npm install

# Before the upgrade. Writes artifacts/pre-upgrade.json. Keep that file.
UPGRADE_TEST_NETWORK=arctic-1 npm run record

# After the upgrade has landed on that network.
UPGRADE_TEST_NETWORK=arctic-1 npm run verify

# Checks on the suite itself. No chain needed; runs in CI on every PR.
npm run selftest
npm run typecheck
```

The read-only probes need no key and no funding, so `npm run record` works
against a public endpoint as-is. Set `UPGRADE_TEST_MNEMONIC` to also send
transaction probes, which is what lets the verify phase check whether committed
history survived. Without it that check is skipped and says so.

| Variable | Default | Purpose |
| --- | --- | --- |
| `UPGRADE_TEST_NETWORK` | `arctic-1` | `local`, `arctic-1`, or `atlantic-2` |
| `UPGRADE_TEST_PLAN_NAME` | `v6.7` | Governance plan name, the key `applied_plan` is stored under |
| `UPGRADE_TEST_REMOVED_MODULES` | `capability feegrant ibc transfer` | Modules the plan deletes from the version map |
| `UPGRADE_TEST_ARTIFACT` | `artifacts/pre-upgrade.json` | Where the artifact is written and read |
| `UPGRADE_TEST_EVM_RPC` | per network | Override, e.g. an archive node |
| `UPGRADE_TEST_REST` | per network | Cosmos REST override |
| `UPGRADE_TEST_MNEMONIC` | unset | Funded key for the transaction probes; falls back to `DAPP_TESTS_MNEMONIC` |

## What it asserts

**The gate.** The verify phase refuses to run unless `applied_plan/<name>` is
non-zero *and* greater than the height the artifact was recorded at. Without
both halves a run against a not-yet-upgraded chain would report a clean pass on
everything that must not change and a confusing failure on everything that
must — the opposite of a useful signal. The gate is a pure function
(`upgradeGateRefusal`) so it is unit tested rather than only exercised live.

**Invariants** must answer identically on both sides, and a difference fails.
These are surfaces the upgrade does not claim to touch, so a failure is
unambiguous.

**Transitions** are expected to change. They are recorded on both sides and
printed as a before/after diff. The specific changes the upgrade is specified to
make get their own named assertions; the rest are reported for a human to read.

**History** committed before the upgrade must still be served afterwards:
`eth_call` at the recorded heights must return the same answer, receipts must
still resolve with the same status and block, and a transaction that failed must
still fail for the same reason.

## Two things worth knowing

**A live network runs a released binary, not this checkout.** arctic-1 is on
v6.6.1, where the oracle query handlers still serve data and the upgrade
precompile at `0x…1015` is not registered at all. So the oracle surfaces are
classified as transitions here rather than invariants: from that network's point
of view the oracle deprecation arrives *with* v6.7. Classifying head-of-tree
behaviour as an invariant would fail the run for the wrong reason.

**The upgrade module is read over Cosmos REST, not the upgrade precompile.**
The one fact the verify phase cannot work without is whether the upgrade ran,
and a precompile that is absent on the binary being tested cannot answer it.
`applied_plan` and `module_versions` therefore come from REST, which is proven
against a real past upgrade: arctic-1 reports v6.3.0 applied at height
138093082.

## Where this sits

Five layers test the same upgrade, trading control for realism:

| Layer | Upgrade is | Pre-upgrade state | Runtime |
| --- | --- | --- | --- |
| `app/upgrade_v67_test.go` | `ApplyUpgrade` in process | synthetic store writes | seconds |
| `make local-upgrade-test` | real gov proposal, one node | empty | ~30s |
| `make cross-upgrade-test` | real gov proposal, **two binaries** | **real** | ~30s + old build |
| `integration_test/upgrade_module/retired_modules_upgrade_test.yaml` | real gov proposal, four nodes | empty | minutes, needs docker |
| this suite | whenever the network upgrades | whatever the network has | two runs, days apart |

The in-process Go tests call `ApplyUpgrade` directly, which never writes
`upgrade-info.json` and never reloads the stores, so `App.SetStoreUpgradeHandlers`
— the code that decides whether a store is dropped at an upgrade height — is not
reached by any of them. `make local-upgrade-test` is the cheapest layer that
does reach it, and `make cross-upgrade-test` is the only one where the
pre-upgrade state was written by modules that were actually alive.

`app/upgrade_orphan_test.go` guards against a removed module leaving a stale
version entry behind. arctic-1 carries `dex` and `accesscontrol` entries from
removals that predate the guard.
