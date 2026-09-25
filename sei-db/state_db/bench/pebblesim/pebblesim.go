// Package pebblesim writes a synthetic mix of EVM storage-slot, balance, and nonce updates into
// a PebbleDB state store at a steady rate, so Pebble's compaction, flush, and disk metrics can be
// observed over a sustained run.
package pebblesim

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	evmss "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/pruning"
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

	// rngStreamMix is the PCG stream constant so seed s and s+1 don't share a sequence.
	rngStreamMix = 0x9e3779b97f4a7c15

	// balancePct and noncePct set the synthetic write mix; slots take the remaining share (plus
	// any rounding remainder) since EVM archive traffic is storage-slot heavy, with balance and
	// nonce updates riding along on every transfer from a much smaller set of hot accounts.
	balancePct = 25
	noncePct   = 15

	// readPoolKeysPerBatch bounds how many keys from one written batch are offered to readPool.
	// BatchSize can run into the hundreds of thousands; the reservoir only needs a steady trickle
	// of representative samples, not a full copy of every write.
	readPoolKeysPerBatch = 100

	// Domain separators for the id-to-key derivations, so a contract id, an account id and a
	// slot id that happen to share a numeric value never derive the same bytes.
	contractAddrSalt = 0x1d8e_3f52_a6b1_c704
	contractPickSalt = 0x27b5_e91c_4f08_6ad2
	accountAddrSalt  = 0x9b41_07de_5c2a_f883
	slotSalt         = 0x64f2_ba19_0d37_e5c1
	slotStyleSalt    = 0xf3a7_5c60_918b_2d4e

	// codeValueLen approximates a small-to-average real contract's bytecode size.
	codeValueLen = 4096

	// codeHashValueLen matches a real keccak256 hash's output size.
	codeHashValueLen = 32
)

// Config controls the synthetic write load.
type Config struct {
	// DataDir is the PebbleDB data directory.
	DataDir string

	// NumContracts is how many contracts exist before the run starts. Contract creation is far
	// too rare to bootstrap from a rate within a benchmark run, so the starting pool is given
	// directly; NewContractPct governs growth from there.
	NumContracts int

	// NewKeyPct is the percentage of writes that mint a brand-new key rather than write a new
	// version of a key already written. It sets both how fast the keyspace grows and, as its
	// reciprocal, the average number of versions a key accumulates. 100 never revisits a key;
	// 0 writes nothing but new versions of the first key in each space.
	NewKeyPct float64

	// NewContractPct is the percentage of brand-new storage keys that land on a contract that is
	// also brand new, the rest going to a contract already in use. 0 holds the contract pool
	// fixed at NumContracts. Its reciprocal is the average number of slots per contract.
	NewContractPct float64

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

	// Seed makes the sequence of key draws and the value bytes deterministic.
	Seed int64

	// ReadsPerSecond is the combined random-read rate across all read workers, enforced by a
	// single shared rate.Limiter. 0 disables reads entirely.
	ReadsPerSecond float64

	// ReadWorkers is how many goroutines issue random reads against the shared limiter.
	// Only meaningful when ReadsPerSecond > 0.
	ReadWorkers int

	// ReadKeyPoolCapacity bounds the reservoir of known-written keys reads sample from. See
	// readKeyPool.
	ReadKeyPoolCapacity int

	// KeepRecent is how many recent versions state-store pruning retains; older versions become
	// eligible for deletion. 0 disables pruning, matching a production node's pruning manager
	// (state_db/ss/pruning.Manager), which this reuses directly.
	KeepRecent int64

	// PruneInterval is how often the pruning manager checks for prunable versions. Only
	// meaningful when KeepRecent > 0.
	PruneInterval time.Duration
}

