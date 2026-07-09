package cryptosim

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	dbTypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// SSReadSimulator issues plain point reads (StateStore.Get) against the SS
// backend while the write workload runs, mirroring the receipt-store read
// workers. Two tiers:
//
//   - hot:  a recently written key, read at the latest version (recent data)
//   - cold: a uniformly sampled historical (version, key) pair at least
//     SSColdReadMinAgeBlocks old, read at its original version (old data)
//
// Workers are unthrottled: achieved reads/s for a given worker count is the
// measurement, like the receipt-store benchmark tables.
type SSReadSimulator struct {
	ctx      context.Context
	store    dbTypes.StateStore
	ring     *ssKeyRing
	res      *ssKeyReservoir
	minAge   int64
	hot      *ssReadTierStats
	cold     *ssReadTierStats
	workerWg sync.WaitGroup

	latency metric.Float64Histogram
	reads   metric.Int64Counter
}

// underlyingStateStore is implemented by wrappers exposing their raw SS store.
type underlyingStateStore interface {
	StateStore() dbTypes.StateStore
}

const (
	ssReadRingSize          = 1 << 20
	ssReadReservoirSize     = 1 << 21
	ssReadSampleStride      = 4 // offer every 4th pair to the cold reservoir
	ssWorkerSampleCap       = 20_000
	ssReadSummaryInterval   = 60 * time.Second
	ssColdRetryLimit        = 16
	ssColdNoEligibleBackoff = 250 * time.Millisecond
)

