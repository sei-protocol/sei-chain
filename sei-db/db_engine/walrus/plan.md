# Project WALRUS — proof of concept

## Context

`sei-db/db_engine/walrus/design.md` proposes answering historical state queries by
searching an immutable WAL backwards instead of maintaining a pebble MVCC store.
Blocks group into ≤4 GB **pods**; each sealed pod gets a bloom filter and an index;
a query for `(key, block)` walks pods backwards until it finds the key or reaches
a snapshot. The write path becomes a pure append — no compaction — and the cost
moves to the read path.

**This is a weekend proof of concept.** The question it answers is whether that
read cost is acceptable, not whether the code is shippable. No `StateStore`, no
`DBWrapper`, no integration with the snapshot `Manager` or the GC controller —
those come later, if the numbers justify them.

The number that decides it: **pods probed per query**, as a distribution.

---

## Design decisions

### Retention and the snapshot ladder

Retention is a window measured in blocks. Pods and snapshots below it may be
dropped — except the newest snapshot at or below the window's floor, which the
oldest retained pod needs as its terminator.

Snapshots form a ladder inside the window and land at **arbitrary heights**;
scheduling is outside WALRUS's control. WALRUS takes its own reference to them:

```go
// RetainSnapshot takes an independent reference to a state snapshot, which this
// instance then reads to terminate backwards walks. Every file under directory is
// hard-linked into WALRUS's own tree, so the caller keeps its directory and may
// delete it as soon as this returns.
RetainSnapshot(blockNumber uint64, directory string) error
```

Hard-linking is what makes this simple: the filesystem is the reference count, so
WALRUS's retention window and whatever pruner the producer runs never need to know
about each other, and there is no lease to negotiate in either direction. WALRUS's
links also make the ladder rediscoverable on restart by listing its own tree.

Two preconditions come with it. The source must be an **immutable** image — a
hard link shares the inode, so a producer rewriting a file in place would rewrite it
underneath a running query; a checkpoint qualifies, a live database directory does
not. And hard links cannot cross filesystems, the same constraint
`verifyHardlinks` (`sei-db/state_db/ss/snapshot/manager.go:700`) already enforces
for the existing snapshot root.

### The query is a half-open block range

Because snapshots are unaligned, a snapshot at height `S` lands mid-pod. So:

> find the newest version of `K` in `(S, B]`, where `S` is the newest snapshot
> height at or below `B`; if there is none, read `K` from the snapshot at `S`.

- The version search takes a **lower bound as well as an upper bound**. Only two
  pods in a walk are partial — the ones holding `B` and `S` — but bounding both
  ends uniformly avoids special-casing either.
- Skip any pod whose `lastBlock <= S` or `firstBlock > B`.
- Locating `S` is a binary search over the sorted ladder.

### Level 1 of the index is a fixed-width array

The sketch's entry point is a sorted array of 4-byte pointers, each dereferenced
to read the key it names. Binary searching that is ~25 **random** dereferences
into a multi-hundred-MB region, per pod probed.

Instead, level 1 is a sorted array of `(keyPrefix uint64, recordOffset uint32)` —
the first 8 bytes of the key, zero-padded, big-endian. Zero-padding preserves
lexicographic order, so it is still the key ordering the sketch calls for, but the
search touches one contiguous array and dereferences exactly once, at the end, to
confirm the full key. Prefix ties — common in real EVM, where every slot of a
contract shares `0x03 || addr[0:7]` — walk the tied run.

Level 2 is as sketched:
`[uint16 keyLen][key][uint32 versionCount][(uint32 blockDelta, uint32 offset) × versionCount]`,
`blockDelta` relative to the pod's first block, ascending.

*Size lever, deliberately not built:* at cryptosim-shaped keys the level-2 key blob
is roughly half the index. Front-coding the sorted keys with restart points every
16 keys drops level 1 to ~22 MB per pod — resident for every pod at once. Measure
first.

### Both searches are `sort.Search` over an mmap

Level 1 and level 2 are both "rightmost element ≤ target". The index file is
sealed and immutable, so mmap it — then the probe cannot fail and stdlib
`sort.Search` covers both. This is a deliberate departure from the sketch's
"abstract binary search algorithm": a custom generic helper is only earned if the
probe can return an error, which mmap removes.

### Scope: point queries only

Range iteration at a height cannot use the bloom filters — every pod back to the
floor holds keys in the range, so it degenerates into a k-way merge across the
whole walk. Out of scope. Level 1 still keeps lexicographic order, so nothing here
forecloses it later.