// DefaultConfig returns batches of 1,000 key/value writes, twice a second, against a fixed
// contract pool, with reads and pruning disabled.
func DefaultConfig() Config {
	return Config{
		NumContracts:        100,
		NewKeyPct:           1,
		NewContractPct:      0,
		BatchSize:           1000,
		BatchInterval:       500 * time.Millisecond,
		QueueDepth:          4,
		Presort:             false,
		Seed:                1,
		ReadsPerSecond:      0,
		ReadWorkers:         4,
		ReadKeyPoolCapacity: 100_000,
		KeepRecent:          0,
		PruneInterval:       30 * time.Second,
	}
}

// PebbleSim drives a PebbleDB state store with a synthetic mix of storage-slot, balance, and
// nonce writes. Key/value generation runs on a dedicated goroutine (started by Generate) that
// feeds pre-built batches through a channel to WriteBatch, so Pebble's write throughput isn't
// gated by generation cost. rng is owned exclusively by that goroutine once Generate starts;
// WriteBatch never touches it.
type PebbleSim struct {
	cfg     Config
	store   types.StateStore
	rng     *rand.Rand
	version atomic.Int64
	metrics *simMetrics
	batches chan batch

	// nextSlotID and nextAccountID count the storage keys and the accounts minted so far, which
	// is all the simulation remembers about its keyspace: every key is derived from its id (see
	// drawID). Only the generator goroutine advances them; they are atomic so WriteBatch can
	// read them for metrics.
	nextSlotID    atomic.Uint64
	nextAccountID atomic.Uint64

	// readPool and readWG back StartReaders; see their own docs.
	readPool *readKeyPool
	readWG   sync.WaitGroup

	// pruner backs StartPruning; nil when pruning is disabled.
	pruner *pruning.Manager
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
	if cfg.ReadsPerSecond > 0 && cfg.ReadWorkers < 1 {
		return nil, fmt.Errorf("read-workers must be >= 1 when reads-per-second > 0, got %d", cfg.ReadWorkers)
	}
	if cfg.NumContracts < 1 {
		return nil, fmt.Errorf("contracts must be >= 1, got %d", cfg.NumContracts)
	}
	if cfg.NewKeyPct < 0 || cfg.NewKeyPct > 100 {
		return nil, fmt.Errorf("new-key-pct must be in [0, 100], got %v", cfg.NewKeyPct)
	}
	if cfg.NewContractPct < 0 || cfg.NewContractPct > 100 {
		return nil, fmt.Errorf("new-contract-pct must be in [0, 100], got %v", cfg.NewContractPct)
	}

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = config.PebbleDBBackend
	ssConfig.KeepRecent = int(cfg.KeepRecent)
	ssConfig.PruneIntervalSeconds = int(cfg.PruneInterval.Seconds())

	// TODO: check if fsync is enabled or not.
	// TODO: check memtables size. increase it.
	// TODO: give more threads to the garbage collector. and checkl related metrics. tableGarbagePointDeletionsEstimate: tableGarbagePointDeletionsEstimate,
	// tableGarbageRangeDeletionsEstimate: tableGarbageRangeDeletionsEstimate,

	// TODO: check ec2 memory speed

	// TODO: check this https://github.com/sei-protocol/sei-chain/tree/cjl/snapshot-experiments

	ssConfig.SeparateEVMSubDBs = false

	store, err := evmss.NewEVMStateStore(cfg.DataDir, ssConfig)
	if err != nil {
		return nil, fmt.Errorf("open evm state store: %w", err)
	}

	sim := &PebbleSim{
		cfg:      cfg,
		store:    store,
		rng:      newSimRNG(cfg.Seed),
		metrics:  newSimMetrics(),
		batches:  make(chan batch, cfg.QueueDepth),
		readPool: newReadKeyPool(cfg.ReadKeyPoolCapacity, cfg.Seed+1),
	}
	sim.version.Store(store.GetLatestVersion())
	return sim, nil
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

	// One code and one codehash write per batch: pebblesim otherwise never touches these two
	// EVM store types at all, so their sub-DBs never see enough volume to cross Pebble's
	// size-triggered flush threshold on their own (see randomCodeKey/randomCodeHashKey).
	codeVal := make([]byte, codeValueLen)
	fillBytes(p.rng, codeVal)
	pairs = append(pairs, &proto.KVPair{Key: p.randomCodeKey(), Value: codeVal})
	codeHashVal := make([]byte, codeHashValueLen)
	fillBytes(p.rng, codeHashVal)
	pairs = append(pairs, &proto.KVPair{Key: p.randomCodeHashKey(), Value: codeHashVal})

	var sortElapsed time.Duration
	if p.cfg.Presort {
		sortStart := time.Now()
		//mvcc.SortChangesetPairs(pairs)
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

	version := p.version.Add(1)
	changesets := []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: b.pairs},
	}}

	writeStart := time.Now()
	if err := p.store.ApplyChangesetSync(version, changesets); err != nil {
		return BatchResult{}, fmt.Errorf("apply changeset at version %d: %w", version, err)
	}
	if err := p.store.SetLatestVersion(version); err != nil {
		return BatchResult{}, fmt.Errorf("set latest version to %d: %w", version, err)
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
	p.metrics.keysWritten.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", "code")))
	p.metrics.keysWritten.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", "codehash")))
	p.metrics.latestHeight.Record(ctx, p.store.GetLatestVersion())
	p.metrics.earliestHeight.Record(ctx, p.store.GetEarliestVersion())
	slotsMinted := p.nextSlotID.Load()
	//nolint:gosec // G115 - id counters are bounded by the number of keys written, far below int64 max
	p.metrics.distinctKeys.Record(ctx, int64(slotsMinted), metric.WithAttributes(attribute.String("kind", "slot")))
	//nolint:gosec // G115 - id counters are bounded by the number of keys written, far below int64 max
	p.metrics.distinctKeys.Record(ctx, int64(p.nextAccountID.Load()), metric.WithAttributes(attribute.String("kind", "account")))
	//nolint:gosec // G115 - id counters are bounded by the number of keys written, far below int64 max
	p.metrics.distinctKeys.Record(ctx, int64(p.contractsAt(slotsMinted)), metric.WithAttributes(attribute.String("kind", "contract")))
	if totalElapsed > p.cfg.BatchInterval {
		p.metrics.deadlineMisses.Add(ctx, 1)
	}

	// Offer a bounded random subsample of this batch to readPool, not the whole batch — b.pairs
	// can be sized in the hundreds of thousands, and the reservoir only needs a steady trickle of
	// representative samples. Done last, after every timing above is captured, so this bookkeeping
	// never counts against Total/Write or the deadline-miss check.
	p.readPool.addFromBatch(b.pairs, readPoolKeysPerBatch)

	return BatchResult{Version: version, Total: totalElapsed, Write: writeElapsed, Stall: stallElapsed, Sort: b.sortElapsed}, nil
}

