# FlatKV-only State Sync Findings

## Summary

Current Tendermint/Cosmos state sync remains expensive after a full move to
FlatKV. Export gets materially better because FlatKV can stream Pebble keys
sequentially, but restore still has to ingest every key through the FlatKV
importer, rebuild Pebble LSM state, and recompute LtHash metadata. The existing
benchmarks already show that FlatKV import is neutral-to-slower per byte than
memIAVL import, so a FlatKV-only chain should not expect state sync restore to
fall to restart-like times under the existing SnapshotItem stream.

The promising path is to distribute the FlatKV checkpoint itself. FlatKV local
snapshots are Pebble checkpoints: SST files are hard-linked into
`data/state_commit/flatkv/snapshot-<height>`, and the `current` symlink selects
the active checkpoint. In FlatKV-only mode, that checkpoint is the full
consensus commit-store state, so an out-of-band archive can copy the immutable
checkpoint directory instead of serializing and re-importing every key.

## Current State-sync Bottleneck

State sync today serializes all commit-store data as protobuf `SnapshotItem`
records and writes zlib-compressed chunks. In the memIAVL path this walks IAVL
snapshot nodes. In the FlatKV path the composite commit store emits a `flatkv`
module and then streams physical Pebble keys through the same `SnapshotItem`
format.

Measured behavior from `sto558-artifacts/flatkv-winnings-gsheet.tsv` and
`sto558-artifacts/flatkv-winnings-gsheet-128g-3k.tsv`:

- State-sync snapshot creation improved from roughly 6.3h on memIAVL-only to
  2.1-2.5h on EVM-migrated FlatKV, primarily because less state remains in
  memIAVL and FlatKV is more sequential.
- State-sync restore/import remained close to a wash on 256Gi/10k hardware
  (37m53s vs 34m24s) and only improved to 55m46s from 1h19m on constrained
  128Gi/3k hardware.
- The migrated node restored fewer bytes but took a similar time, which implies
  FlatKV's per-byte import cost is higher than memIAVL's tree-build path. That
  matches the expected Pebble LSM compaction/write-amplification cost plus
  LtHash rebuild.

For FlatKV-only, the key-stream exporter should no longer suffer memIAVL random
read behavior, but restore still has to rebuild Pebble from a stream. A 40GiB
state is therefore still expected to take tens of minutes, not seconds.

## Checkpoint Archive Design

FlatKV already creates local Pebble checkpoints in
`sei-db/state_db/sc/flatkv/snapshot.go`. `WriteSnapshot` writes each FlatKV
database (`account`, `code`, `storage`, `misc`, `metadata`) into a versioned
snapshot directory and updates `current`. Checkpoint creation is hardlink-based,
so it is close to constant time when the source and destination are on the same
filesystem.

The new out-of-band bootstrap path archives:

- `data/state_commit/flatkv/snapshot-<height>/`
- `wasm/`, when present
- `data/state_store/` for latest query/RPC state
- a manifest containing the chain ID, height, app hash, archive format version,
  and per-file SHA-256 entries

Restore downloads the archive, verifies every file against the manifest, installs
the checkpoint directory, installs the query state, updates the `current`
symlink, and bootstraps Tendermint state at the archived height using the
existing light-client state-provider semantics. The node then starts normally
with `statesync.enable = false` and block-syncs from peers.

This changes restore from:

```text
download chunks -> protobuf decode -> per-key FlatKV import -> compaction -> LtHash rebuild
```

to:

```text
download archive -> checksum files -> move immutable checkpoint + query state -> light-client AppHash check
```

At large state sizes, runtime should be dominated by object-store throughput and
local disk extraction rather than consensus-store reconstruction.

## Validation Plan

The first implementation is intentionally out-of-band and validated with the
existing Docker Compose FlatKV-only cluster:

1. Run the existing `integration_test/contracts/verify_flatkv_only_statesync.sh`
   as the baseline key-stream restore path.
2. Create and upload a checkpoint archive from `sei-node-0`.
3. Wipe `sei-node-3`, restore the archive from S3, bootstrap Tendermint state,
   restart the node, and require it to catch up.
4. Compare donor/victim block hashes at a shared height and run the existing EVM
   smoke query.