var ssPointReadLatencyBuckets = []float64{
	0.00001, 0.000025, 0.00005, 0.0001, 0.00025, 0.0005,
	0.001, 0.0025, 0.005, 0.01, 0.025,
	0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// NewSSReadSimulator wires point-read workers against db's underlying state
// store. Returns nil (no-op) when no workers are configured.
func NewSSReadSimulator(ctx context.Context, config *CryptoSimConfig, db wrappers.DBWrapper) (*SSReadSimulator, error) {
	if config.SSPointReadWorkers == 0 && config.SSColdPointReadWorkers == 0 {
		return nil, nil
	}
	provider, ok := db.(underlyingStateStore)
	if !ok {
		return nil, fmt.Errorf("SS point-read workers require an SS-backed backend (got %s)", config.Backend)
	}

	meter := otel.Meter("cryptosim")
	latency, _ := meter.Float64Histogram(
		"cryptosim_ss_point_read_latency_seconds",
		metric.WithDescription("SS point read (Get) latency by tier (hot=recent key at latest version, cold=old version)"),
		metric.WithExplicitBucketBoundaries(ssPointReadLatencyBuckets...),
		metric.WithUnit("s"),
	)
	reads, _ := meter.Int64Counter(
		"cryptosim_ss_point_reads_total",
		metric.WithDescription("SS point reads issued, by tier and outcome"),
		metric.WithUnit("{count}"),
	)

	s := &SSReadSimulator{
		ctx:     ctx,
		store:   provider.StateStore(),
		ring:    newSSKeyRing(ssReadRingSize),
		res:     newSSKeyReservoir(ssReadReservoirSize, config.Seed),
		minAge:  config.SSColdReadMinAgeBlocks,
		hot:     newSSReadTierStats("hot"),
		cold:    newSSReadTierStats("cold"),
		latency: latency,
		reads:   reads,
	}

	for i := 0; i < config.SSPointReadWorkers; i++ {
		s.workerWg.Add(1)
		go s.hotWorker(rand.New(rand.NewSource(config.Seed + int64(i)))) //nolint:gosec // benchmark jitter, not crypto
	}
	for i := 0; i < config.SSColdPointReadWorkers; i++ {
		s.workerWg.Add(1)
		go s.coldWorker(rand.New(rand.NewSource(config.Seed + 1000 + int64(i)))) //nolint:gosec // benchmark jitter, not crypto
	}
	go s.summaryLoop()

	fmt.Printf("Started SS point-read workers: hot=%d cold=%d (cold min age %d blocks)\n",
		config.SSPointReadWorkers, config.SSColdPointReadWorkers, config.SSColdReadMinAgeBlocks)
	return s, nil
}

// Sample feeds the key trackers from a finalized block's changesets. Called
// from the single FinalizeBlock thread; takes the reservoir lock once per block.
func (s *SSReadSimulator) Sample(version int64, changesets []*proto.NamedChangeSet) {
	if s == nil {
		return
	}
	s.res.mu.Lock()
	for _, cs := range changesets {
		for i, pair := range cs.Changeset.Pairs {
			if pair.Delete {
				continue
			}
			key := append([]byte(nil), pair.Key...)
			s.ring.push(ssKeyEntry{version: version, key: key})
			if i%ssReadSampleStride == 0 {
				s.res.offerLocked(ssKeyEntry{version: version, key: key})
			}
		}
	}
	s.res.mu.Unlock()
}

func (s *SSReadSimulator) hotWorker(rng *rand.Rand) {
	defer s.workerWg.Done()
	for s.ctx.Err() == nil {
		entry, ok := s.ring.random(rng)
		if !ok {
			time.Sleep(ssColdNoEligibleBackoff)
			continue
		}
		version := s.store.GetLatestVersion()
		if version <= 0 {
			time.Sleep(ssColdNoEligibleBackoff)
			continue
		}
		s.read(s.hot, version, entry.key)
	}
}

func (s *SSReadSimulator) coldWorker(rng *rand.Rand) {
	defer s.workerWg.Done()
	for s.ctx.Err() == nil {
		latest := s.store.GetLatestVersion()
		entry, ok := s.res.randomOlderThan(rng, latest-s.minAge)
		if !ok {
			time.Sleep(ssColdNoEligibleBackoff)
			continue
		}
		s.read(s.cold, entry.version, entry.key)
	}
}

func (s *SSReadSimulator) read(tier *ssReadTierStats, version int64, key []byte) {
	start := time.Now()
	value, err := s.store.Get(wrappers.EVMStoreName, version, key)
	elapsed := time.Since(start)

	outcome := "found"
	switch {
	case err != nil:
		outcome = "error"
	case value == nil:
		outcome = "not_found"
	}
	s.latency.Record(s.ctx, elapsed.Seconds(), metric.WithAttributes(attribute.String("tier", tier.name)))
	s.reads.Add(s.ctx, 1, metric.WithAttributes(
		attribute.String("tier", tier.name),
		attribute.String("outcome", outcome),
	))
	tier.record(elapsed, outcome)
}

func (s *SSReadSimulator) summaryLoop() {
	ticker := time.NewTicker(ssReadSummaryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			// Give workers a moment to finish in-flight reads, then print totals.
			s.workerWg.Wait()
			s.printSummary("FINAL")
			return
		case <-ticker.C:
			s.printSummary("interval")
		}
	}
}

func (s *SSReadSimulator) printSummary(label string) {
	for _, tier := range []*ssReadTierStats{s.hot, s.cold} {
		line := tier.summary()
		if line != "" {
			fmt.Printf("[ss-read %s] %s\n", label, line)
		}
	}
}

type ssKeyEntry struct {
	version int64
	key     []byte
}

// ssKeyRing holds the most recently written keys. Push is single-writer
// (FinalizeBlock); reads are lock-free via per-slot atomic pointers.
type ssKeyRing struct {
	slots []atomic.Pointer[ssKeyEntry]
	pos   atomic.Int64
}

func newSSKeyRing(size int) *ssKeyRing {
	return &ssKeyRing{slots: make([]atomic.Pointer[ssKeyEntry], size)}
}

func (r *ssKeyRing) push(e ssKeyEntry) {
	idx := r.pos.Add(1) - 1
	r.slots[idx%int64(len(r.slots))].Store(&e)
}

func (r *ssKeyRing) random(rng *rand.Rand) (ssKeyEntry, bool) {
	filled := r.pos.Load()
	if filled == 0 {
		return ssKeyEntry{}, false
	}
	if filled > int64(len(r.slots)) {
		filled = int64(len(r.slots))
	}
	e := r.slots[rng.Int63n(filled)].Load()
	if e == nil {
		return ssKeyEntry{}, false
	}
	return *e, true
}

