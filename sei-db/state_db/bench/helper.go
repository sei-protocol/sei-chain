package bench

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cosmos/iavl"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/snapshots"
	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

const (
	// EVMStoreName simulates the EVM module store
	EVMStoreName = "evm"

	// KeySize EVM storage key: 0x03 prefix + 20-byte address + 32-byte slot = 53 bytes
	KeySize   = 53
	ValueSize = 32
)

// TestScenario bundles benchmark parameters and distribution.
type TestScenario struct {
	Name           string
	TotalKeys      int64
	NumBlocks      int64
	DuplicateRatio float64 // 0.0 = all inserts, 1.0 = all updates
	// The database backend to use for the benchmark.
	Backend      wrappers.DBType
	Distribution KeyDistribution

	// SnapshotPath, when set, points to a state sync snapshot chunks directory
	// (e.g. "<node_home>/data/snapshots/<height>/<format>/") containing numbered
	// chunk files (0, 1, 2, ...). Before the benchmark begins, the snapshot is
	// imported into the database via the native Committer.Importer path as a
	// preparation stage.
	SnapshotPath string
}

// KeyDistribution defines how many keys to generate per block.
type KeyDistribution func(numBlocks, totalKeys, block int64) int64

// EvenDistribution generates same number of keys on each block.
func EvenDistribution(numBlocks, totalKeys, _ int64) int64 {
	if numBlocks <= 0 || totalKeys < numBlocks {
		return 0
	}
	return totalKeys / numBlocks
}

// BurstyDistribution emits periodic bursts with optional jitter.
// Example: base=100 keys/block, burstEvery=5, burstMultiplier=3 =>
// blocks 0,5,10... emit 300 keys; other blocks emit 100 keys (then +/- jitter).
func BurstyDistribution(seed int64, burstEvery, burstMultiplier, maxJitter int64) KeyDistribution {
	rng := mrand.New(mrand.NewSource(seed))
	return func(numBlocks, totalKeys, block int64) int64 {
		if numBlocks <= 0 {
			return 0
		}
		keysPerBlock := totalKeys / numBlocks
		count := keysPerBlock
		if burstEvery > 0 && block%burstEvery == 0 {
			count *= burstMultiplier
		}
		if maxJitter > 0 {
			count += rng.Int63n(2*maxJitter+1) - maxJitter
		}
		if count < 0 {
			return 0
		}
		return count
	}
}

// NormalDistribution samples keys per block from a normal distribution.
// Example: totalKeys=1000, numBlocks=10, stddevFactor=0.2 =>
// mean=100 keys, stddev=20 keys
func NormalDistribution(seed int64, stddevFactor float64) KeyDistribution {
	rng := mrand.New(mrand.NewSource(seed))
	return func(numBlocks, totalKeys, _ int64) int64 {
		if numBlocks <= 0 {
			return 0
		}
		mean := float64(totalKeys) / float64(numBlocks)
		stddev := mean * stddevFactor
		if stddev <= 0 {
			return int64(mean)
		}
		count := int64(mean + rng.NormFloat64()*stddev)
		if count < 0 {
			return 0
		}
		return count
	}
}

// RampDistribution linearly ramps keysPerBlock by a factor over the run.
// Example: totalKeys=1000, numBlocks=10, startFactor=0.5, endFactor=1.5 =>
// per-block base=100; block 0 ~50 keys, block 9 ~150 keys (linearly interpolated).
func RampDistribution(startFactor, endFactor float64) KeyDistribution {
	return func(numBlocks, totalKeys, block int64) int64 {
		if numBlocks <= 1 {
			return int64(float64(totalKeys) * endFactor)
		}
		keysPerBlock := totalKeys / numBlocks
		t := float64(block) / float64(numBlocks-1)
		factor := startFactor + t*(endFactor-startFactor)
		count := int64(float64(keysPerBlock) * factor)
		if count < 0 {
			return 0
		}
		return count
	}
}

// ProgressReporter reports benchmark progress periodically.
type ProgressReporter struct {
	totalKeys   int64
	totalBlocks int64
	keysWritten atomic.Int64
	startTime   time.Time
	done        chan struct{}
	interval    time.Duration
}

// NewProgressReporter creates a new progress reporter.
func NewProgressReporter(totalKeys, totalBlocks int64, interval time.Duration) *ProgressReporter {
	return &ProgressReporter{
		totalKeys:   totalKeys,
		totalBlocks: totalBlocks,
		done:        make(chan struct{}),
		interval:    interval,
	}
}

// Start begins periodic progress reporting in a background goroutine.
func (p *ProgressReporter) Start() {
	p.startTime = time.Now()
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-ticker.C:
				p.report()
			}
		}
	}()
}

