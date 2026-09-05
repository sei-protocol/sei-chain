# WALRUS implementation notes

What is on disk, how the pieces fit, and the decisions behind them. See `design.md` for the
original sketch and `plan.md` for the design rationale.

None of this is contract. It is deliberately kept out of the interface files: an interface says
what a type answers, and a reader should not have to parse a file format to learn a method
signature.

---

## On-disk formats

Every format begins with a magic value and a one-byte format version, validated on read, so a
file written by an incompatible build is refused rather than misparsed. All multi-byte integers
are big endian.

### Pod data file — `{firstBlock}-{lastBlock}.pod`

```
header (25 bytes):
  magic       8 bytes  "WALRSPOD"
  version     1 byte
  firstBlock  8 bytes
  lastBlock   8 bytes

data section, a sequence of block records:
  uvarint blockDelta      block number, relative to firstBlock
  uvarint entryCount
  entry * entryCount
  uint32  CRC32-IEEE      over every preceding byte of this block record

entry:
  uvarint keyLength
  key
  byte    flags           bit 0 set means the entry is a deletion
  uvarint valueLength
  value
```

An entry's offset, as the index records it, is the byte position of its `keyLength` prefix
measured **from the start of the data section**, not from the start of the file. That is what
lets the header grow later without shrinking the addressable range, and it is why the 4 GiB cap
applies to the data section alone.

The checksum covers a whole block record rather than each entry, because a block is the atomic
unit of the write.

### Pod index — `{firstBlock}-{lastBlock}.pod.idx`

```
header (41 bytes):
  magic        8 bytes  "WALRSIDX"
  version      1 byte
  firstBlock   8 bytes
  lastBlock    8 bytes
  keyCount     8 bytes
  level2Start  8 bytes  byte offset of level two from the start of the file
```

Two levels follow. Level one is a fixed-width array, sorted ascending by key:

```
uint64 keyPrefix      the key's first 8 bytes, zero padded on the right
uint32 recordOffset   byte offset of the key's level two record, from the start of level two
```

Level two holds one record per distinct key, in the same order:

```
uint16 keyLength
key
uint32 versionCount
versionCount * (uint32 blockDelta, uint32 entryOffset), ascending by blockDelta
```

Zero padding preserves lexicographic order, so ordering level one by prefix and breaking ties on
the full key is the same ordering as the keys themselves.

The fixed width is the point. A binary search reads one contiguous array and dereferences into
level two exactly once — at the end, to confirm the full key — instead of dereferencing on every
probe. The sketch's original design was an array of 4-byte pointers, which costs roughly 25
random reads into a multi-hundred-megabyte region per pod probed.

Ties on the 8-byte prefix are walked linearly. They are common in real EVM data, where every
storage slot of one contract shares `0x03 || addr[0:7]`.

`blockDelta` is relative to the pod's first block and `entryOffset` is relative to the data
section, so both fit a `uint32` for any pod within the size cap.

**Size lever, deliberately not built yet.** At EVM-shaped key sizes the level-two key blob is
roughly half the index. Front-coding the sorted keys with restart points every 16 keys would
drop level one to about 22 MB per 4 GB pod — small enough to hold resident for every pod at
once — at the cost of a short linear walk after the search. Measure before building it.

### Pod bloom filter — `{firstBlock}-{lastBlock}.pod.bloom`

```
header (18 bytes):
  magic      8 bytes  "WALRSBLM"
  version    1 byte
  bitCount   8 bytes
  hashCount  1 byte
bits         ceil(bitCount / 8) bytes
```

Sizing is the standard optimum: `m = -n·ln(p) / (ln2)²`, `k = round((m/n)·ln2)`, with `k` clamped
to `[1, 255]` since it is stored in one byte. A filter for zero keys still gets one bit and one
hash, so probing it is the same code path as probing any other.

Bit positions come from one 64-bit hash expanded by the Kirsch-Mitzenmacher construction
(`h_i = h1 + i·h2`), so a probe hashes once regardless of `hashCount`. Hashing uses
`cespare/xxhash/v2`, already a direct dependency — no new dependency is pulled in. Nothing in the
repo provides a reusable tunable-FPR bloom: pebble's is internal, and `evmrpc/ethbloom` is a
fixed 2048-bit Ethereum bloom.

