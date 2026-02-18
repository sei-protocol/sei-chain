package evmrpc

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EVMTraceDiagnosticsEnvVar enables verbose trace latency logs.
// Set to "true" or "1" to emit per-request diagnostics for debug trace endpoints.
const EVMTraceDiagnosticsEnvVar = "EVM_TRACE_DIAGNOSTICS"

type traceDiagnosticsCtxKeyType string

const traceDiagnosticsCtxKey traceDiagnosticsCtxKeyType = "trace_diagnostics"

var traceDiagnosticsCounter atomic.Uint64

type traceBlockByNumberStats struct {
	BlockNumber           int64
	FetchBlockDur         time.Duration
	FetchBlockResultsDur  time.Duration
	FilterTransactionsDur time.Duration
	BuildMetadataDur      time.Duration
	TotalDur              time.Duration
	TxResults             int
	EVMTxCount            int
	SkippedPrioritized    int
	SkippedAssociate      int
	DecodeErrors          int
	ReceiptLookupErrors   int
	RunnableNonEVM        int
}

type traceReplayStats struct {
	UptoTxIndex     int
	ReplayedTxCount int
	SkippedPriority int
	InitDur         time.Duration
	LoopDur         time.Duration
	DeliverTxDur    time.Duration
	TotalDur        time.Duration
	Error           string
}

type traceStateAtTxStats struct {
	TxIndex      int
	TxHash       string
	ReplayDur    time.Duration
	VMContextDur time.Duration
	DecodeTxDur  time.Duration
	TotalDur     time.Duration
	Error        string
}

type tracePrepareTxStats struct {
	TxHash       string
	AnteDur      time.Duration
	TotalDur     time.Duration
	HasSignature bool
	Error        string
}

type traceDiagnostics struct {
	id        uint64
	op        string
	blockRef  string
	tracer    string
	startTime time.Time

	mu             sync.Mutex
	blockStats     []traceBlockByNumberStats
	replayStats    []traceReplayStats
	stateAtTxStats map[int]traceStateAtTxStats
	prepareTxStats map[string]tracePrepareTxStats
}

func IsTraceDiagnosticsEnabled() bool {
	val := os.Getenv(EVMTraceDiagnosticsEnvVar)
	return strings.ToLower(val) == "true" || val == "1"
}

func startTraceDiagnostics(ctx context.Context, op, blockRef, tracer string) (context.Context, *traceDiagnostics) {
	diag := &traceDiagnostics{
		id:             traceDiagnosticsCounter.Add(1),
		op:             op,
		blockRef:       blockRef,
		tracer:         tracer,
		startTime:      time.Now(),
		stateAtTxStats: map[int]traceStateAtTxStats{},
		prepareTxStats: map[string]tracePrepareTxStats{},
	}
	traceDiagPrintf(
		"start req=%d op=%s block=%s tracer=%s",
		diag.id, diag.op, diag.blockRef, diag.tracer,
	)
	return context.WithValue(ctx, traceDiagnosticsCtxKey, diag), diag
}

func traceDiagnosticsFromContext(ctx context.Context) *traceDiagnostics {
	if ctx == nil {
		return nil
	}
	diag, _ := ctx.Value(traceDiagnosticsCtxKey).(*traceDiagnostics)
	return diag
}

func (d *traceDiagnostics) RecordBlockStats(stats traceBlockByNumberStats) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.blockStats = append(d.blockStats, stats)
}

func (d *traceDiagnostics) RecordReplayStats(stats traceReplayStats) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.replayStats = append(d.replayStats, stats)
}

func (d *traceDiagnostics) RecordStateAtTxStats(stats traceStateAtTxStats) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stateAtTxStats[stats.TxIndex] = stats
}

func (d *traceDiagnostics) RecordPrepareTxStats(stats tracePrepareTxStats) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prepareTxStats[strings.ToLower(stats.TxHash)] = stats
}

