package verifier

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "composite", "verifier")

// BackendAccessor is the minimal interface the verifier needs from its host
// CompositeCommitStore. Kept as an interface so the verifier package does not
// import composite (and composite can inject itself without a cycle).
type BackendAccessor interface {
	// CosmosCommitter returns the live memiavl committer. Must not be nil
	// when the verifier is constructed.
	CosmosCommitter() *memiavl.CommitStore

	// FlatKVStore returns the live FlatKV store, or nil if FlatKV is not
	// enabled in the current write mode. When nil, all oracles are no-ops.
	FlatKVStore() flatkv.Store

	// LoadReadOnlyAt opens a read-only view of both backends at the given
	// version. Caller must Close() the returned accessor. Returns nil
	// accessor + error on failure. Used by Oracle 4.
	LoadReadOnlyAt(version int64) (BackendAccessor, func() error, error)
}

// Verifier runs the configured oracles against a composite store. Cheap to
// construct when Config is disabled (AnyEnabled == false): a disabled
// Verifier accepts all hook calls as no-ops.
type Verifier struct {
	cfg         Config
	accessor    BackendAccessor
	instruments *instrumentation

	// Guards the scan scheduler. OnCommit is called on the ABCI hot path,
	// so we must not block it; background work runs on a worker goroutine.
	mu      sync.Mutex
	running bool

	// Cadence tracking. Atomic so we can read without holding mu during
	// hot-path OnCommit calls.
	lastScan atomic.Int64
	lastLt   atomic.Int64
	lastHist atomic.Int64

	// Time of the most recent memiavl Commit finishing, used to compute
	// dual_write lag when flatkv Commit finishes.
	cosmosCommitAt atomic.Int64

	// Worker lifecycle.
	work     chan asyncJob
	stopOnce sync.Once
	done     chan struct{}
}

// asyncJob is one unit of background work submitted from the hot path.
type asyncJob struct {
	kind    asyncKind
	version int64
}

type asyncKind int

const (
	jobScan asyncKind = iota + 1
	jobLtHash
	jobHistorical
)

// New constructs a Verifier for the given backend accessor. Returns a
// non-nil Verifier even when cfg disables all oracles so callers can keep
// hook calls unconditional; hooks on a disabled verifier are cheap no-ops.
func New(cfg Config, accessor BackendAccessor) *Verifier {
	v := &Verifier{
		cfg:         cfg,
		accessor:    accessor,
		instruments: newInstrumentation(),
		work:        make(chan asyncJob, 16),
		done:        make(chan struct{}),
	}
	if cfg.AnyEnabled() && accessor != nil {
		go v.workerLoop()
		logger.Info("flatkv verifier enabled",
			"write", cfg.WriteEnabled,
			"write_panic", cfg.WritePanic,
			"scan_interval", cfg.ScanInterval,
			"lthash_interval", cfg.LtHashInterval,
			"hist_interval", cfg.HistInterval,
			"hist_lag", cfg.HistLag,
			"sample_limit", cfg.SampleLimit,
		)
	}
	return v
}

// Close stops the worker goroutine. Safe to call multiple times.
func (v *Verifier) Close() {
	if v == nil {
		return
	}
	v.stopOnce.Do(func() {
		close(v.work)
		<-v.done
	})
}

// OnApplyChangeSets is the Oracle 1 hook. Called from composite.ApplyChangeSets
// *after* both backends have applied the changeset but *before* Commit. The
// verifier reads back every EVM key from both backends and records any
// byte-level divergence.
//
// Must not panic unless cfg.WritePanic is set. Cheap when WriteEnabled is false.
func (v *Verifier) OnApplyChangeSets(changesets []*proto.NamedChangeSet) {
	if v == nil || !v.cfg.WriteEnabled || v.accessor == nil {
		return
	}
	cosmos := v.accessor.CosmosCommitter()
	flat := v.accessor.FlatKVStore()
	if cosmos == nil || flat == nil {
		return
	}
	evmStore := cosmos.GetChildStoreByName(keys.EVMStoreKey)
	if evmStore == nil {
		return
	}

	ctx := context.Background()
	mismatches := VerifyWrites(ctx, evmStore, flat, changesets)
	if len(mismatches) == 0 {
		return
	}

	byReason := map[string]int64{}
	for _, m := range mismatches {
		reason := "value_mismatch"
		if m.FromDelete {
			reason = "delete_mismatch"
		} else if m.Expected == nil {
			reason = "memiavl_missing"
		} else if m.Actual == nil {
			reason = "flatkv_missing"
		}
		byReason[reason]++
		logger.Error("flatkv verifier: write-time mismatch",
			"reason", reason,
			"detail", m.String(),
		)
	}
	for reason, n := range byReason {
		v.instruments.recordWriteMismatch(ctx, reason, "evm", n)
	}
	if v.cfg.WritePanic {
		panic(fmt.Sprintf(
			"flatkv verifier: %d write-time mismatches (first=%s)",
			len(mismatches), mismatches[0].String(),
		))
	}
}

