## Summary

Add opt-in, exact-version Pebble checkpoints for the State Store. This PR contains snapshot generation and retention only; State Store rollback remains separate.

- `sei-db/db_engine/types/types.go` and `sei-db/db_engine/pebbledb/mvcc/db.go`: add an ordered drain barrier to the single-writer FIFO. A snapshot barrier runs after version H and before H+1, supports shutdown cancellation, and stamps only the latest-version marker in the checkpoint copy.
- `sei-db/state_db/ss/cosmos/store.go` and `sei-db/state_db/ss/evm/store.go`: expose checkpoint scheduling for Cosmos, unified EVM, and separate EVM sub-databases. Composite publication waits for every required checkpoint.
- `sei-db/state_db/ss/evm/store.go`, `sei-db/state_db/ss/composite/store.go`, and MVCC prune paths: report the highest earliest-version marker across managed databases, allow divergent member floors after recovery, and advance prune's earliest marker before deleting history so snapshots can safely inherit per-DB markers.
- `sei-db/state_db/ss/composite/snapshot.go`: publish checkpoints through a temporary directory, durable rename, and atomic `current` symlink. Startup removes stale staging data, restores `current`, resumes from the newest snapshot, and enforces retention even after a failed publish.
- `sei-db/state_db/ss/composite/snapshot.go`: reject startup when any live SS database cannot hardlink into the snapshot root. Custom Cosmos directories place the root in a sibling `<db>-snapshots` directory. Known limitation: all enabled Cosmos and EVM databases must use one filesystem.
- `sei-db/state_db/ss/composite/store.go` and `sei-cosmos/storev2/rootmulti/store.go`: trigger snapshots from one place, `rootmulti.flush`, for both populated and empty blocks. `ApplyChangesetAsync` schedules nothing, so callers outside the commit path do not inherit a trigger, and a state store that cannot schedule fails the build rather than losing snapshots silently. State-sync import and direct version-marker writes cannot publish a partial snapshot.
- `sei-db/config/*.go`, `app/seidb.go`, and `sei-cosmos/server/config/config.go`: keep snapshots default-off. When enabled, SS mirrors SC's effective block interval, minimum time interval, and retention settings. SS and SC apply independent in-flight gates, so they do not guarantee identical retained heights.
- `sei-db/state_db/ss/composite/snapshot.go` and `snapshot_metrics.go`: allow one snapshot in flight, cancel queued work during close, persist apparent-size metadata outside the publish lock, and export attempt, skip, outcome, duration, in-flight, height, retained-count, and apparent-byte metrics.
- `sei-db/common/utils/path.go`: define the shared default snapshot directory name. Managed snapshot paths have no lease; consumers must coordinate with generation and pruning.

## Test plan

- `sei-db/db_engine/pebbledb/mvcc/db_test.go` and `prune_test.go`: checkpoint copies preserve exact latest markers, inherit earliest markers, queued checkpoints cancel before work starts, and prune advances the earliest marker before delete work while preserving pruning coverage.
- `sei-db/state_db/ss/composite/snapshot_test.go`: cover exact labels, exclusion of versions written after the boundary while the write path keeps going, Cosmos-only and EVM-split layouts, idle EVM sub-databases, empty blocks, state-sync isolation, inherited per-store earliest markers, and latest labels across EVM sub-databases.
- `sei-db/state_db/ss/evm/db_test.go` and `sei-db/state_db/ss/composite/recovery_test.go`: cover max earliest-floor reporting and allow recovery with divergent member floors.
- `sei-cosmos/storev2/rootmulti/store_test.go`: cover the commit-path wiring, so a boundary produces a snapshot whether the block carried changesets or not.
- `sei-db/state_db/ss/composite/snapshot_test.go`: cover one-in-flight and minimum-time gates, close during barrier scheduling, custom directories, cross-filesystem rejection, restart resumption, stale staging cleanup, out-of-order publication, failed-publication retention, and persisted size metadata.
- `sei-db/config/ss_config_test.go`, `app/config_fuzz_test.go`, and configuration goldens: cover default-off rollout and effective SC cadence mirroring.
- Verified with `go test -race ./sei-db/db_engine/pebbledb/mvcc ./sei-db/state_db/ss/... ./evmrpc ./sei-cosmos/storev2/...`.