func (d *traceDiagnostics) Finish(resultCount int, err error) {
	if d == nil {
		return
	}

	total := time.Since(d.startTime)
	d.mu.Lock()
	blockStats := append([]traceBlockByNumberStats(nil), d.blockStats...)
	replayStats := append([]traceReplayStats(nil), d.replayStats...)
	stateByIndex := make([]traceStateAtTxStats, 0, len(d.stateAtTxStats))
	for _, v := range d.stateAtTxStats {
		stateByIndex = append(stateByIndex, v)
	}
	prepareByHash := make([]tracePrepareTxStats, 0, len(d.prepareTxStats))
	for _, v := range d.prepareTxStats {
		prepareByHash = append(prepareByHash, v)
	}
	d.mu.Unlock()
	prepareByHashMap := make(map[string]tracePrepareTxStats, len(prepareByHash))
	for _, v := range prepareByHash {
		prepareByHashMap[strings.ToLower(v.TxHash)] = v
	}

	errString := ""
	if err != nil {
		errString = err.Error()
	}
	traceDiagPrintf(
		"finish req=%d op=%s block=%s tracer=%s total_ms=%.3f result_count=%d err=%q",
		d.id, d.op, d.blockRef, d.tracer, durationMs(total), resultCount, errString,
	)

	sort.Slice(blockStats, func(i, j int) bool {
		return blockStats[i].TotalDur > blockStats[j].TotalDur
	})
	for _, s := range blockStats {
		traceDiagPrintf(
			"req=%d block_build block=%d total_ms=%.3f fetch_block_ms=%.3f fetch_block_results_ms=%.3f filter_txs_ms=%.3f build_metadata_ms=%.3f tx_results=%d evm_txs=%d runnable_non_evm=%d skipped_prioritized=%d skipped_associate=%d decode_errors=%d receipt_lookup_errors=%d",
			d.id,
			s.BlockNumber,
			durationMs(s.TotalDur),
			durationMs(s.FetchBlockDur),
			durationMs(s.FetchBlockResultsDur),
			durationMs(s.FilterTransactionsDur),
			durationMs(s.BuildMetadataDur),
			s.TxResults,
			s.EVMTxCount,
			s.RunnableNonEVM,
			s.SkippedPrioritized,
			s.SkippedAssociate,
			s.DecodeErrors,
			s.ReceiptLookupErrors,
		)
	}

	sort.Slice(replayStats, func(i, j int) bool {
		return replayStats[i].UptoTxIndex < replayStats[j].UptoTxIndex
	})
	for _, s := range replayStats {
		traceDiagPrintf(
			"req=%d replay upto_tx_index=%d replayed_txs=%d skipped_prioritized=%d total_ms=%.3f init_ms=%.3f loop_ms=%.3f deliver_tx_ms=%.3f err=%q",
			d.id,
			s.UptoTxIndex,
			s.ReplayedTxCount,
			s.SkippedPriority,
			durationMs(s.TotalDur),
			durationMs(s.InitDur),
			durationMs(s.LoopDur),
			durationMs(s.DeliverTxDur),
			s.Error,
		)
	}

	sort.Slice(stateByIndex, func(i, j int) bool {
		return stateByIndex[i].TxIndex < stateByIndex[j].TxIndex
	})
	for _, s := range stateByIndex {
		prepareMs := 0.0
		prepareAnteMs := 0.0
		if p, ok := prepareByHashMap[strings.ToLower(s.TxHash)]; ok {
			prepareMs = durationMs(p.TotalDur)
			prepareAnteMs = durationMs(p.AnteDur)
		}
		traceDiagPrintf(
			"req=%d tx_trace tx_index=%d tx_hash=%s total_ms=%.3f replay_ms=%.3f vm_context_ms=%.3f decode_tx_ms=%.3f prepare_tx_ms=%.3f prepare_ante_ms=%.3f err=%q",
			d.id,
			s.TxIndex,
			s.TxHash,
			durationMs(s.TotalDur),
			durationMs(s.ReplayDur),
			durationMs(s.VMContextDur),
			durationMs(s.DecodeTxDur),
			prepareMs,
			prepareAnteMs,
			s.Error,
		)
	}

	sort.Slice(prepareByHash, func(i, j int) bool {
		return prepareByHash[i].TotalDur > prepareByHash[j].TotalDur
	})
	for _, s := range prepareByHash {
		traceDiagPrintf(
			"req=%d prepare_tx tx_hash=%s has_signature=%t total_ms=%.3f ante_ms=%.3f err=%q",
			d.id,
			s.TxHash,
			s.HasSignature,
			durationMs(s.TotalDur),
			durationMs(s.AnteDur),
			s.Error,
		)
	}
}

func durationMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func tracerName(configTracer *string) string {
	if configTracer == nil || len(strings.TrimSpace(*configTracer)) == 0 {
		return "default"
	}
	return *configTracer
}

func traceDiagPrintf(format string, args ...interface{}) {
	fmt.Printf("[JEREMYDEBUG] "+format+"\n", args...)
}