// Stop stops the progress reporter and prints final stats.
func (p *ProgressReporter) Stop() {
	close(p.done)
	elapsed := time.Since(p.startTime).Seconds()
	keys := p.keysWritten.Load()
	fmt.Printf("[Final] keys=%d/%d, keys/sec=%.0f, elapsed=%.2fs\n",
		keys, p.totalKeys, float64(keys)/elapsed, elapsed)
}

// Add records that keys were written.
func (p *ProgressReporter) Add(keys int) {
	p.keysWritten.Add(int64(keys))
}

func (p *ProgressReporter) report() {
	keys := p.keysWritten.Load()
	elapsed := time.Since(p.startTime).Seconds()
	if elapsed > 0 {
		keysPerBlock := p.totalKeys / p.totalBlocks
		if keysPerBlock > 0 {
			blocks := keys / keysPerBlock
			fmt.Printf("[Progress] blocks=%d/%d, keys=%d/%d, keys/sec=%.0f\n",
				blocks, p.totalBlocks, keys, p.totalKeys, float64(keys)/elapsed)
			return
		}
		fmt.Printf("[Progress] blocks=%d/%d, keys=%d/%d, keys/sec=%.0f\n",
			0, p.totalBlocks, keys, p.totalKeys, float64(keys)/elapsed)
	}
}

// startChangesetGenerator streams per-block changesets based on the scenario distribution.
func startChangesetGenerator(scenario TestScenario) <-chan *proto.NamedChangeSet {
	if scenario.Distribution == nil {
		scenario.Distribution = EvenDistribution
	}
	duplicateRatio := scenario.DuplicateRatio
	if duplicateRatio < 0 {
		duplicateRatio = 0
	}
	if duplicateRatio > 1 {
		duplicateRatio = 1
	}
	rng := mrand.New(mrand.NewSource(1))
	out := make(chan *proto.NamedChangeSet)
	go func() {
		defer close(out)
		var uniqueCounter int64
		for i := range scenario.NumBlocks {
			numKeysInBlock := scenario.Distribution(scenario.NumBlocks, scenario.TotalKeys, i)
			if numKeysInBlock < 0 {
				numKeysInBlock = 0
			}
			kvPairs := make([]*iavl.KVPair, int(numKeysInBlock))
			duplicateCount := int64(float64(numKeysInBlock) * duplicateRatio)
			for j := range kvPairs {
				var keyIndex int64
				if int64(j) < duplicateCount && uniqueCounter > 0 {
					keyIndex = rng.Int63n(uniqueCounter)
				} else {
					keyIndex = uniqueCounter
					uniqueCounter++
				}
				key := keyFromIndex(keyIndex)
				val := make([]byte, ValueSize)
				if _, err := rand.Read(val); err != nil {
					panic(fmt.Sprintf("failed to generate random value: %v", err))
				}
				kvPairs[j] = &iavl.KVPair{Key: key, Value: val}
			}
			cs := &proto.NamedChangeSet{
				Name:      EVMStoreName,
				Changeset: iavl.ChangeSet{Pairs: kvPairs},
			}
			out <- cs
		}
	}()
	return out
}

func keyFromIndex(index int64) []byte {
	key := make([]byte, KeySize)
	key[0] = 0x03
	var input [9]byte
	if index < 0 {
		panic(fmt.Sprintf("negative key index: %d", index))
	}
	binary.LittleEndian.PutUint64(input[1:], uint64(index))
	sum1 := sha256.Sum256(input[:])
	input[0] = 1
	sum2 := sha256.Sum256(input[:])
	copy(key[1:], sum1[:])
	copy(key[1+len(sum1):], sum2[:len(key)-1-len(sum1)])
	return key
}

// parseSnapshotHeight extracts the block height from a state sync snapshot
// chunks directory path. The expected layout is <snapshots>/<height>/<format>/,
// so the height is the parent of the format directory.
func parseSnapshotHeight(chunksDir string) (int64, error) {
	heightStr := filepath.Base(filepath.Dir(filepath.Clean(chunksDir)))
	h, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse snapshot height from path %q: %w", chunksDir, err)
	}
	if h <= 0 || h > math.MaxUint32 {
		return 0, fmt.Errorf("snapshot height %d out of range", h)
	}
	return h, nil
}

// openSnapshotStream opens the numbered chunk files in chunksDir and returns a
// StreamReader that decompresses and demuxes the protobuf item stream.
func openSnapshotStream(chunksDir string) (*snapshots.StreamReader, error) {
	if _, err := os.Stat(filepath.Join(chunksDir, "0")); err != nil {
		return nil, fmt.Errorf("no chunk files found in %s: %w", chunksDir, err)
	}

	chunks := make(chan io.ReadCloser)
	go func() {
		defer close(chunks)
		for i := 0; ; i++ {
			path := filepath.Join(chunksDir, strconv.Itoa(i))
			f, err := os.Open(filepath.Clean(path))
			if err != nil {
				if os.IsNotExist(err) {
					return
				}
				pr, pw := io.Pipe()
				_ = pw.CloseWithError(fmt.Errorf("open chunk %d: %w", i, err))
				chunks <- pr
				return
			}
			chunks <- f
		}
	}()

	return snapshots.NewStreamReader(chunks)
}

