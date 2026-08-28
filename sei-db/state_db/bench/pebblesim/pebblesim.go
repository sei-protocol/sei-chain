// Package pebblesim writes a synthetic mix of EVM storage-slot, balance, and nonce updates into
// a PebbleDB state store at a steady rate, so Pebble's compaction, flush, and disk metrics can be
// observed over a sustained run.
package pebblesim

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

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

	// nonceValueLen matches x/evm/keeper's binary.BigEndian.PutUint64 nonce encoding.
	nonceValueLen = 8

	// balanceValueLen approximates a uint256 EVM balance. Sei doesn't store balances in this SS
	// layer yet (they still live in the tendermint/IAVL store), so there's no production format
	// to mirror; this is just sized to match a real EVM word.
	balanceValueLen = 32

	// contractAddressType tags simulated addresses (see crand.CannedRandom.Address). The same
	// pool backs storage, balance, and nonce keys — a small set of hot accounts getting
	// repeated writes, like real EVM traffic.
	contractAddressType = 'c'

	// balancePct and noncePct set the synthetic write mix; slots take the remaining share (plus
	// any rounding remainder) since EVM archive traffic is storage-slot heavy, with balance and
	// nonce updates riding along on every transfer from a much smaller set of hot accounts.
	balancePct = 25
	noncePct   = 15
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

	// BatchSize is the number of key/value writes per batch, split 60/25/15 across storage
	// slots, balances, and nonces.
	BatchSize int

	// BatchInterval is the time between batches.
	BatchInterval time.Duration

	// Seed makes the synthetic contract pool and slot/value data deterministic.
	Seed int64
}

// DefaultConfig returns batches of 1,000 key/value writes, twice a second.
func DefaultConfig() Config {
	return Config{
		NumContracts:     100,
		SlotsPerContract: 1000,
		BatchSize:        1000,
		BatchInterval:    500 * time.Millisecond,
		Seed:             1,
	}
}

// PebbleSim drives a PebbleDB state store with a synthetic mix of storage-slot, balance, and
// nonce writes.
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

	ssConfig.SeparateEVMSubDBs = true

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

// WriteBatch writes cfg.BatchSize random key/value updates — split 60/25/15 across storage
// slots, balances, and nonces — at the next version. Compare Total against cfg.BatchInterval to
// tell whether this batch missed its block deadline — Total is what a real block-time budget has
// to cover. Write isolates just the ApplyChangesetSync/SetLatestVersion call, so a miss can be
// attributed to Pebble itself rather than to this benchmark's own key/value generation. Both are
// also recorded as pebblesim_batch_duration_seconds / pebblesim_write_duration_seconds.
func (p *PebbleSim) WriteBatch() (BatchResult, error) {
	batchStart := time.Now()
	p.version++

	nBalance := p.cfg.BatchSize * balancePct / 100
	nNonce := p.cfg.BatchSize * noncePct / 100
	nSlots := p.cfg.BatchSize - nBalance - nNonce

	pairs := make([]*proto.KVPair, 0, p.cfg.BatchSize)
	for i := 0; i < nSlots; i++ {
		pairs = append(pairs, &proto.KVPair{Key: p.randomStorageKey(), Value: p.rng.Bytes(slotLen)})
	}
	for i := 0; i < nBalance; i++ {
		pairs = append(pairs, &proto.KVPair{Key: p.randomBalanceKey(), Value: p.rng.Bytes(balanceValueLen)})
	}
	for i := 0; i < nNonce; i++ {
		pairs = append(pairs, &proto.KVPair{Key: p.randomNonceKey(), Value: p.rng.Bytes(nonceValueLen)})
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

	ctx := context.Background()
	p.metrics.batchDuration.Record(ctx, totalElapsed.Seconds())
	p.metrics.writeDuration.Record(ctx, writeElapsed.Seconds())
	p.metrics.batchesWritten.Add(ctx, 1)
	p.metrics.keysWritten.Add(ctx, int64(nSlots), metric.WithAttributes(attribute.String("kind", "slot")))
	p.metrics.keysWritten.Add(ctx, int64(nBalance), metric.WithAttributes(attribute.String("kind", "balance")))
	p.metrics.keysWritten.Add(ctx, int64(nNonce), metric.WithAttributes(attribute.String("kind", "nonce")))
	if totalElapsed > p.cfg.BatchInterval {
		p.metrics.deadlineMisses.Add(ctx, 1)
	}

	return BatchResult{Version: p.version, Total: totalElapsed, Write: writeElapsed}, nil
}

// randomStorageKey builds a real EVM storage-slot key (0x03 || address || slot) for a random
// contract from the simulated pool.
func (p *PebbleSim) randomStorageKey() []byte {
	addr := p.randomAddress()
	slotID := p.rng.Int64Range(0, p.cfg.SlotsPerContract)

	key := make([]byte, 0, len(keys.StateKeyPrefix())+keys.AddressLen+slotLen)
	key = append(key, keys.StateKeyPrefix()...)
	key = append(key, addr...)

	slot := make([]byte, slotLen)
	//nolint:gosec // G115 - slotID is bounded by cfg.SlotsPerContract, never negative or overflowing
	binary.BigEndian.PutUint64(slot[slotLen-8:], uint64(slotID))
	return append(key, slot...)
}

// randomAddress picks a random address from the simulated pool.
func (p *PebbleSim) randomAddress() []byte {
	return p.contracts[p.rng.Int64Range(0, int64(len(p.contracts)))]
}

// randomNonceKey builds a real EVM nonce key (0x0a || address) for a random address from the
// simulated pool.
func (p *PebbleSim) randomNonceKey() []byte {
	return keys.BuildEVMKey(keys.EVMKeyNonce, p.randomAddress())
}

// randomBalanceKey builds a synthetic balance key (evmss.StoreBalance || address) for a random
// address from the simulated pool. Balances have no real on-disk key format yet — evmss.StoreBalance
// is the sub-DB type the codebase already reserves for them ("reserved for future migration"; they
// currently live in the tendermint/IAVL store, not this SS layer), so reusing it here keeps this
// benchmark's placeholder in sync with that reservation instead of picking an independent value.
func (p *PebbleSim) randomBalanceKey() []byte {
	key := make([]byte, 0, 1+keys.AddressLen)
	key = append(key, byte(evmss.StoreBalance))
	return append(key, p.randomAddress()...)
}

// Version returns the most recently written version.
func (p *PebbleSim) Version() int64 {
	return p.version
}

// Close releases the underlying PebbleDB store.
func (p *PebbleSim) Close() error {
	return p.store.Close()
}
