## Summary

Adds per-DB LtHash tracking to each of the four FlatKV data databases (account, code, storage, legacy) alongside the existing global LtHash. The global LtHash is now derived from per-DB hashes via the homomorphic property (sum of per-DB = global), eliminating an independent computation path and making the invariant structural.

**Persistence model**: authoritative per-DB hashes live in `metadataDB` (written atomically with the global hash in `commitGlobalMetadata`); secondary copies are embedded in each DB's `LocalMeta` for per-DB integrity verification.

- `keys.go`: Extends `LocalMeta` with an optional `*lthash.LtHash` field. `MarshalLocalMeta`/`UnmarshalLocalMeta` accept both 8-byte (version-only, old format) and 2056-byte (version + LtHash) formats. Rejects any other length.
- `store.go`: Adds `perDBCommittedLtHash` and `perDBWorkingLtHash` maps to `CommitStore`. `loadGlobalMetadata` now calls `loadPerDBLtHashes` to read per-DB hashes from `metadataDB`; missing keys initialize to zero (fresh start).
- `store_meta.go`: `commitGlobalMetadata` atomically writes per-DB hashes in the same batch as global version and global hash. `snapshotLtHashes` clones all working hashes (global + per-DB) to committed state. `loadPerDBLtHashes` reads per-DB keys from `metadataDB`, initializing to zero on not-found.
- `store_write.go`: `ApplyChangeSets` computes per-DB hashes independently using the already-separated pair slices, then derives the global hash via `Reset()` + `MixIn()`. `commitBatches` embeds the per-DB hash into each DB's `LocalMeta`. Uses a fixed-size `[4]dbPairs` array to avoid per-block map allocation.
- `store_catchup.go`: WAL replay snapshots per-DB hashes via `snapshotLtHashes()` after each replayed version.
- `importer.go`: Snapshot import uses `snapshotLtHashes()` to persist per-DB committed state.

## Test plan

`perdb_lthash_test.go` (11 tests):
- **SkewRecovery**: Tampers accountDB `LocalMeta` to version V-1, reopens, verifies catchup produces correct per-DB hashes via full scan.
- **PersistenceAfterReopen**: 10-block commit cycle, close, reopen, verifies persisted per-DB hashes match full scan and committed == working.
- **IncrementalEqualsFullScan**: 20 blocks with writes, updates, and deletes across all DB types; verifies incremental per-DB hashes match full scan at multiple checkpoints.
- **SumEqualsGlobal**: Verifies the homomorphic invariant (sum of per-DB hashes == global hash) after 5 mixed-state blocks.
- **CatchupReplay**: Snapshot at V2, commit to V5, reopen from snapshot, verifies per-DB hashes after WAL replay match pre-close values.
- **EmptyBlocks**: Verifies 5 empty blocks do not drift per-DB hashes.
- **AfterImport**: Snapshot import via `Importer`, verifies per-DB hashes match full scan and committed == working.
- **Rollback**: Commits to V5, rolls back to V3, verifies per-DB hashes match full scan at V3.
- **PersistedInMetadataDB**: Reads per-DB keys directly from `metadataDB` after commit, verifies they match in-memory committed hashes.
- **AfterDirectImport**: Large `ApplyChangeSets` with 20 pairs, verifies per-DB hashes match full scan.

`keys_test.go`:
- **RoundTripWithLtHash**: Verifies 2056-byte `LocalMeta` round-trip serialization with embedded LtHash.
- Updated existing tests to assert `LtHash == nil` for old 8-byte format.