Small local state validates the mechanism. Large-state performance should be
validated later on a harbor/EKS snapshot because the win scales with state size
and object-store bandwidth.

## Validation Results

Environment: local Docker Compose cluster started with `GIGA_FLATKV_ONLY=true`
on 2026-07-21. The fixture was generated with
`integration_test/contracts/deploy_flatkv_evm_fixture.sh`.

Baseline key-stream state sync:

- Command: `integration_test/contracts/verify_flatkv_only_statesync.sh`
- Result: PASS
- Snapshot height: 600
- Victim: `sei-node-3`
- Wall time: 28s total for the script after fixture generation
- Recovery segment: victim started state sync, accepted snapshot 600, restored
  FlatKV, and caught donor within ~15s on the small local dataset.
- Post-checks: EVM fixture query passed; FlatKV dump digest matched across all
  four validators at height 674.

Out-of-band archive smoke, using a local file rather than S3:

- Create command: `seid flatkv-archive create --home /root/.sei --chain-id sei
  --archive-rpc http://localhost:26657 --out /tmp/flatkv-archive-local.tar.zst`
- Result: PASS
- Archive: checkpoint height 2000, AppHash
  `5F0B1005238CBC09DC0481812F9C91BA39112B778C1867F57192CCD01730EB96`, 51 files.
- Restore command: `seid flatkv-archive restore --home /root/.sei --chain-id sei
  --from /tmp/flatkv-archive-local.tar.zst --verification-rpc
  sei-node-0:26657 --verification-rpc sei-node-1:26657 --trust-height <H>
  --trust-hash <hash> --force`
- Result: PASS
- Bootstrap details: light-client verified the archived AppHash, installed the
  FlatKV checkpoint, restored wasm, bootstrapped Tendermint state, saved the
  corresponding block in blockstore, and started from height 2000.
- Catch-up: victim reached donor gap 0 within ~10s after direct `seid start`.

S3 end-to-end:

- Implementation exists in `seid flatkv-archive create --upload s3://...` and
  `seid flatkv-archive restore --from s3://...`.
- Test script exists at
  `integration_test/contracts/verify_flatkv_archive_restore.sh`.
- Result: PASS with `S3_URI=s3://harbor-validation-results/eng-yiren/flatkv-archive-smoke/archive.tar.zst`.
- Archive: checkpoint height 37000, AppHash
  `C71D76FC56DE1FA2D5C2663FA12FEC04A4F47796E8B3A6F2B71F4A690B8CFF34`, 51 files.
- Create+upload elapsed: 4s.
- Download+restore+bootstrap elapsed: 6s.
- Total script elapsed: 22s.
- Victim caught donor at gap 0, EVM fixture query passed, and donor/victim block
  hashes matched at height 37125.

Important correction from validation: archiving only the FlatKV commit-store is
consensus-correct but insufficient for an RPC node. The victim could handshake
and produce the same block hash, but EVM latest queries returned missing
historical account data until `data/state_store/` was included. The archive now
includes `state_store` by default and skips its live `changelog/` directories to
avoid archiving files that grow while the node is running.


## FlatKV iterator instability: the forked-cluster wedge root cause (2026-07-22)

The first 4-validator forked-pacific-1 FlatKVOnly cluster wedged at block
219060004: fork-0/2/3 panicked deterministically with "unexpected validator in
unbonding queue; status was not unbonding", while fork-1 diverged earlier with
an app-hash mismatch. Both symptoms had one root cause, found by dumping the
flatkv changelog (WAL) of the stalled nodes with `forkdebug --dump-wal`:

- Block 219060003's staking changeset contained **zero** validator-queue
  (`0x43`) or redelegation-queue (`0x42`) delete pairs on fork-0, and fork-1
  additionally lost all 62 unbonding-queue (`0x41`) deletes. The deletions
  never reached the storage layer, and each node lost a different subset —
  hence both the ghost queue entries and the cross-node app-hash divergence.
- The lost deletes are exactly the "delete `iter.Key()` while the iterator is
  open" EndBlock paths in staking (`UnbondAllMatureValidators`,
  `DequeueAllMatureUBDQueue`, `DequeueAllMatureRedelegationQueue`).