// ssKeyReservoir is a uniform reservoir sample over every offered pair,
// giving cold readers (version, key) points spanning the whole run history.
type ssKeyReservoir struct {
	mu      sync.RWMutex
	entries []ssKeyEntry
	seen    int64
	rng     *rand.Rand
	cap     int
}

func newSSKeyReservoir(capacity int, seed int64) *ssKeyReservoir {
	return &ssKeyReservoir{
		entries: make([]ssKeyEntry, 0, capacity),
		rng:     rand.New(rand.NewSource(seed + 7777)), //nolint:gosec // benchmark sampling, not crypto
		cap:     capacity,
	}
}

// offerLocked implements standard reservoir sampling; caller holds mu.
func (r *ssKeyReservoir) offerLocked(e ssKeyEntry) {
	r.seen++
	if len(r.entries) < r.cap {
		r.entries = append(r.entries, e)
		return
	}
	if j := r.rng.Int63n(r.seen); j < int64(r.cap) {
		r.entries[j] = e
	}
}

// randomOlderThan returns a sampled entry with version <= maxVersion.
func (r *ssKeyReservoir) randomOlderThan(rng *rand.Rand, maxVersion int64) (ssKeyEntry, bool) {
	if maxVersion <= 0 {
		return ssKeyEntry{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := len(r.entries)
	if n == 0 {
		return ssKeyEntry{}, false
	}
	for attempt := 0; attempt < ssColdRetryLimit; attempt++ {
		e := r.entries[rng.Intn(n)]
		if e.version <= maxVersion {
			return e, true
		}
	}
	return ssKeyEntry{}, false
}

// ssReadTierStats accumulates counts plus a bounded uniform latency sample per
// tier for exact percentile reporting.
type ssReadTierStats struct {
	name       string
	count      atomic.Int64
	notFound   atomic.Int64
	errors     atomic.Int64
	totalNanos atomic.Int64

	mu      sync.Mutex
	sample  []float64 // seconds
	seen    int64
	rng     *rand.Rand
	lastCnt int64
	lastAt  time.Time
}

func newSSReadTierStats(name string) *ssReadTierStats {
	return &ssReadTierStats{
		name:   name,
		sample: make([]float64, 0, ssWorkerSampleCap),
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // benchmark sampling, not crypto
		lastAt: time.Now(),
	}
}

func (t *ssReadTierStats) record(elapsed time.Duration, outcome string) {
	t.count.Add(1)
	t.totalNanos.Add(elapsed.Nanoseconds())
	switch outcome {
	case "not_found":
		t.notFound.Add(1)
	case "error":
		t.errors.Add(1)
	}
	t.mu.Lock()
	t.seen++
	if len(t.sample) < ssWorkerSampleCap {
		t.sample = append(t.sample, elapsed.Seconds())
	} else if j := t.rng.Int63n(t.seen); j < int64(ssWorkerSampleCap) {
		t.sample[j] = elapsed.Seconds()
	}
	t.mu.Unlock()
}

func (t *ssReadTierStats) summary() string {
	count := t.count.Load()
	if count == 0 {
		return ""
	}
	t.mu.Lock()
	sample := append([]float64(nil), t.sample...)
	t.mu.Unlock()
	sort.Float64s(sample)

	now := time.Now()
	t.mu.Lock()
	rate := float64(count-t.lastCnt) / now.Sub(t.lastAt).Seconds()
	t.lastCnt = count
	t.lastAt = now
	t.mu.Unlock()

	avg := time.Duration(t.totalNanos.Load() / count)
	return fmt.Sprintf("%s: reads=%d rate=%.0f/s avg=%s p50=%s p99=%s not_found=%d errors=%d",
		t.name, count, rate, avg.Round(time.Microsecond),
		ssQuantile(sample, 0.50).Round(time.Microsecond),
		ssQuantile(sample, 0.99).Round(time.Microsecond),
		t.notFound.Load(), t.errors.Load())
}

func ssQuantile(sorted []float64, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q * float64(len(sorted)-1))
	return time.Duration(sorted[idx] * float64(time.Second))
}