// ---------------------------------------------------------------------------
// Read path
// ---------------------------------------------------------------------------

// readKeyPool is a fixed-capacity, uniform reservoir sample (Vitter's Algorithm R) of keys
// actually written to the store, so reads draw from it instead of independently re-deriving a
// key that may never have been written — the keyspace grows without bound at a high
// NewKeyPct, so a key built the same way writes are built would almost always miss.
type readKeyPool struct {
	mu   sync.RWMutex
	keys [][]byte
	seen int64      // total keys ever offered, for Algorithm R's replacement odds
	rng  *rand.Rand // private stream; only WriteBatch's goroutine calls addFromBatch, so no lock needed around it
}

func newReadKeyPool(capacity int, seed int64) *readKeyPool {
	return &readKeyPool{keys: make([][]byte, 0, capacity), rng: newSimRNG(seed)}
}

// addFromBatch offers up to `take` uniformly random keys from pairs, without touching every
// element.
func (k *readKeyPool) addFromBatch(pairs []*proto.KVPair, take int) {
	n := len(pairs)
	if n == 0 {
		return
	}
	if take > n {
		take = n
	}
	for i := 0; i < take; i++ {
		k.add(pairs[k.rng.IntN(n)].Key)
	}
}

// add offers a single key for inclusion, replacing a uniformly random existing entry once the
// pool is at capacity so every offered key has an equal long-term chance of surviving.
func (k *readKeyPool) add(key []byte) {
	k.seen++
	if len(k.keys) < cap(k.keys) {
		k.mu.Lock()
		k.keys = append(k.keys, key)
		k.mu.Unlock()
		return
	}
	if j := k.rng.Int64N(k.seen); j < int64(len(k.keys)) {
		k.mu.Lock()
		k.keys[j] = key
		k.mu.Unlock()
	}
}