// NoteCosmosCommit marks the wall-clock time memiavl.Commit returned so that
// NoteFlatKVCommit can record the dual-write lag. Both calls are optional;
// if either is skipped the lag metric simply is not emitted for that commit.
func (v *Verifier) NoteCosmosCommit() {
	if v == nil || v.instruments == nil {
		return
	}
	v.cosmosCommitAt.Store(time.Now().UnixNano())
}

// NoteFlatKVCommit records the gap between memiavl.Commit and the supplied
// flatkv.Commit timestamp as the dual_write_lag histogram.
func (v *Verifier) NoteFlatKVCommit() {
	if v == nil || v.instruments == nil {
		return
	}
	prev := v.cosmosCommitAt.Swap(0)
	if prev == 0 {
		return
	}
	delta := time.Now().UnixNano() - prev
	if delta < 0 {
		return
	}
	v.instruments.recordDualWriteLag(context.Background(), float64(delta)/float64(time.Second))
}

// OnCommit is the periodic-scheduler hook. Called with the version that was
// just committed. Dispatches async scan / lthash / historical jobs as
// configured. Non-blocking; if the worker channel is full the job is dropped
// and a metric logged.
func (v *Verifier) OnCommit(committedVersion int64) {
	if v == nil || v.accessor == nil {
		return
	}

	if n := v.cfg.ScanInterval; n > 0 {
		if committedVersion-v.lastScan.Load() >= n {
			v.lastScan.Store(committedVersion)
			v.submit(asyncJob{kind: jobScan, version: committedVersion})
		}
	}
	if n := v.cfg.LtHashInterval; n > 0 {
		if committedVersion-v.lastLt.Load() >= n {
			v.lastLt.Store(committedVersion)
			v.submit(asyncJob{kind: jobLtHash, version: committedVersion})
		}
	}
	if n := v.cfg.HistInterval; n > 0 {
		if committedVersion-v.lastHist.Load() >= n {
			target := committedVersion - v.cfg.HistLag
			if target > 0 {
				v.lastHist.Store(committedVersion)
				v.submit(asyncJob{kind: jobHistorical, version: target})
			}
		}
	}
}

func (v *Verifier) submit(job asyncJob) {
	select {
	case v.work <- job:
	default:
		logger.Warn("flatkv verifier: worker queue full, dropping job",
			"kind", job.kind, "version", job.version)
	}
}

func (v *Verifier) workerLoop() {
	defer close(v.done)
	for job := range v.work {
		switch job.kind {
		case jobScan:
			v.runScan(job.version)
		case jobLtHash:
			v.runLtHash(job.version)
		case jobHistorical:
			v.runHistorical(job.version)
		}
	}
}

func (v *Verifier) runScan(version int64) {
	ctx := context.Background()
	cosmos := v.accessor.CosmosCommitter()
	flat := v.accessor.FlatKVStore()
	if cosmos == nil || flat == nil {
		return
	}
	evmStore := cosmos.GetChildStoreByName(keys.EVMStoreKey)
	if evmStore == nil {
		return
	}

	start := time.Now()
	res, err := ForwardSubsetLatest(ctx, evmStore, flat, version, v.cfg.SampleLimit)
	duration := time.Since(start).Seconds()
	v.instruments.recordScan(ctx, "scan", res.RowsExamined, duration)
	if err != nil {
		logger.Error("flatkv verifier: scan failed", "version", version, "err", err)
		return
	}
	v.reportScanMismatches(ctx, "scan", version, res.Mismatches)
	logger.Info("flatkv verifier: scan complete",
		"version", version,
		"rows", res.RowsExamined,
		"mismatches", len(res.Mismatches),
		"duration_s", duration,
	)
}