Mechanism: FlatKV's iterator lanes passed Pebble's reused key/value buffers
(or subslices, e.g. `StripModulePrefix` in the misc lane) straight through.
Pebble rewrites those buffers on `Next()`. cachekv keys its dirty-entry maps
by zero-copy `UnsafeBytesToStr` strings of the caller's slice, so when staking
does `store.Delete(iter.Key())` and then advances the iterator, the map keys
mutate underneath the map — the delete entries become unreachable and are
silently dropped when cachekv flushes the block's changeset. memiavl/IAVL
iterators return stable slices, so this never fired before FlatKVOnly ran real
cosmos-module EndBlock logic against flatkv-backed non-EVM stores.

Fix (commit 81dd4f446): `CommitStore.Iterator` now wraps its output in
`iterators.NewCopyingIterator`, which snapshots each position's key/value into
fresh buffers, restoring the stability contract callers assume. Regression
test `TestIteratorKeyValueStability` reproduces the production pattern
(retain keys across Next, then delete by the retained keys) — red without the
wrapper, green with it.

## Production-scale validation on the forked pacific-1 cluster (2026-07-22)

Environment: the 4-validator `pacific-fork-1` FlatKVOnly cluster on harbor EKS
(real converted pacific-1 state: 39GiB flatkv checkpoint + 38GiB state_store +
12GiB wasm ≈ 89GiB raw; gp3 PVCs, eu-central-1). Donor: fork-3, held out of
consensus for the create. Victim: a fresh full-node pod with an empty PVC.

Timed results, single run each:

- **Create + S3 upload: 18m38s** (`seid flatkv-archive create` on fork-3).
  Archive: height 219130000, 23,847 files, 81.4GiB tar.zst (Pebble SSTs are
  already compressed, so zstd gains little). Pack ~14m, upload ~4.7m.
- **Restore + bootstrap: 32m35s** (`seid flatkv-archive restore` on the
  victim). Breakdown: S3 download + extract + per-file SHA-256 verify ≈ 32.4m
  (disk-bound: ~170GiB of writes through a 125MiB/s gp3 volume), light-client
  verification + Tendermint bootstrap ≈ 12s.
- **Catch-up: ~4m** to block-sync the ~6,600 blocks produced during restore;
  victim reported `catching_up=false` at donor height. fork-3 rejoined
  consensus cleanly after being unfrozen.

Comparison with the measured key-stream state sync baselines on comparable
pacific-1 state (sto558 gsheets): snapshot creation 2.1–2.5h (EVM-migrated) /
6.3h (memiavl-only) vs **18.6m** here (~7–20x); restore 55m–1h19m on
128Gi/3k hardware vs **32.6m** — and the archive additionally carries
state_store and wasm, which the key-stream path does not transfer at all
(a state-synced RPC node still has to rebuild or resync those). Restore is now
bandwidth/disk-bound rather than CPU-bound (no per-key import, no LSM
rebuild, no LtHash recomputation), so provisioned IOPS/throughput translate
directly into faster restores.

Operational findings from this run:

1. **Create requires a quiesced node.** *(Resolved 2026-07-26 — see the
   live-donor section below.)* The first archive was created on a
   live validator and uploaded fine, but restore failed checksum verification
   (`sha256 mismatch for state_store/.../037313.log`): the flatkv checkpoint
   is immutable, but `state_store` is a live Pebble instance whose files
   mutate between manifest hashing and tar write. A second live attempt
   failed harder (file deleted mid-archive). Freezing the donor first made
   create deterministic. Resolved by online state-store checkpoints.
2. **Restore must not stage the archive in container ephemeral storage.**
   The default `os.CreateTemp("")` staged the 81GiB download in the
   container's ephemeral layer and the kubelet evicted the pod
   (ephemeral-storage pressure). Workaround: `TMPDIR` on the data volume.
   Follow-up: stream-extract directly from S3 (as homepack does) or default
   the staging path to the node home.

## Restore scales with provisioned disk throughput (2026-07-22, round 2)

Hardware baseline for both rounds (recorded for reproducibility):

