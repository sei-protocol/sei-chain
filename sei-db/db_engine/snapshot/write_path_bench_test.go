package snapshot

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// A block's write set in the cryptosim benchmark is roughly 4,000 pairs: ~1,930 account rows keyed
// by "evm/" + prefix byte + 20-byte address with an 81-byte value, and ~2,030 storage rows keyed by
// the same prefix plus address||slot with a 41-byte value. These constants reproduce that shape.
const (
	benchPairsPerBlock = 4000
	benchAccountKeyLen = 25
	benchStorageKeyLen = 57
	benchValueLen      = 81
)

// benchWriteSet builds one block's worth of KV pairs, distinct from every other block's.
func benchWriteSet(block int) []*proto.KVPair {
	pairs := make([]*proto.KVPair, 0, benchPairsPerBlock)
	for i := 0; i < benchPairsPerBlock; i++ {
		keyLen := benchAccountKeyLen
		if i%2 == 1 {
			keyLen = benchStorageKeyLen
		}
		key := make([]byte, keyLen)
		copy(key, "evm/")
		binary.BigEndian.PutUint64(key[4:], uint64(block))
		binary.BigEndian.PutUint64(key[12:], uint64(i))
		pairs = append(pairs, &proto.KVPair{Key: key, Value: make([]byte, benchValueLen)})
	}
	return pairs
}

func benchEngine(b *testing.B, shardCount uint64) (SnapshotEngine, func()) {
	b.Helper()
	config := newTestConfig(shardCount, 1<<30)
	config.EstimatedOverheadPerEntry = 256
	db := newTestDB(nil)
	pool := threading.NewElasticPool("bench-misc", 8)
	engine, err := NewSnapshotEngine(config, db, pool, pool)
	if err != nil {
		b.Fatal(err)
	}
	return engine, func() {
		_ = engine.Close()
		pool.Close()
		_ = db.Close()
	}
}