### Sizing

At cryptosim-shaped load (~4.5k unique keys/block, mean key 34 B, value 32 B):
~69 B/entry, ~310 KB/block, so **~13,800 blocks and ~62M entries per 4 GB pod**,
~30M unique keys, and a bloom around 31 MB at 1% FPR.

---

## What gets built

### `sei-db/db_engine/walrus`

| File | Contents |
|---|---|
| `walrus.go` | `Walrus` interface |
| `read_status.go` | `ReadStatus` |
| `walrus_config.go` | `Config`, `DefaultConfig()`, `Validate()` |
| `walrus_impl.go` | pod lifecycle, query walk, retention |
| `pod.go` | `PodInfo`, `Pod`, `Block`, pod file naming |
| `pod_file.go` | `PodReader` |
| `pod_index.go` | `PodIndex` |
| `pod_bloom.go` | `PodBloom` |
| `snapshot.go` | `Snapshot` |
| `pod_accumulator.go` | `PodAccumulator` — gathers blocks and decides the cut |
| `pod_builder.go` | `PodBuilder` — writes all three of a pod's files |
| `catalog.go` | `Catalog`, `Query` — what exists on disk, and when it may be deleted |
| `walrus_metrics.go` | OTel instruments |
| `statestub/` | flat pebble store that produces the snapshots |
| `harness/` | query harness: load generation, reader goroutines, Prometheus export |

**Query contract.** The out-of-range cases are return values, not sentinel errors:

```go
// ReadStatus reports how a historical read resolved.
type ReadStatus uint8

const (
    // ReadFound means the key held a value at the requested block.
    ReadFound ReadStatus = 0
    // ReadAbsent means the key held no value at the requested block.
    ReadAbsent ReadStatus = 1
    // ReadTooNew means the block falls in the pod still under construction.
    ReadTooNew ReadStatus = 2
    // ReadTooOld means the block fell out of the retention window.
    ReadTooOld ReadStatus = 3
)

// Get returns the value the key held at the end of the given block.
Get(key []byte, blockNumber uint64) (value []byte, status ReadStatus, err error)
```

What the walk cost — pods probed, pods searched, whether the floor was read — stays between the
implementation and its instruments. It is labeled there by `ReadStatus`, which is what separates a
never-written key (always `ReadAbsent`) from a live one.

**Pod data file.**

```
header : magic "WALRS" | format version byte | podSeq u64 | firstBlock u64
record : uvarint keyLen | key | flags byte (bit0 = tombstone) | uvarint valLen | value | u32 CRC32
```

Blocks never span pods. A block larger than a whole pod is an unrecoverable
configuration error.

Tombstones come straight from `proto.KVPair.Delete`
(`sei-db/proto/changeset.proto:6`); one found during the walk ends it with
`ReadAbsent`.

**The write path is accumulate, then build.** A block handed to the top-level object goes
straight into a pod accumulator, which stores it and tracks the encoded size. When a block
would push the pod past its limit, the blocks before it become a complete pod and that
block starts the next one. The engine then hands the completed pod to the builder.

```go
// PodAccumulator gathers blocks until they fill a pod.
type PodAccumulator interface {
    // Add stores a block. When the block does not fit in the pod being accumulated, that
    // pod is returned complete and the block becomes the first of the next one.
    //
    // pod is nil unless this call completed one.
    Add(block Block) (pod []Block)

    // Drain returns the blocks gathered so far and resets, or nil if there are none. This
    // is how a pod short of the limit gets written at shutdown.
    Drain() []Block
}
```

Putting the byte accounting here is what keeps it out of both neighbours: the engine never
learns the pod framing, and the builder never decides a cut.

**Interfaces carry the contract, not the format.** An interface file says what the type
answers and what the caller may rely on. Byte layouts, magic values, format version
constants, and field widths belong beside the code that encodes and decodes them, in the
implementation file. Putting them on the interface makes every caller read a file format
in order to learn a method signature.

**Each file on disk gets one object that owns it.** A `PodIndex`, `PodBloom`, or
`PodReader` is the handle for its file: it knows the file's path and size, answers queries
against it, and is what closes and deletes it. Retention goes through those objects rather
than recomputing paths from `PodInfo` and a directory, so exactly one place knows what a
pod's files are called.

