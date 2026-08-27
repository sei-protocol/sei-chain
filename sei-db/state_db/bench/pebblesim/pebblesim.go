// Package pebblesim writes synthetic EVM storage-slot updates into a PebbleDB
// state store at a steady rate, so Pebble's compaction, flush, and disk
// metrics can be observed over a sustained run.
package pebblesim

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	evmss "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
)

const (
	// slotLen is the length in bytes of an EVM storage slot key and value.
	slotLen = 32

	// contractAddressType tags simulated contract addresses (see crand.CannedRandom.Address).
	contractAddressType = 'c'
)

// Config controls the synthetic write load.
type Config struct {
	// DataDir is the PebbleDB data directory.
	DataDir string

	// NumContracts is the size of the simulated contract pool.
	NumContracts int

	// SlotsPerContract bounds the slot index range written per contract, so slots get revisited
	// and accumulate real version history instead of growing the keyspace forever.
	SlotsPerContract int64

	// BatchSize is the number of storage-slot writes per batch.
	BatchSize int

	// BatchInterval is the time between batches.
	BatchInterval time.Duration

	// Seed makes the synthetic contract pool and slot/value data deterministic.
	Seed int64
}

// DefaultConfig returns batches of 1,000 storage-slot writes, twice a second.
func DefaultConfig() Config {
	return Config{
		NumContracts:     100,
		SlotsPerContract: 1000,
		BatchSize:        1000,
		BatchInterval:    500 * time.Millisecond,
		Seed:             1,
	}
}

// PebbleSim drives a PebbleDB state store with synthetic storage-slot writes.
type PebbleSim struct {
	cfg       Config
	store     types.StateStore
	rng       *crand.CannedRandom
	contracts [][]byte
	version   int64
	metrics   *simMetrics
}

// Open creates (or resumes) the EVM state store at cfg.DataDir, PebbleDB-backed, the same store
// type an archival node uses to serve historical EVM queries.
func Open(cfg Config) (*PebbleSim, error) {
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = config.PebbleDBBackend

	store, err := evmss.NewEVMStateStore(cfg.DataDir, ssConfig)
	if err != nil {
		return nil, fmt.Errorf("open evm state store: %w", err)
	}

	rng := crand.NewCannedRandom(1<<20, cfg.Seed)
	contracts := make([][]byte, cfg.NumContracts)
	for i := range contracts {
		contracts[i] = rng.Address(contractAddressType, int64(i), keys.AddressLen)
	}

	return &PebbleSim{
		cfg:       cfg,
		store:     store,
		rng:       rng,
		contracts: contracts,
		version:   store.GetLatestVersion(),
		metrics:   newSimMetrics(),
	}, nil
}

// BatchResult reports what one WriteBatch call did: the version it wrote, the full wall-clock
// time (key/value generation plus the Pebble write), and the Pebble-write-only portion of that
// time.
type BatchResult struct {
	Version int64
	Total   time.Duration
	Write   time.Duration
}

// WriteBatch writes cfg.BatchSize random storage-slot updates at the next version. Compare
// Total against cfg.BatchInterval to tell whether this batch missed its block deadline — Total
// is what a real block-time budget has to cover. Write isolates just the
// ApplyChangesetSync/SetLatestVersion call, so a miss can be attributed to Pebble itself rather
// than to this benchmark's own key/value generation. Both are also recorded as
// pebblesim_batch_duration_seconds / pebblesim_write_duration_seconds.
func (p *PebbleSim) WriteBatch() (BatchResult, error) {
	batchStart := time.Now()
	p.version++

	pairs := make([]*proto.KVPair, p.cfg.BatchSize)
	for i := range pairs {
		pairs[i] = &proto.KVPair{Key: p.randomStorageKey(), Value: p.rng.Bytes(slotLen)}
	}
	changesets := []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}

	writeStart := time.Now()
	if err := p.store.ApplyChangesetSync(p.version, changesets); err != nil {
		return BatchResult{}, fmt.Errorf("apply changeset at version %d: %w", p.version, err)
	}
	if err := p.store.SetLatestVersion(p.version); err != nil {
		return BatchResult{}, fmt.Errorf("set latest version to %d: %w", p.version, err)
	}
	writeElapsed := time.Since(writeStart)
	totalElapsed := time.Since(batchStart)

	p.metrics.batchDuration.Record(context.Background(), totalElapsed.Seconds())
	p.metrics.writeDuration.Record(context.Background(), writeElapsed.Seconds())
	p.metrics.batchesWritten.Add(context.Background(), 1)
	p.metrics.slotsWritten.Add(context.Background(), int64(p.cfg.BatchSize))
	if totalElapsed > p.cfg.BatchInterval {
		p.metrics.deadlineMisses.Add(context.Background(), 1)
	}

	return BatchResult{Version: p.version, Total: totalElapsed, Write: writeElapsed}, nil
}

// randomStorageKey builds a real EVM storage-slot key (0x03 || address || slot) for a random
// contract from the simulated pool.
func (p *PebbleSim) randomStorageKey() []byte {
	addr := p.contracts[p.rng.Int64Range(0, int64(len(p.contracts)))]
	slotID := p.rng.Int64Range(0, p.cfg.SlotsPerContract)

	key := make([]byte, 0, len(keys.StateKeyPrefix())+keys.AddressLen+slotLen)
	key = append(key, keys.StateKeyPrefix()...)
	key = append(key, addr...)

	slot := make([]byte, slotLen)
	//nolint:gosec // G115 - slotID is bounded by cfg.SlotsPerContract, never negative or overflowing
	binary.BigEndian.PutUint64(slot[slotLen-8:], uint64(slotID))
	return append(key, slot...)
}

// Version returns the most recently written version.
func (p *PebbleSim) Version() int64 {
	return p.version
}

// Close releases the underlying PebbleDB store.
func (p *PebbleSim) Close() error {
	return p.store.Close()
}