func (v *Verifier) runLtHash(version int64) {
	ctx := context.Background()
	flat := v.accessor.FlatKVStore()
	if flat == nil {
		return
	}

	// flatkv.VerifyLtHash rejects a store with uncommitted writes. The
	// OnCommit hook runs after the composite Commit returned, which means
	// flatkv has committed too — but a block writing to flatkv could land
	// in parallel with this goroutine. Open a readonly snapshot at the
	// target version to avoid racing the main thread.
	ro, err := flat.LoadVersion(version, true)
	if err != nil {
		logger.Error("flatkv verifier: lthash load readonly failed",
			"version", version, "err", err)
		return
	}
	defer func() { _ = ro.Close() }()

	start := time.Now()
	err = VerifyLtHashSelf(ctx, ro)
	duration := time.Since(start).Seconds()
	v.instruments.recordScan(ctx, "lthash", 0, duration)
	if err != nil {
		v.instruments.recordLtHashMismatch(ctx, 1)
		logger.Error("flatkv verifier: lthash mismatch",
			"version", version, "err", err)
		return
	}
	logger.Info("flatkv verifier: lthash verified",
		"version", version, "duration_s", duration)
}

func (v *Verifier) runHistorical(version int64) {
	if v.accessor == nil {
		return
	}
	ctx := context.Background()
	roAccessor, closer, err := v.accessor.LoadReadOnlyAt(version)
	if err != nil {
		logger.Error("flatkv verifier: historical open failed",
			"version", version, "err", err)
		return
	}
	defer func() {
		if closer != nil {
			_ = closer()
		}
	}()

	cosmos := roAccessor.CosmosCommitter()
	flat := roAccessor.FlatKVStore()
	if cosmos == nil || flat == nil {
		logger.Info("flatkv verifier: historical skipped (missing backend)",
			"version", version)
		return
	}
	evmStore := cosmos.GetChildStoreByName(keys.EVMStoreKey)
	if evmStore == nil {
		return
	}

	start := time.Now()
	res, err := ForwardSubsetLatest(ctx, evmStore, flat, version, v.cfg.SampleLimit)
	duration := time.Since(start).Seconds()
	v.instruments.recordScan(ctx, "historical", res.RowsExamined, duration)
	if err != nil {
		logger.Error("flatkv verifier: historical scan failed",
			"version", version, "err", err)
		return
	}
	v.reportHistoricalMismatches(ctx, version, res.Mismatches)
	logger.Info("flatkv verifier: historical scan complete",
		"version", version,
		"rows", res.RowsExamined,
		"mismatches", len(res.Mismatches),
		"duration_s", duration,
	)
}

func (v *Verifier) reportScanMismatches(ctx context.Context, oracle string, version int64, ms []ScanMismatch) {
	if len(ms) == 0 {
		return
	}
	byKind := map[string]int64{}
	for _, m := range ms {
		label := scanKindLabel(m.Kind)
		byKind[label]++
		logger.Error("flatkv verifier: scan mismatch",
			"oracle", oracle,
			"version", version,
			"kind", label,
			"key", fmt.Sprintf("%x", m.Key),
			"flat", fmt.Sprintf("%x", m.Flat),
			"memiavl", fmt.Sprintf("%x", m.MemVal),
		)
	}
	for label, n := range byKind {
		v.instruments.recordScanMismatch(ctx, label, n)
	}
}

func (v *Verifier) reportHistoricalMismatches(ctx context.Context, version int64, ms []ScanMismatch) {
	if len(ms) == 0 {
		return
	}
	byKind := map[string]int64{}
	for _, m := range ms {
		label := scanKindLabel(m.Kind)
		byKind[label]++
		logger.Error("flatkv verifier: historical mismatch",
			"version", version,
			"kind", label,
			"key", fmt.Sprintf("%x", m.Key),
			"flat", fmt.Sprintf("%x", m.Flat),
			"memiavl", fmt.Sprintf("%x", m.MemVal),
		)
	}
	for label, n := range byKind {
		v.instruments.recordHistoricalMismatch(ctx, label, n)
	}
}

func scanKindLabel(k ScanMismatchKind) string {
	switch k {
	case MismatchValue:
		return "value_mismatch"
	case MismatchMemiavlMissing:
		return "memiavl_missing"
	case MismatchDecode:
		return "decode_error"
	default:
		return "unknown"
	}
}