Every handle stays resident for the life of its pod, and holds nothing but metadata — its
path, its size, the blocks it covers. A petabyte of pods is roughly 260k pods, so residency
is only affordable at that price: the bloom filters alone would run to terabytes, and three
open descriptors per pod would be 780k of them, far past any process limit.

Because a handle pins nothing, it has no `Close()`. It has a `Delete()`, which is the
operational verb garbage collection actually wants. Whether reads open the file per call or
an implementation keeps its own small descriptor cache is an implementation matter that the
contract does not need to expose.

**A snapshot is the same kind of handle.** It is a thin wrapper over a pebble checkpoint —
block number, path, size, a `Get`, and a `Delete` — matching the pod handles exactly. When
the underlying database is opened is an implementation concern the contract does not
expose, so `Snapshot` has no `Close()`.

### The catalog

One type owns the lifecycle of everything on disk, replacing both `PodSet` and
`SnapshotLadder`. Note that the four artifacts are really two lifecycles: a pod's data
file, index, and bloom filter are created together by one `Build()` and die together, which
is what `Pod.Delete()` already is. So the catalog tracks pods and snapshots.

**Deletion is gated by an admission floor and a reference count.** These are different
jobs, and separating them is what keeps either one simple:

- `queryFloor` — the oldest block that may be queried. Policy sets it; it only moves
  forward. A query below it is refused. This is what lets collection make progress: raise
  the floor, no new query can reference anything under it, the queries already running
  drain, and collection proceeds.
- a reference count on every pod and every snapshot, taken for each object a query
  resolves and released when the query finishes. Collection may delete an object only when
  its count is zero.

The count is what makes safety local. Collection asks each object two independent
questions — does policy want it gone, and is anyone reading it — instead of deriving a
second watermark from the lowest block any in-flight query might touch. A query pins
precisely the objects it resolved, which is also precisely what it will read.

**Retention is advancing the floor snapshot.** The catalog holds one distinguished
snapshot, the floor, and the invariant that gives every query an answer is:

> the floor snapshot exists, and pods cover every block from just above it through head.

A pseudo-snapshot at block 0 that holds no data and answers every read "not found" is the
floor of a fresh instance, which makes the invariant true from the first block and removes
every nil case downstream.

Collection is then one operation rather than two sweeps. Policy names a target block from
the retention window; the catalog picks the newest **real** snapshot at or below it, makes
that the new floor, and deletes everything strictly below: every pod whose last block is at
or below the new floor, and every snapshot older than it, the outgoing pseudo-snapshot
included. If there is no real snapshot at or below the target, nothing moves.

That last clause is the whole safety argument, and the pseudo-snapshot must not be read as
satisfying it. Deleting pods while the floor is still the pseudo-snapshot would leave a
query walking back through a hole and terminating on a snapshot that reports "not found"
for everything — answering `ReadAbsent` for a key that plainly had a value. A gap between
the floor and the oldest pod is silent data corruption, not a refusal, which is why
deletion is expressed as moving the floor rather than as two independent sweeps that could
disagree.

**A query handle is the only way to reach a pod or a snapshot.** That is what makes the
guard an invariant rather than a convention: no caller can read a file without first having
held deletion back for it.

```go
// Catalog records every file on disk and is the authority on when one may be deleted.
type Catalog interface {
    // AddPod registers a newly written pod, making it queryable.
    AddPod(pod *Pod)

    // AddSnapshot registers a newly retained snapshot.
    AddSnapshot(snapshot Snapshot)

    // Query admits a read at blockNumber and holds back deletion of every file it may
    // touch until the returned handle is released. admitted is false when blockNumber is
    // below the query floor.
    Query(blockNumber uint64) (query *Query, admitted bool)

    // SetQueryFloor raises the oldest block that may be queried. It never lowers it.
    SetQueryFloor(blockNumber uint64)

    // Collect deletes every file not needed to serve a query at the deletion floor,
    // reporting how many files went and how many bytes that reclaimed.
    Collect() (files int, bytes int64, err error)

    // Bounds reports the range of blocks the catalog can answer for.
    Bounds() (ok bool, first uint64, last uint64)
}

// Query is an admitted read at one block, and the only route to the files it may touch.
type Query struct{ /* ... */ }

// Floor returns the newest snapshot at or below the queried block, where a walk that finds
// no version of a key terminates. There is always one: a fresh instance starts with a
// pseudo-snapshot at block 0 that answers every read "not found".
func (q *Query) Floor() Snapshot

// Pods returns the pods holding blocks in (floor, queried block], newest first — exactly
// the pods a backwards walk visits, in the order it visits them.
func (q *Query) Pods() []*Pod

// Release ends the read, dropping the references it holds.
func (q *Query) Release()
```

