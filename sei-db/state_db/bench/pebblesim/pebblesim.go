// Package pebblesim writes a synthetic mix of EVM storage-slot, balance, and nonce updates into
// a PebbleDB state store at a steady rate, so Pebble's compaction, flush, and disk metrics can be
// observed over a sustained run.
package pebblesim

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
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

	// contractAddressType tags simulated addresses. The same pool backs storage, balance,
	// and nonce keys — a small set of hot accounts getting repeated writes, like real
	// EVM traffic.
	contractAddressType = 'c'

	// rngStreamMix is the PCG stream constant so seed s and s+1 don't share a sequence.
	rngStreamMix = 0x9e3779b97f4a7c15

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

	// QueueDepth is how many generated batches to buffer ahead of the writer, so a slow
	// generation batch can be absorbed by batches already queued instead of stalling the write.
	QueueDepth int

	// Presort orders each batch with mvcc.SortChangesetPairs in buildBatch, on the generator
	// goroutine, before it reaches WriteBatch. The write path then only scans to confirm the
	// order rather than sorting the generator's unsorted slot/balance/nonce runs, which moves
	// that cost off the write's critical path.
	Presort bool

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
		QueueDepth:       4,
		Presort:          false,
		Seed:             1,
	}
}

// PebbleSim drives a PebbleDB state store with a synthetic mix of storage-slot, balance, and
// nonce writes. Key/value generation runs on a dedicated goroutine (started by Generate) that
// feeds pre-built batches through a channel to WriteBatch, so Pebble's write throughput isn't
// gated by generation cost. rng and contracts are owned exclusively by that goroutine once
// Generate starts; WriteBatch never touches them.
type PebbleSim struct {
	cfg       Config
	store     types.StateStore
	rng       *rand.Rand
	contracts [][]byte
	version   int64
	metrics   *simMetrics
	batches   chan batch
}

// batch is one generated set of key/value updates, along with the per-kind counts WriteBatch
// needs for metrics — computed once here rather than re-derived from pairs after the fact.
type batch struct {
	pairs                    []*proto.KVPair
	nSlots, nBalance, nNonce int
	sortElapsed              time.Duration
}

// Open creates (or resumes) the EVM state store at cfg.DataDir, PebbleDB-backed, the same store
// type an archival node uses to serve historical EVM queries.
func Open(cfg Config) (*PebbleSim, error) {
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = config.PebbleDBBackend

	// TODO: check if fsync is enabled or not.
	// TODO: check memtables size. increase it.
	// TODO: give more threads to the garbage collector. and checkl related metrics. tableGarbagePointDeletionsEstimate: tableGarbagePointDeletionsEstimate,
	// tableGarbageRangeDeletionsEstimate: tableGarbageRangeDeletionsEstimate,

	// TODO: check ec2 memory speed

	// TODO: check this https://github.com/sei-protocol/sei-chain/tree/cjl/snapshot-experiments

	ssConfig.SeparateEVMSubDBs = true

	store, err := evmss.NewEVMStateStore(cfg.DataDir, ssConfig)
	if err != nil {
		return nil, fmt.Errorf("open evm state store: %w", err)
	}

	rng := newSimRNG(cfg.Seed)

	contracts := make([][]byte, cfg.NumContracts)
	for i := range contracts {
		contracts[i] = makeContractAddress(rng, int64(i))
	}

	return &PebbleSim{
		cfg:       cfg,
		store:     store,
		rng:       rng,
		contracts: contracts,
		version:   store.GetLatestVersion(),
		metrics:   newSimMetrics(),
		batches:   make(chan batch, cfg.QueueDepth),
	}, nil
}

// Generate starts the background goroutine that builds batches and feeds them into the queue
// WriteBatch drains, keeping up to cfg.QueueDepth batches ready ahead of the writer. It runs
// until ctx is done.
func (p *PebbleSim) Generate(ctx context.Context) {
	go func() {
		for {
			b := p.buildBatch()
			select {
			case p.batches <- b:
			case <-ctx.Done():
				return
			}
		}
	}()
}