// sample returns a uniformly random previously-written key, or nil if none have landed yet.
func (k *readKeyPool) sample(rng *rand.Rand) []byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if len(k.keys) == 0 {
		return nil
	}
	return k.keys[rng.IntN(len(k.keys))]
}

// keyKind labels a key by its on-disk kind for read metrics, inferred from the same prefix byte
// randomStorageKey/randomBalanceKey/randomNonceKey already encode.
func keyKind(key []byte) string {
	if len(key) == 0 {
		return "unknown"
	}
	noncePrefix, _ := keys.EVMKeyPrefixByte(keys.EVMKeyNonce)
	codePrefix, _ := keys.EVMKeyPrefixByte(keys.EVMKeyCode)
	codeHashPrefix, _ := keys.EVMKeyPrefixByte(keys.EVMKeyCodeHash)
	switch key[0] {
	case keys.StateKeyPrefix()[0]:
		return "slot"
	case noncePrefix:
		return "nonce"
	case byte(evmss.StoreBalance):
		return "balance"
	case codePrefix:
		return "code"
	case codeHashPrefix:
		return "codehash"
	default:
		return "unknown"
	}
}

// StartReaders starts cfg.ReadWorkers goroutines issuing random reads against the store,
// combined at cfg.ReadsPerSecond via one shared rate.Limiter. Each worker owns an independent
// RNG stream used only to pick an index into readPool — it never touches p.rng, which Generate's
// goroutine owns exclusively. Every read samples a key from readPool (guaranteed to have been
// actually written at some point) and queries it at whatever version p.Version() currently
// holds. A nil sample (the pool hasn't received its first batch yet) is skipped, not treated as
// an error. No-op if cfg.ReadsPerSecond <= 0. Runs until ctx is done; Close waits for every
// worker to exit before closing the store.
func (p *PebbleSim) StartReaders(ctx context.Context) {
	if p.cfg.ReadsPerSecond <= 0 {
		return
	}

	limiter := rate.NewLimiter(rate.Limit(p.cfg.ReadsPerSecond), 1)
	p.readWG.Add(p.cfg.ReadWorkers)
	for i := 0; i < p.cfg.ReadWorkers; i++ {
		rng := newSimRNG(p.cfg.Seed + 2 + int64(i))
		go func() {
			defer p.readWG.Done()
			for {
				if err := limiter.Wait(ctx); err != nil {
					return
				}
				p.doRead(ctx, rng)
			}
		}()
	}
}

// doRead issues one random read sampled from readPool, recording pebblesim_read_duration_seconds
// (labeled by kind and hit/miss) or pebblesim_read_errors_total on failure. It does not record
// pebble_get_latency directly — that histogram is already emitted by the underlying pebbledb
// wrapper on every Get, split by physical sub-DB; this one adds the hit/miss split that layer
// can't give.
func (p *PebbleSim) doRead(ctx context.Context, rng *rand.Rand) {
	key := p.readPool.sample(rng)
	if key == nil {
		return
	}
	kind := keyKind(key)

	start := time.Now()
	val, err := p.store.Get(keys.EVMStoreKey, p.Version(), key)
	elapsed := time.Since(start)
	if err != nil {
		p.metrics.readErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", kind)))
		return
	}

	p.metrics.readDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.Bool("hit", val != nil),
	))
}