`Query()` resolves the floor and the span of pods once and takes a reference to each, under
one lock, so what a query sees can never be deleted underneath it. Resolving the whole span
up front is also what keeps per-item lookups out of the catalog: the caller servicing the
walk already holds everything it needs and asks the catalog nothing further.

The cost is one reference per pod in the span rather than a single registration, which is
irrelevant beside the bloom probe each of those pods is about to get.

**File layout for encoding and decoding is deferred.** Formats come out of the interface
files now because they are not contract; where they land is a decision for when the
implementation is written, not before.

**One config for the whole package.** `walrus.Config` is flat and every component takes it
whole — accumulator, builder, index, bloom. No per-component config structs and no
threading of individual knobs through constructors. If it ever grows to the point of being
unreadable, that is when to split it, not before.

**A pod is built in one shot, from memory.** The builder receives a pod's worth of blocks
already in hand and writes all three of the pod's files:

```go
// Block is one block's changes, as the builder receives them.
type Block struct {
    Number     uint64
    ChangeSets []*proto.NamedChangeSet
}

// PodBuilder writes pods.
type PodBuilder interface {
    // Build writes the pod holding blocks: its WAL file, its bloom filter, and its index.
    // The returned Pod queries those files from disk; nothing from blocks stays pinned in
    // memory once Build returns.
    Build(blocks []Block) (*Pod, error)
}

// Pod is a written pod, open for querying.
type Pod struct {
    Info  *PodInfo
    Data  PodReader
    Index PodIndex
    Bloom PodBloom
}
```

Holding a whole pod in memory is affordable — 4 GB against a ~256 GB host — and buying
that deletes a great deal of machinery. There is no incremental append path and no
rotation decision inside the writer, because the accumulator already made the cut. There
is no scanner and no post-pass, because the data never left memory to need re-reading. And
the bloom filter's key count is known from the sorted keys before any file is opened, so
nothing has to happen in a particular order to discover it.

Lifecycle stays with the engine: driving the accumulator, recovering after a crash,
retention, and the concurrency of parallel builds.

**`PodInfo` is identity only.** It carried `DataSize`, `EntryCount`, and `KeyCount`, which
were zero when the struct came from `ParseSealedPodName()` and populated when it came
back from a build — one type meaning two things, with no way to tell which you held. The
counts do not need to travel: `KeyCount` exists only to size the bloom filter and never
leaves the build, and the other two are observational and are recorded to instruments
where they are measured. What survives is sequence plus block range, which is exactly
what a directory listing yields.

**Index build is a post-pass over the sealed pod**, not streaming — the sketch
says construction may be freely parallelized, and a post-pass honours that while
keeping the writer a pure append with no multi-GB accumulator held per in-flight
pod. Sequential re-read of a just-written pod comes out of page cache.

**`statestub`** is the stand-in for FlatKV: apply each block's changeset to a flat
(latest-value-only) store, and on an interval checkpoint, hand the directory to
`RetainSnapshot`, and delete its own copy. It
wraps `sei-db/db_engine/pebbledb`, which already gives a `KeyValueDB` with `Batch`
and implements `Checkpointable` (`db.go:186`, assertion at `:193`) — so reading a
key out of a retained snapshot is a plain `Get`, no MVCC decoding.

### What is deliberately skipped

Crash recovery beyond truncating a torn tail on reopen, and directory locking.
Metrics are **not** skipped — they are the deliverable.

### Not reused

- **`seiwal`.** No random access (`seiwal_iterator.go:237` reads whole files and
  linear-scans; the TODO at `:291` for a missing offset index *is* the pod index),
  rotation is post-append rather than predictive, and `KVPair`s inside a marshaled
  `ChangeSet` have no recoverable byte offset
  (`sei-db/state_db/statewal/state_wal_serialization.go:35`). WALRUS writes its own
  pod files, borrowing the conventions: magic + format version byte, CRC32 per
  record, `{seq}-{first}-{last}` sealed names.
- **Bloom filters.** Nothing reusable in the tree — pebble's is internal,
  `evmrpc/ethbloom` is a fixed 2048-bit Ethereum bloom. Write one; hashing uses
  `cespare/xxhash/v2` with Kirsch–Mitzenmacher double hashing, already a direct
  dependency, so nothing new is pulled in.