- Node: `m6a.4xlarge` (16 vCPU, 64GiB RAM), eu-central-1a; EBS bandwidth cap
  for this instance type ≈ 6.6Gbps (~830MiB/s).
- Round 1 volume: StorageClass `gp3` with no parameters = EBS defaults,
  3000 IOPS / 125MiB/s throughput, 300Gi.
- Round 2 volume: new StorageClass `gp3-10k-1000` = 10,000 IOPS / 1000MiB/s
  throughput, 300Gi. Same pod spec (8 CPU / 32Gi requests), same image, same
  81.4GiB archive (height 219130000), same region.

Result: identical restore command dropped from **32m35s to 12m02s (2.7x)**
purely from the volume change; light-client verify + bootstrap stayed ~10s.
Catch-up remained network/replay-bound as before. Effective volume traffic
went from ~130MiB/s (at the 125MiB/s cap) to ~350MiB/s sustained — the round-2
run no longer saturates the volume, so the residual bottleneck is the
sequential download-then-extract flow (staging write + read-back + extract
write ≈ 250GiB of volume traffic per restore) and s3manager download
concurrency, not the disk ceiling. Streaming extraction directly from S3
(homepack-style, no staging file) removes ~90GiB of that traffic and is the
next lever: projected under 8 minutes on this hardware.

Confirms the headline claim: the key-stream state sync baseline was CPU-bound
(55m-1h19m even on 750MiB/s volumes), while archive restore converts
provisioned IO directly into wall-clock speedup.

## Live-donor create via online state-store checkpoints (2026-07-26)

The quiesce requirement from the 07-22 run (finding 1 above) is resolved.
`state_store` now takes interval-based online checkpoints, mirroring the
FlatKV snapshot mechanism: every `ss-checkpoint-interval` versions the
composite state store drains its async apply queues and takes a hardlink
Pebble checkpoint of each backend into
`data/state_store/snapshots/snapshot-<version>`, keeping
`ss-checkpoint-keep-recent` of them. Checkpoint creation is hardlink-based and
does not block writes, so the donor keeps producing blocks.

`flatkv-archive create` now prefers the newest state-store checkpoint and
pairs it with the newest FlatKV snapshot at height <= the checkpoint version,
so every archived file is immutable; the live-directory path remains only as
a fallback for stopped donors.

End-to-end validation on the same `pacific-fork-1` cluster, donor fork-3
**live in consensus throughout** (`ss-checkpoint-interval = 10000`,
`keep-recent = 1`):

- **Create + S3 upload: 19m35s.** Paired state-store checkpoint
  `snapshot-...219953060` with FlatKV snapshot 219950000; archive labeled
  height 219950000, 23,425 files, 81.4GiB. No hash mismatches. fork-3
  advanced ~2,500 blocks during the create with no consensus interruption.
- **Restore + bootstrap: 12m47s** on the gp3-10k-1000 victim (fresh PVC),
  matching the round-2 quiesced-archive number (12m02s).
- **Catch-up: ~4m** to block-sync the ~6,000-block gap; victim reported
  `catching_up=false` at chain head.

This closes the last functional gap: archives can now be produced from any
running FlatKVOnly node without operator intervention beyond enabling the
checkpoint interval.

## SC-only archive benchmark (2026-07-27)

Measured the validator-archive shape: FlatKV checkpoint only, skipping
`state_store` and `wasm` (via `--state-store-dir`/`--wasm-dir` pointed at
nonexistent paths). Same live donor (fork-3, producing blocks throughout) and
the same gp3-10k-1000 victim:

- Archive: height 220090000, **37.6 GiB**, 10,113 files (vs 81.4 GiB /
  23,425 files full).
- **Create + upload: 4m08s** (vs 19m35s full).
- **Restore + bootstrap: 4m10s** (vs 12m47s full).
- Catch-up: ~2.5m to block-sync ~9,700 blocks; `catching_up=false` at head.

Both phases beat the linear byte-share estimate (39/89 of the full-archive
times would be ~8.5m create / ~5.6m restore): state_store and wasm carry most
of the archive's file count, so per-file overhead (SHA-256 stream setup, tar
headers, small-file IO) drops disproportionately. Node-bootstrap-to-consensus
in under 10 minutes total is achievable today for validator-shaped restores.