// ---------------------------------------------------------------------------
// Key generation
//
// Every key is named by an integer id and derived from it, so the simulation holds no key
// material at all: it remembers only how many ids it has minted, and revisiting a key is
// redrawing its id. That is what makes the new-versus-new-version split an explicit
// percentage (cfg.NewKeyPct) rather than a side effect of how wide some pool happens to be.
// ---------------------------------------------------------------------------

// drawID returns the id of the key to write next: a brand-new one with probability
// cfg.NewKeyPct, otherwise one drawn uniformly from the ids already minted. The first draw
// always mints, since nothing exists yet to revisit.
func (p *PebbleSim) drawID(next *atomic.Uint64) uint64 {
	n := next.Load()
	if n == 0 || p.rng.Float64()*100 < p.cfg.NewKeyPct {
		next.Store(n + 1)
		return n
	}
	return p.rng.Uint64N(n)
}

// randomStorageKey builds a real EVM storage-slot key (0x03 || address || slot).
func (p *PebbleSim) randomStorageKey() []byte {
	id := p.drawID(&p.nextSlotID)
	key := make([]byte, 0, len(keys.StateKeyPrefix())+keys.AddressLen+slotLen)
	key = append(key, keys.StateKeyPrefix()...)
	key = append(key, contractAddress(p.contractForSlot(id))...)
	return append(key, slotForID(id)...)
}

// contractsAt returns how many contracts exist by the time slot id is minted: the initial pool
// plus one for every 100/NewContractPct slots minted before it. Contracts are minted on a fixed
// schedule rather than by a coin flip so the count depends only on id, which is what lets a slot
// keep naming the same contract however far the pool has grown since.
func (p *PebbleSim) contractsAt(id uint64) uint64 {
	//nolint:gosec // G115 - Open rejects a NumContracts below 1, and NewContractPct is in [0, 100]
	return uint64(p.cfg.NumContracts) + uint64(float64(id)*p.cfg.NewContractPct/100)
}

// contractForSlot returns the id of the contract slot id lives on: a brand-new contract when
// this slot is the one that mints it, otherwise one already in use, drawn uniformly. A contract
// therefore never exists without at least one slot, and older contracts accumulate more slots
// than younger ones.
func (p *PebbleSim) contractForSlot(id uint64) uint64 {
	minted := p.contractsAt(id)
	if p.contractsAt(id+1) > minted {
		return minted
	}
	return splitmix64(id^contractPickSalt) % minted
}

// slotForID derives the 32-byte storage slot named by id, half array-style (a small, zero-padded
// index, like a simple Solidity variable) and half mapping-style (32 bytes of full entropy, like
// a keccak256-derived mapping entry). Real EVM storage is a mix of both; using only one style
// would make this benchmark's data compress unrealistically well or poorly.
func slotForID(id uint64) []byte {
	slot := make([]byte, slotLen)
	if splitmix64(id^slotStyleSalt)&1 == 0 {
		binary.BigEndian.PutUint64(slot[slotLen-8:], id)
		return slot
	}
	expand(slot, id, slotSalt)
	return slot
}

// contractAddress derives the 20-byte address of contract id.
func contractAddress(id uint64) []byte {
	return addressForID(id, contractAddrSalt)
}

// accountAddress derives the 20-byte address of account id.
func accountAddress(id uint64) []byte {
	return addressForID(id, accountAddrSalt)
}

func addressForID(id, salt uint64) []byte {
	addr := make([]byte, keys.AddressLen)
	expand(addr, id, salt)
	return addr
}

// drawAccount returns the address of the account to write a balance or nonce for: a brand-new
// account with probability cfg.NewKeyPct, otherwise one already written.
func (p *PebbleSim) drawAccount() []byte {
	return accountAddress(p.drawID(&p.nextAccountID))
}