---

## Measuring viability — the query harness

A standalone harness, not a cryptosim backend. It borrows cryptosim's shape —
block-at-a-time changeset generation, executor goroutines, independent
rate-limited reader goroutines, a canned-random key generator — without depending
on cryptosim or on `DBWrapper`.

It generates EVM-shaped load — 21-byte account keys, 53-byte storage-slot keys,
32-byte values, a hot/cold access mix — feeds each block to `statestub` and WALRUS
together, then issues historical point queries from reader goroutines.

**Metrics are the deliverable, so they are built the way the repo builds them:**
OTel instruments exported to Prometheus, following `sei-db/seiwal/seiwal_metrics.go`
and `sei-db/state_db/ss/snapshot/metrics.go` — a package-level
`var meter = otel.Meter("walrus")`, instruments built through a `must` helper,
`Record*` free functions. The harness stands up the Prometheus exporter and
`/metrics` endpoint the way `cryptosim/cmd/cryptosim/main.go:24` does, and the
existing Grafana setup (`docker/monitornode/scripts/start-{prometheus,grafana}.sh`)
scrapes it.

Instruments, `walrus_` prefixed:

- `pods_probed` histogram — **the headline number**, a distribution not a mean
- `query_duration_seconds` histogram, labeled by resolution
  (found / absent / snapshot-hit)
- `bloom_probes_total` counter, labeled true-hit / false-positive
- `queries_total` counter, labeled by `ReadStatus`
- `snapshot_reads_total` counter — how often the walk reaches the floor
- `pod_bytes` / `index_bytes` / `bloom_bytes` gauges — the space cost of the
  schema, and the ratio that decides whether 4 GB pods are the right size
- `index_build_duration_seconds` histogram — the write-side cost of the post-pass

Plus the harness's own write-side counters (blocks, keys, bytes appended) so
append throughput is visible against the 1M updates/sec target.

Swept: snapshot ladder interval, bloom FPR, retention window, pod size.

### Query sampling

Block heights are drawn uniformly at random across the whole supported range —
oldest retained pod to newest sealed pod — not weighted toward the tip, which
would report near-zero pods probed and prove nothing.

Keys split two ways, by a configured fraction:

- **Never-written keys.** Drawn from an id range the load generator never
  allocates, so no pod anywhere holds them. This is the maximum-depth query: it
  probes every pod back to the floor, misses the snapshot too, and returns
  `ReadAbsent`. It is simultaneously the worst case for walk length and the clean
  measurement of bloom false-positive cost, since every pod it opens is a false
  positive by construction.
- **Live keys.** Ids the generator does write, each with nonzero probability per
  block, so their walk depth is set by how recently they were last touched.

Reported separately — averaging them together would hide both.

Patterns worth lifting rather than reinventing: `CannedRandom.Address`
(`sei-db/common/rand/canned_random.go:170`) reproduces any account/contract/slot
key deterministically from `(seed, id)`, so a reader needs no stored key set —
it can synthesize a valid key for any id on the fly. The reader-goroutine shape is
`tickerLoop` (`cryptosim/reciept_store_simulator.go:304`) with a per-reader
`crand.Clone(true)`, since `CannedRandom` is not thread-safe
(`canned_random.go:72`). Key shapes come from `cryptosim/data_generator.go:169`,
`:214`, `:270`.

## Verification

1. `scripts/ramtest.sh ./sei-db/db_engine/walrus/...` — pod framing round-trip,
   predictive rotation at the 4 GB boundary, index search against a brute-force
   oracle, bloom FPR within its configured bound, a walk that terminates in a
   snapshot mid-pod, tombstone handling, retention dropping pods but keeping the
   floor snapshot.
2. A differential test: same changeset stream into WALRUS and into an in-memory
   `map[string][]version` oracle, then assert `Get(key, block)` agrees over a large
   random sample including tombstoned, never-written, and out-of-retention keys.
3. `make dblint` from within `sei-db/`.

## Verdict criteria

The schema is viable if, at a snapshot ladder interval whose disk cost is
acceptable, the never-written query — the worst case — probes few enough pods to
stay inside a sane latency budget. If it does not, the finding is what the fix has
to be: denser snapshots, hierarchical blooms over pod groups, or per-key
back-pointer chains. Either outcome is a result.