### Writing

All three of a pod's files are written under a `.partial` extension and renamed into place
together, so an interrupted build leaves no pod a later open could mistake for a complete one.
Recovery is deleting leftovers, not rebuilding them.

---

## Reading

Index and bloom lookups are pure functions of an immutable file, so the probe cannot fail once
the file is open. That is why `PodBloom.MayContain` returns no error, and why both searches use
stdlib `sort.Search` rather than a custom fallible binary search — the sketch called for an
abstract binary search helper, but a helper is only earned when the probe can return an error.

No handle holds its file's contents. That has to be true at scale: a petabyte is roughly 260k
pods, the bloom filters alone run to terabytes, and three open descriptors per pod would be 780k
of them.

The index and data handles hold path, size, and block range, and open the file for the duration of
an operation. A single index search opens the file once, binary searches level one by reading
twelve byte slots, and dereferences into level two once to confirm the full key. Both are reached
only after a bloom filter has failed to rule the pod out, so they are rare relative to the probes
in front of them.

The bloom filter is **memory mapped** rather than opened per probe or read into memory. A probe is
the hot path — a walk tests every pod it passes — so it cannot afford an open, but the filters are
far too large to hold. Mapping puts the choice where it belongs: the pages a probe touches are
paged in on demand and the operating system evicts them under pressure. It also keeps `MayContain`
free of an error return, since a mapped immutable file has nothing left that can fail, and costs no
descriptor, because the mapping outlives the descriptor it was created from.

`Delete` unmaps. That is why the catalog refuses to delete a pod a query still references:
unlinking a file underneath a reader is safe, but unmapping memory it is still reading is not.

---

## The write path

`Walrus.AppendBlock` → `PodAccumulator.Add` → when a pod comes back, `PodBuilder.Build` →
`Catalog.AddPod`.

The accumulator is the only component that knows what a block costs on disk. It tracks encoded
size as blocks arrive; when the next block would exceed the target, the blocks before it become
a pod and that block starts the next one. The engine above never learns the framing, and the
builder below never decides a cut.

The builder works entirely in memory — a 4 GB pod against a ~256 GB host. That is what removes
the incremental append path, the rotation decision inside the writer, and any pass back over a
sealed file. It also means the distinct key count is known from the sorted keys before any file
is opened, so the bloom filter can be sized without the index having been written first.

Build order inside `Build`: collect `(key, blockNumber, offset)` for every entry while writing
the data file, sort by `(keyPrefix, key, blockNumber)`, then write the index and the bloom from
the sorted run.

A block that writes the same key more than once resolves to its last write.

Changesets carrying a store name other than `Config.StoreName` are rejected rather than dropped,
so a miswired caller fails loudly instead of losing data.

---

## The query path

`Catalog.Query(block)` resolves the floor snapshot and the span of pods under one lock and takes
a reference to each. The walk then visits pods newest-first, testing each bloom filter and
searching the index only where the filter does not rule the key out. The first version found
wins; a deletion entry ends the walk with `ReadAbsent`. Running out of pods means reading the
floor snapshot.

The index search takes a **half-open block range** `(lowBlock, highBlock]`. The lower bound
matters because snapshots land at arbitrary heights, not on pod boundaries: a snapshot at block
S sits mid-pod, and versions at or below S are answered by the snapshot, not by the pod that
also happens to hold them. Only two pods in a walk are partial — the ones holding the queried
block and the floor — but bounding both ends uniformly avoids special-casing either.

Cost accounting (pods probed, pods searched, whether the floor was read) stays between the
implementation and its instruments. It is labeled by `ReadStatus`, which separates a
never-written key — always `ReadAbsent` — from a live one.

---

## The catalog

**Two independent gates on deletion**, asked per object:

- the **query floor**, which policy raises and which only moves forward. It refuses queries
  below it, which is what lets collection make progress: raise the floor, no new query can
  reference anything under it, running queries drain, collection proceeds.
- a **reference count** per pod and per snapshot, taken when a query is admitted and dropped
  when it is released. Collection may delete an object only at zero.