// drawContract returns the address of a contract already in use, drawn uniformly. Code and
// codehash writes address contracts rather than accounts, and don't mint: at one write per batch
// they would take the contract pool nowhere, and contract creation is already driven by
// cfg.NewContractPct on the storage side.
func (p *PebbleSim) drawContract() []byte {
	return contractAddress(p.rng.Uint64N(p.contractsAt(p.nextSlotID.Load())))
}

// randomNonceKey builds a real EVM nonce key (0x0a || address).
func (p *PebbleSim) randomNonceKey() []byte {
	return keys.BuildEVMKey(keys.EVMKeyNonce, p.drawAccount())
}

// randomCodeKey builds a real EVM contract-code key (0x07 || address).
func (p *PebbleSim) randomCodeKey() []byte {
	return keys.BuildEVMKey(keys.EVMKeyCode, p.drawContract())
}

// randomCodeHashKey builds a real EVM code-hash key (0x08 || address).
func (p *PebbleSim) randomCodeHashKey() []byte {
	return keys.BuildEVMKey(keys.EVMKeyCodeHash, p.drawContract())
}

// randomBalanceKey builds a synthetic balance key (evmss.StoreBalance || address). Balances have
// no real on-disk key format yet — evmss.StoreBalance is the sub-DB type the codebase already
// reserves for them ("reserved for future migration"; they currently live in the tendermint/IAVL
// store, not this SS layer), so reusing it here keeps this benchmark's placeholder in sync with
// that reservation instead of picking an independent value.
func (p *PebbleSim) randomBalanceKey() []byte {
	key := make([]byte, 0, 1+keys.AddressLen)
	key = append(key, byte(evmss.StoreBalance))
	return append(key, p.drawAccount()...)
}

// Version returns the most recently written version.
func (p *PebbleSim) Version() int64 {
	return p.version.Load()
}

// StartPruning starts the same background pruning manager a production node runs
// (state_db/ss/pruning.Manager): every cfg.PruneInterval it prunes everything older than
// cfg.KeepRecent versions behind the store's latest. No-op if cfg.KeepRecent <= 0 or
// cfg.PruneInterval <= 0.
func (p *PebbleSim) StartPruning() {
	if p.cfg.KeepRecent <= 0 || p.cfg.PruneInterval <= 0 {
		return
	}
	p.pruner = pruning.NewPruningManager(p.store, p.cfg.KeepRecent, int64(p.cfg.PruneInterval.Seconds()))
	p.pruner.Start()
}

// Close stops the pruning manager (if running), waits for every StartReaders worker to exit,
// then releases the underlying PebbleDB store. Waiting on readers first guarantees none of them
// ever calls Get on a closed store.
func (p *PebbleSim) Close() error {
	if p.pruner != nil {
		p.pruner.Stop()
	}
	p.readWG.Wait()
	return p.store.Close()
}

// Compact forces a full compaction of the store, so its on-disk size reflects steady state
// rather than whatever background compaction happened to leave after live writes. A no-op
// if the store doesn't support types.Compactable.
/*
func (p *PebbleSim) Compact() error {
	c, ok := p.store.(types.Compactable)
	if !ok {
		return nil
	}
	return c.Compact()
}
*/

func newSimRNG(seed int64) *rand.Rand {
	s := uint64(seed) //nolint:gosec // G115 - benchmark seed, wrap is fine
	return rand.New(rand.NewPCG(s, s^rngStreamMix))
}

// splitmix64 is the SplittableRandom finalizer: a bijection that scatters an id across the full
// 64-bit range, so consecutive ids derive unrelated keys.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// expand fills dst with a deterministic byte stream derived from id and salt.
func expand(dst []byte, id, salt uint64) {
	x := id ^ salt
	for i := 0; i < len(dst); {
		x = splitmix64(x)
		v := x
		for n := 0; n < 8 && i < len(dst); n++ {
			dst[i] = byte(v)
			v >>= 8
			i++
		}
	}
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