// importSnapshot reads a state sync snapshot from chunksDir and feeds every
// item through the given Importer (AddModule / AddNode). This is the same
// import path used by the real state sync restore logic.
// Returns the total number of leaf keys imported.
func importSnapshot(chunksDir string, importer sctypes.Importer) error {
	streamReader, err := openSnapshotStream(chunksDir)
	if err != nil {
		return fmt.Errorf("create stream reader: %w", err)
	}
	defer func() {
		_ = streamReader.Close()
	}()

	var (
		totalKeys  int64
		startTime  = time.Now()
		currModule = ""
	)

	for {
		var item snapshottypes.SnapshotItem
		err := streamReader.ReadMsg(&item)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read snapshot item: %w", err)
		}

		switch i := item.Item.(type) {
		case *snapshottypes.SnapshotItem_Store:
			currModule = i.Store.Name
			if currModule == "evm" {
				if err := importer.AddModule(i.Store.Name); err != nil {
					return fmt.Errorf("add module %s: %w", i.Store.Name, err)
				}
				fmt.Printf("[Snapshot] Importing store: %s\n", i.Store.Name)
			} else {
				fmt.Printf("[Snapshot] Skipping store: %s\n", i.Store.Name)
			}
		case *snapshottypes.SnapshotItem_IAVL:
			if currModule != "evm" {
				continue
			}
			if i.IAVL.Height > math.MaxInt8 {
				return fmt.Errorf("node height %d exceeds int8", i.IAVL.Height)
			}
			node := &sctypes.SnapshotNode{
				Key:     i.IAVL.Key,
				Value:   i.IAVL.Value,
				Height:  int8(i.IAVL.Height), //nolint:gosec
				Version: i.IAVL.Version,
			}
			if node.Height == 0 && node.Value == nil {
				node.Value = []byte{}
			}
			importer.AddNode(node)
			if node.Height == 0 {
				totalKeys++
				if totalKeys%1_000_000 == 0 {
					elapsed := time.Since(startTime).Seconds()
					fmt.Printf("[Snapshot] keys=%d, keys/sec=%.0f, elapsed=%.2fs\n",
						totalKeys, float64(totalKeys)/elapsed, elapsed)
				}
			}
		default:
			break
		}
	}

	elapsed := time.Since(startTime).Seconds()
	fmt.Printf("[Snapshot] Import Done: keys=%d, keys/sec=%.0f, elapsed=%.2fs\n",
		totalKeys, float64(totalKeys)/elapsed, elapsed)

	return importer.Close()
}

// runBenchmark runs the benchmark with optional progress reporting.
// If withProgress is true, reports keys/sec every 5 seconds to stdout.
func runBenchmark(b *testing.B, scenario TestScenario, withProgress bool) {
	if scenario.Distribution == nil {
		scenario.Distribution = EvenDistribution
	}

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		func() {
			dbDir := b.TempDir()
			b.StopTimer()
			cs := wrappers.NewDBImpl(b, dbDir, scenario.Backend)
			require.NotNil(b, cs)

			// Load snapshot if available
			if scenario.SnapshotPath != "" {
				snapshotHeight, err := parseSnapshotHeight(scenario.SnapshotPath)
				require.NoError(b, err)
				importer, err := cs.Importer(snapshotHeight)
				require.NoError(b, err)
				err = importSnapshot(scenario.SnapshotPath, importer)
				require.NoError(b, err)
				err = cs.LoadVersion(0)
				require.NoError(b, err)
			}
			changesetChannel := startChangesetGenerator(scenario)

			var progress *ProgressReporter
			if withProgress {
				progress = NewProgressReporter(scenario.TotalKeys, scenario.NumBlocks, 5*time.Second)
				progress.Start()
			}

			baseVersion := cs.Version()
			b.StartTimer()
			fmt.Printf("Opening DB with base version %d\n", baseVersion)

			for block := int64(1); block < scenario.NumBlocks; block++ {
				changeset, ok := <-changesetChannel
				if !ok {
					break
				}
				err := cs.ApplyChangeSets([]*proto.NamedChangeSet{changeset})
				require.NoError(b, err)
				version, err := cs.Commit()
				require.NoError(b, err)
				require.Equal(b, baseVersion+block, version)
				if progress != nil {
					progress.Add(len(changeset.Changeset.Pairs))
				}
			}
			err := cs.Close() // close to make sure all data got flushed
			require.NoError(b, err)

			b.StopTimer()
			if progress != nil {
				progress.Stop()
			}

			elapsed := b.Elapsed().Seconds()
			b.ReportMetric(float64(scenario.TotalKeys)/elapsed, "keys/sec")
			b.ReportMetric(elapsed, "seconds")
		}()
	}
}
