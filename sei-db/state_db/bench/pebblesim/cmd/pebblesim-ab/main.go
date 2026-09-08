// Command pebblesim-ab is an A/B experiment on a fixed, bounded key set versus an
// ever-growing one, both driven at 500,000 storage-slot writes/sec with a 1-second block
// time. "hot" uses exactly 5,000 contracts x 100 slots (= 500,000 combinations, matching
// the per-block write count): every block deterministically writes a new version to every
// single one of those combinations, so each key accumulates one real, distinct version per
// block — no collisions, nothing silently dropped. "cold" writes 500,000 brand-new
// (contract, slot) combinations every block, via a counter that never repeats, so no key is
// ever revisited. Both write the exact same number of storage slots overall; only whether
// those slots repeat across blocks differs. This bypasses the pebblesim package's random,
// with-replacement key sampling entirely — that sampling is what caused most same-block
// draws to collide when the key pool was tiny (see the previous run of this tool) — in favor
// of building each block's changeset directly against the store.
//
// Addresses and slots are built to avoid artificial padding that would bias the compression
// measurement: addresses are fully scattered across all 20 bytes (no embedded id, no type
// tag — see scatterAddress), and slots are an even mix of array-style (small, zero-padded,
// like a simple variable) and mapping-style (full 32-byte hash, like a balances entry) — see
// arraySlot/mappingSlot.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	evmss "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
)