// buildBatch generates one batch of cfg.BatchSize random key/value updates, split 60/25/15
// across storage slots, balances, and nonces. If cfg.Presort is set, pairs are handed to
// WriteBatch already in the store's write order.
func (p *PebbleSim) buildBatch() batch {
	nBalance := p.cfg.BatchSize * balancePct / 100
	nNonce := p.cfg.BatchSize * noncePct / 100
	nSlots := p.cfg.BatchSize - nBalance - nNonce

	pairs := make([]*proto.KVPair, 0, p.cfg.BatchSize)
	slotVals := make([]byte, nSlots*slotLen)
	for i := 0; i < nSlots; i++ {
		val := slotVals[i*slotLen : (i+1)*slotLen]
		fillBytes(p.rng, val)
		pairs = append(pairs, &proto.KVPair{Key: p.randomStorageKey(), Value: val})
	}
	balanceVals := make([]byte, nBalance*balanceValueLen)
	for i := 0; i < nBalance; i++ {
		val := balanceVals[i*balanceValueLen : (i+1)*balanceValueLen]
		fillBytes(p.rng, val)
		pairs = append(pairs, &proto.KVPair{Key: p.randomBalanceKey(), Value: val})
	}
	nonceVals := make([]byte, nNonce*nonceValueLen)
	for i := 0; i < nNonce; i++ {
		val := nonceVals[i*nonceValueLen : (i+1)*nonceValueLen]
		fillBytes(p.rng, val)
		pairs = append(pairs, &proto.KVPair{Key: p.randomNonceKey(), Value: val})
	}

	var sortElapsed time.Duration
	if p.cfg.Presort {
		sortStart := time.Now()
		mvcc.SortChangesetPairs(pairs)
		sortElapsed = time.Since(sortStart)
	}

	return batch{pairs: pairs, nSlots: nSlots, nBalance: nBalance, nNonce: nNonce, sortElapsed: sortElapsed}
}

// BatchResult reports what one WriteBatch call did: the version it wrote, the full wall-clock
// time, the Pebble-write-only portion of that time, how long the call stalled waiting for the
// generator to hand over a batch, and — if cfg.Presort is set — how long that batch spent being
// sorted on the generator goroutine. Sort happened earlier, concurrently with a previous
// WriteBatch call, so it is not part of Total; it is reported separately to keep that cost
// visible rather than hidden by the pipeline.
type BatchResult struct {
	Version int64
	Total   time.Duration
	Write   time.Duration
	Stall   time.Duration
	Sort    time.Duration
}

// WriteBatch takes the next pre-generated batch of cfg.BatchSize key/value updates and writes it
// to Pebble at the next version. Compare Total against cfg.BatchInterval to tell whether this
// batch missed its block deadline — Total is what a real block-time budget has to cover. Write
// isolates just the ApplyChangesetSync/SetLatestVersion call, and Stall isolates the wait for the
// generator goroutine, so a miss can be attributed to Pebble itself rather than to this
// benchmark's own key/value generation falling behind. All three are also recorded as
// pebblesim_batch_duration_seconds / pebblesim_write_duration_seconds / pebblesim_stall_duration_seconds,
// alongside pebblesim_sort_duration_seconds for Sort.
func (p *PebbleSim) WriteBatch(ctx context.Context) (BatchResult, error) {
	batchStart := time.Now()

	stallStart := time.Now()
	var b batch
	select {
	case b = <-p.batches:
	case <-ctx.Done():
		return BatchResult{}, ctx.Err()
	}
	stallElapsed := time.Since(stallStart)

	p.version++
	changesets := []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: b.pairs},
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

	p.metrics.batchDuration.Record(ctx, totalElapsed.Seconds())
	p.metrics.writeDuration.Record(ctx, writeElapsed.Seconds())
	p.metrics.stallDuration.Record(ctx, stallElapsed.Seconds())
	p.metrics.sortDuration.Record(ctx, b.sortElapsed.Seconds())
	p.metrics.batchesWritten.Add(ctx, 1)
	p.metrics.keysWritten.Add(ctx, int64(b.nSlots), metric.WithAttributes(attribute.String("kind", "slot")))
	p.metrics.keysWritten.Add(ctx, int64(b.nBalance), metric.WithAttributes(attribute.String("kind", "balance")))
	p.metrics.keysWritten.Add(ctx, int64(b.nNonce), metric.WithAttributes(attribute.String("kind", "nonce")))
	if totalElapsed > p.cfg.BatchInterval {
		p.metrics.deadlineMisses.Add(ctx, 1)
	}

	return BatchResult{Version: p.version, Total: totalElapsed, Write: writeElapsed, Stall: stallElapsed, Sort: b.sortElapsed}, nil
}

// randomStorageKey builds a real EVM storage-slot key (0x03 || address || slot) for a random
// contract from the simulated pool.
func (p *PebbleSim) randomStorageKey() []byte {
	addr := p.randomAddress()
	slotID := p.rng.Int64N(p.cfg.SlotsPerContract)

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
	return p.contracts[p.rng.Int64N(int64(len(p.contracts)))]
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

func newSimRNG(seed int64) *rand.Rand {
	s := uint64(seed) //nolint:gosec // G115 - benchmark seed, wrap is fine
	return rand.New(rand.NewPCG(s, s^rngStreamMix))
}

func makeContractAddress(rng *rand.Rand, id int64) []byte {
	addr := make([]byte, keys.AddressLen)
	fillBytes(rng, addr)
	addr[0] = contractAddressType
	binary.BigEndian.PutUint64(addr[9:], uint64(id)) //nolint:gosec // G115 - contract index
	return addr
}

func fillBytes(rng *rand.Rand, dst []byte) {
	for i := 0; i < len(dst); {
		x := rng.Uint64()
		for n := 0; n < 8 && i < len(dst); n++ {
			dst[i] = byte(x)
			x >>= 8
			i++
		}
	}
}