An earlier design used a second, lagging watermark instead of reference counts. The count is
better because safety becomes local: collection asks each object whether anyone is reading it,
rather than deriving a bound from the lowest block any in-flight query might touch. A query pins
precisely what it resolved, which is precisely what it will read. The cost is one reference per
pod in the span rather than one registration per query, which is irrelevant beside the bloom
probe each of those pods is about to receive.

The in-flight count is per object, so a long-running query at an old block holds back only the
objects it named.

**Retention is advancing the floor snapshot**, not two sweeps. The invariant is:

> the floor snapshot exists, and pods cover every block from just above it through head.

Collection picks the newest snapshot backed by real data at or below the requested query floor,
makes it the floor, and deletes everything strictly below — pods whose last block is at or below
it, and every older snapshot. If no such snapshot exists, nothing moves. A pod straddling the
floor is kept whole.

**The block 0 pseudo-snapshot** holds no data and reports every key absent. It makes the
invariant true for a fresh instance and removes every "is there a floor" branch from the query
path, so `Query.Floor()` returns a `Snapshot` with no `found` flag.

It must never be treated as licensing pod deletion. Deleting pods while the floor is still the
pseudo-snapshot leaves a walk falling through the gap and terminating on a snapshot that reports
everything absent, returning `ReadAbsent` for a key that plainly had a value. That is a wrong
answer indistinguishable from a right one, not a failure — which is the whole reason retention
is expressed as moving the floor rather than as two sweeps that could disagree.

---

## Snapshots

`Walrus.RetainSnapshot(block, dir)` hard-links every file under `dir` into this instance's tree
and registers the handle with the catalog.

Hard links, rather than moving the directory, are what decouple the lifecycles: the filesystem
becomes the reference count, the producer keeps its own copy and may delete it immediately, and
neither side needs a lease from the other.

Two preconditions follow, and both are on `RetainSnapshot`. The source must be **immutable** — a
hard link shares the inode, so a producer rewriting a file in place would rewrite it underneath a
running query; a checkpoint qualifies, a live database directory does not. And hard links cannot
cross filesystems, the same constraint `verifyHardlinks`
(`sei-db/state_db/ss/snapshot/manager.go:700`) already enforces for the existing snapshot root.
Probe it at open the way that does, rather than discovering it on the first retain.

A consequence worth remembering when reading the space gauges: disk is not reclaimed when the
producer deletes its copy, only when the last link goes. The gauges measure what this instance is
holding alive, not what it wrote.

Snapshot directories are named `snapshot-` followed by the block number zero-padded to 20 digits,
mirroring `sei-db/state_db/ss/snapshot/manager.go`.

---

## Recovery

Opening deletes rather than repairs, because a pod is written whole or not at all:

- any file carrying the `.partial` extension is the wreckage of an interrupted build and is
  deleted
- pods are sorted by first block and kept only while they form an unbroken run with all three of
  their files present; everything at and above the first break is deleted
- a snapshot directory left mid-retain is deleted, since its hard links were never published

Truncating rather than keeping a pod above a gap is what stops a walk falling through the hole and
answering from the floor. The blocks above the gap are gone and the caller resumes appending from
there.

## Not reused, and why

**`seiwal`** looks like the right shape but is not. It has no random access — `seiwal_iterator.go:237`
reads whole files and linear-scans, and the TODO at `:291` for a missing offset index *is* the pod
index at a different granularity. Its rotation is post-append rather than predictive. And
individual `KVPair`s inside a marshaled `ChangeSet` have no recoverable byte offset
(`sei-db/state_db/statewal/state_wal_serialization.go:35`). WALRUS writes its own pod files,
borrowing the conventions: magic plus format version, CRC32, block range encoded in the file name.

**`statestub`** wraps `sei-db/db_engine/pebbledb`, which already provides a `KeyValueDB` with
`Batch` and implements `Checkpointable` (`db.go:186`, assertion at `:193`). Reading a key out of
a retained snapshot is then a plain `Get` with no MVCC decoding, because the snapshot is a flat
image rather than a version history.

---

## Sizing, for reference

At EVM-shaped load — roughly 4.5k unique keys per block, mean key 34 B, value 32 B — an entry
costs about 69 B, a block about 310 KB. A 4 GB pod therefore holds roughly 13,800 blocks and 62M
entries across about 30M distinct keys, and its bloom filter runs about 31 MB at a 1% false
positive rate.