const (
	writesPerBlock = 500_000
	totalBlocks    = 180 // 3 simulated minutes at 1 block/sec

	// hotContracts * hotSlots must equal writesPerBlock: every block sweeps the whole grid.
	hotContracts = 5_000
	hotSlots     = 100

	slotValueLen = 32

	// assumedBlockInterval is the block time totalBlocks is assumed to represent, used only
	// to extrapolate the measured size to a daily growth rate.
	assumedBlockInterval = 1 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type result struct {
	totalWrites    int64
	compactedBytes int64
	dailyBytes     int64
}

func run() error {
	ctx := context.Background()

	hot, err := runScenario(ctx, "hot (5k x 100 grid, reused every block)", hotGenerator())
	if err != nil {
		return fmt.Errorf("hot: %w", err)
	}
	report("hot (5k x 100 grid, reused every block)", hot)

	cold, err := runScenario(ctx, "cold (new keys every block)", coldGenerator())
	if err != nil {
		return fmt.Errorf("cold: %w", err)
	}
	report("cold (new keys every block)", cold)

	if hot.compactedBytes > 0 {
		ratio := float64(cold.compactedBytes) / float64(hot.compactedBytes)
		log.Printf("cold is %.1fx the on-disk size of hot for the same %d total slot-writes",
			ratio, int64(totalBlocks)*writesPerBlock)
	}
	return nil
}

func report(name string, r result) {
	log.Printf("%-36s %14d total writes  compacted %10s  => %s/day",
		name, r.totalWrites, formatBytes(r.compactedBytes), formatBytes(r.dailyBytes))
}

// blockGenerator fills pairs (already sized to writesPerBlock) with this block's writes.
type blockGenerator func(pairs []*proto.KVPair)

// hotGenerator sweeps the full 5,000 x 100 grid every block, in the same order each time, so
// every one of the 500,000 keys gets exactly one new version per block. Half of each
// contract's slots are array-style (a small zero-padded index, like a simple Solidity
// variable); the other half are mapping-style (a full 32-byte hash, like a balances mapping
// entry) — see arraySlot/mappingSlot.
func hotGenerator() blockGenerator {
	rng := rand.New(rand.NewPCG(1, 1))
	return func(pairs []*proto.KVPair) {
		i := 0
		for c := uint64(0); c < hotContracts; c++ {
			addr := scatterAddress(c)
			for s := uint64(0); s < hotSlots; s++ {
				slot := arraySlot(s)
				if s >= hotSlots/2 {
					slot = mappingSlot(c*hotSlots + s)
				}
				val := make([]byte, slotValueLen)
				fillRandom(rng, val)
				pairs[i] = &proto.KVPair{Key: storageKey(addr, slot), Value: val}
				i++
			}
		}
	}
}

// coldGenerator hands out a never-repeating (address, slot) pair on every call, alternating
// array-style and mapping-style slots (see hotGenerator). The address is always
// scatterAddress(next) — a bijection, so distinct counter values guarantee distinct
// addresses, and therefore distinct keys, regardless of slot style.
func coldGenerator() blockGenerator {
	rng := rand.New(rand.NewPCG(2, 2))
	var next uint64
	return func(pairs []*proto.KVPair) {
		for i := range pairs {
			addr := scatterAddress(next)
			slot := mappingSlot(next)
			if next%2 == 0 {
				slot = arraySlot(next / 2)
			}
			val := make([]byte, slotValueLen)
			fillRandom(rng, val)
			pairs[i] = &proto.KVPair{Key: storageKey(addr, slot), Value: val}
			next++
		}
	}
}

// splitmix64 is the standard SplitMix64 finalizer: a bijection on uint64, used here purely to
// scatter a monotonic counter into realistic-looking, non-sequential bytes.
func splitmix64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// scatterAddress expands id into a full 20-byte address with no embedded structure (no type
// tag, no raw id bytes) — real EVM addresses are hash-derived and carry no such structure
// either. splitmix64(id) alone is already a bijection, so distinct ids always land on
// distinct addresses even though every byte looks scattered.
func scatterAddress(id uint64) []byte {
	addr := make([]byte, keys.AddressLen)
	binary.BigEndian.PutUint64(addr[0:8], splitmix64(id))
	binary.BigEndian.PutUint64(addr[8:16], splitmix64(id+0x9E3779B97F4A7C15))
	var tail [8]byte
	binary.BigEndian.PutUint64(tail[:], splitmix64(id+0xD1B54A32D192ED03))
	copy(addr[16:20], tail[:4])
	return addr
}

// arraySlot represents a simple/array Solidity variable: a small integer left-padded with
// zeros, matching how a real sequential storage slot looks on disk.
func arraySlot(n uint64) []byte {
	slot := make([]byte, slotValueLen)
	binary.BigEndian.PutUint64(slot[slotValueLen-8:], n)
	return slot
}

// mappingSlot represents a Solidity mapping entry, whose real slot is keccak256(key ++
// base) — a full 32-byte hash with no padding. This isn't a real keccak256 (no cryptographic
// property is needed here), just enough chained splitmix64 calls to fill all 32 bytes with no
// embedded structure.
func mappingSlot(x uint64) []byte {
	slot := make([]byte, slotValueLen)
	binary.BigEndian.PutUint64(slot[0:8], splitmix64(x))
	binary.BigEndian.PutUint64(slot[8:16], splitmix64(x+0x9E3779B97F4A7C15))
	binary.BigEndian.PutUint64(slot[16:24], splitmix64(x+0xD1B54A32D192ED03))
	binary.BigEndian.PutUint64(slot[24:32], splitmix64(x+0xBF58476D1CE4E5B9))
	return slot
}

// storageKey builds a real EVM storage-slot key (0x03 || address || slot) from a pre-built
// address and slot.
func storageKey(addr, slot []byte) []byte {
	key := make([]byte, 0, len(keys.StateKeyPrefix())+len(addr)+len(slot))
	key = append(key, keys.StateKeyPrefix()...)
	key = append(key, addr...)
	key = append(key, slot...)
	return key
}

func fillRandom(rng *rand.Rand, dst []byte) {
	for i := 0; i < len(dst); {
		x := rng.Uint64()
		for n := 0; n < 8 && i < len(dst); n++ {
			dst[i] = byte(x)
			x >>= 8
			i++
		}
	}
}

func runScenario(ctx context.Context, name string, gen blockGenerator) (result, error) {
	dir, err := os.MkdirTemp("", "pebblesim-ab-*")
	if err != nil {
		return result{}, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(dir)

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = config.PebbleDBBackend
	ssConfig.SeparateEVMSubDBs = true

	store, err := evmss.NewEVMStateStore(dir, ssConfig)
	if err != nil {
		return result{}, fmt.Errorf("open: %w", err)
	}

	start := time.Now()
	pairs := make([]*proto.KVPair, writesPerBlock)
	for version := int64(1); version <= totalBlocks; version++ {
		gen(pairs)
		changesets := []*proto.NamedChangeSet{{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: pairs}}}
		if err := store.ApplyChangesetSync(version, changesets); err != nil {
			return result{}, fmt.Errorf("apply changeset at version %d: %w", version, err)
		}
		if err := store.SetLatestVersion(version); err != nil {
			return result{}, fmt.Errorf("set latest version to %d: %w", version, err)
		}
	}
	log.Printf("%s: wrote %d blocks (%d writes/block) in %s, compacting...",
		name, totalBlocks, writesPerBlock, time.Since(start).Round(time.Millisecond))

	if err := store.Compact(); err != nil {
		return result{}, fmt.Errorf("compact: %w", err)
	}

	// Close before measuring: manual compaction can leave background obsolete-file cleanup
	// still running asynchronously, which would race a directory walk. Close blocks until the
	// store (and that cleanup) has fully quiesced, so the listing below is stable.
	if err := store.Close(); err != nil {
		return result{}, fmt.Errorf("close: %w", err)
	}

	size, err := dirSize(dir)
	if err != nil {
		return result{}, fmt.Errorf("measure size: %w", err)
	}

	seconds := float64(totalBlocks) * assumedBlockInterval.Seconds()
	dailyBytes := int64(float64(size) / seconds * 86400)

	return result{
		totalWrites:    int64(totalBlocks) * writesPerBlock,
		compactedBytes: size,
		dailyBytes:     dailyBytes,
	}, nil
}

// dirSize sums the on-disk size of every file under dir (SSTs, WAL, MANIFEST, OPTIONS, ...) —
// the full footprint the data directory actually occupies, not just table files.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