// BenchmarkEngineBatchSet measures the whole engine-level write: the serial per-key bucketing loop
// plus the fan-out into the shards. Compare against BenchmarkShardManagerShard (the bucketing loop's
// hashing alone) and BenchmarkShardBatchSet (one shard's share of the work with no bucketing or
// fan-out) to see which half dominates.
//
// The window parameter is how many committed-but-unretired versions precede the measured write,
// since that is what decides how many keys already have a deque in versionedData.
func BenchmarkEngineBatchSet(b *testing.B) {
	for _, window := range []int{0, 8, 32} {
		b.Run(fmt.Sprintf("window=%d", window), func(b *testing.B) {
			engine, cleanup := benchEngine(b, 8)
			defer cleanup()

			for block := 0; block < window; block++ {
				if err := engine.BatchSet(benchWriteSet(block)); err != nil {
					b.Fatal(err)
				}
				if _, err := engine.Commit(); err != nil {
					b.Fatal(err)
				}
			}

			// One write set per iteration, all distinct, so every iteration inserts keys that are
			// new to the window rather than re-writing the previous iteration's.
			sets := make([][]*proto.KVPair, b.N)
			for i := range sets {
				sets[i] = benchWriteSet(window + i)
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := engine.BatchSet(sets[i]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// benchStringWriteSet is benchWriteSet in the form the flatkv write path now hands over: keys as the
// strings they already are, and pairs in one backing array rather than one allocation each.
func benchStringWriteSet(block int) []StringKVPair {
	pairs := make([]StringKVPair, 0, benchPairsPerBlock)
	for _, pair := range benchWriteSet(block) {
		pairs = append(pairs, StringKVPair{Key: string(pair.Key), Value: pair.Value})
	}
	return pairs
}

// BenchmarkEngineBatchSetString is BenchmarkEngineBatchSet over the same write set through the
// string-keyed path, so the two are directly comparable.
func BenchmarkEngineBatchSetString(b *testing.B) {
	for _, window := range []int{0, 8, 32} {
		b.Run(fmt.Sprintf("window=%d", window), func(b *testing.B) {
			engine, cleanup := benchEngine(b, 8)
			defer cleanup()

			for block := 0; block < window; block++ {
				if err := engine.BatchSetString(benchStringWriteSet(block)); err != nil {
					b.Fatal(err)
				}
				if _, err := engine.Commit(); err != nil {
					b.Fatal(err)
				}
			}

			sets := make([][]StringKVPair, b.N)
			for i := range sets {
				sets[i] = benchStringWriteSet(window + i)
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := engine.BatchSetString(sets[i]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkShardManagerShard measures just the per-key hashing in BatchSet's serial bucketing loop.
// Every Set, Get, BatchSet and BatchGet key pays this.
func BenchmarkShardManagerShard(b *testing.B) {
	manager, err := newShardManager(8)
	if err != nil {
		b.Fatal(err)
	}
	pairs := benchWriteSet(0)

	b.ReportAllocs()
	for b.Loop() {
		for i := range pairs {
			_ = manager.Shard(pairs[i].Key)
		}
	}
}

// BenchmarkShardBatchSet measures a single shard's BatchSet directly: no bucketing, no fan-out, no
// goroutine handoff. Its per-pair cost is the deque and map work in setLocked.
func BenchmarkShardBatchSet(b *testing.B) {
	db := newTestDB(nil)
	defer func() { _ = db.Close() }()
	pool := threading.NewElasticPool("bench-shard", 4)
	defer pool.Close()

	config := DefaultTestSnapshotEngineConfig()
	config.EstimatedOverheadPerEntry = 256
	s, err := NewShard(context.Background(), config, db, pool, 1<<30,
		func() error { return ErrEngineClosed },
		func(error) {})
	if err != nil {
		b.Fatal(err)
	}

	sets := make([][]*proto.KVPair, b.N)
	for i := range sets {
		sets[i] = benchWriteSet(i)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := s.BatchSet(sets[i]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkShardBatchSetRewrite writes the same keys every iteration, so every key already has a
// deque in versionedData. Subtracting this from BenchmarkShardBatchSet isolates the cost of
// creating a deque per key that is new to the un-retired window.
func BenchmarkShardBatchSetRewrite(b *testing.B) {
	db := newTestDB(nil)
	defer func() { _ = db.Close() }()
	pool := threading.NewElasticPool("bench-shard-rewrite", 4)
	defer pool.Close()

	config := DefaultTestSnapshotEngineConfig()
	config.EstimatedOverheadPerEntry = 256
	s, err := NewShard(context.Background(), config, db, pool, 1<<30,
		func() error { return ErrEngineClosed },
		func(error) {})
	if err != nil {
		b.Fatal(err)
	}

	pairs := benchWriteSet(0)
	// Seed the deques so the measured writes all take the already-present path.
	if err := s.BatchSet(pairs); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := s.BatchSet(pairs); err != nil {
			b.Fatal(err)
		}
	}
}

// benchRepeatFraction is the share of a cryptosim block's writes that name a key already written in
// an un-retired version, and so take versionHistory's in-place-update path rather than its
// first-write path.
//
// It comes out of the harness config (cryptosim_config.go): 100 hot accounts drawn at
// HotAccountProbability 0.1, and 100 hot ERC20 contracts of 10 slots each drawn at
// HotErc20ContractProbability 0.5, against ~3,960 writes per block. Cold accounts come from a
// million-key pool and essentially never recur inside an 8-32 version window.
const benchRepeatFraction = 0.07

// BenchmarkShardBatchSetMixed writes a realistic blend of keys new to the version window and keys
// already in it. BenchmarkShardBatchSet and BenchmarkShardBatchSetRewrite bound this from either
// side, but neither is the workload: holding versionHistory by value in versionedData made the
// repeat path cost a map store where a pointer could have been mutated in place, and this is the
// benchmark that says what that is worth at the rate it actually happens.
//
// The repeats here land at the version already held, which replaces the newest value in place. A key
// repeated at a *later* version instead spills the previous value into the overflow deque, paying one
// allocation the first time it is written at a second version and none after. That path is not
// covered here; it costs one deque per repeatedly-written key, where the design this replaced paid
// one per key.
func BenchmarkShardBatchSetMixed(b *testing.B) {
	db := newTestDB(nil)
	defer func() { _ = db.Close() }()
	pool := threading.NewElasticPool("bench-shard-mixed", 4)
	defer pool.Close()

	config := DefaultTestSnapshotEngineConfig()
	config.EstimatedOverheadPerEntry = 256
	s, err := NewShard(context.Background(), config, db, pool, 1<<30,
		func() error { return ErrEngineClosed },
		func(error) {})
	if err != nil {
		b.Fatal(err)
	}

	repeated := benchWriteSet(0)
	repeatCount := int(float64(benchPairsPerBlock) * benchRepeatFraction)
	// Seed the repeated keys so they are already present when the measured writes reach them.
	if err := s.BatchSet(repeated[:repeatCount]); err != nil {
		b.Fatal(err)
	}

	sets := make([][]*proto.KVPair, b.N)
	for i := range sets {
		fresh := benchWriteSet(i + 1)
		sets[i] = append(append([]*proto.KVPair{}, repeated[:repeatCount]...), fresh[repeatCount:]...)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := s.BatchSet(sets[i]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDropVersions measures retiring one version's worth of writes. This runs on the lifecycle
// goroutine but holds the same exclusive shard lock BatchSet needs, so whatever it costs is time the
// execution thread can spend blocked. Compare it against the per-block BatchSet cost directly.
func BenchmarkDropVersions(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := newTestDB(nil)
		pool := threading.NewElasticPool("bench-drop", 4)
		config := DefaultTestSnapshotEngineConfig()
		config.EstimatedOverheadPerEntry = 256
		s, err := NewShard(context.Background(), config, db, pool, 1<<30,
			func() error { return ErrEngineClosed },
			func(error) {})
		if err != nil {
			b.Fatal(err)
		}
		// One version holding a block's worth of keys, sealed so it is eligible to retire.
		if err := s.BatchSet(benchWriteSet(0)); err != nil {
			b.Fatal(err)
		}
		version := s.Commit()
		b.StartTimer()

		if err := s.DropVersions(version-1, version); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		pool.Close()
		_ = db.Close()
		b.StartTimer()
	}
}
