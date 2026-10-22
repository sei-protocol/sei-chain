package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/tracers"
	tracerlogger "github.com/ethereum/go-ethereum/eth/tracers/logger"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native" // registers the native tracers
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
)

const (
	muxTracerName = "muxTracer"
	// maxMuxTracerNestingDepth bounds how deeply muxTracer configs may nest, as in the v2 EVM RPC.
	maxMuxTracerNestingDepth = 16
)

// allowedTracers are go-ethereum's native tracers. JavaScript tracers are never run.
var allowedTracers = map[string]struct{}{
	"callTracer":     {},
	"flatCallTracer": {},
	"prestateTracer": {},
	"4byteTracer":    {},
	muxTracerName:    {},
	"noopTracer":     {},
}

var (
	errTraceBusy           = errors.New("trace request rejected due to concurrency limit: server busy")
	errTraceTxNotFound     = errors.New("transaction not found")
	errTraceOverrides      = errors.New("state and block overrides are not supported by EVM-only RPC")
	errTracerNameEmpty     = errors.New("debug tracer name must not be empty")
	errTracerNotAllowed    = errors.New("debug tracer is not allowed; only go-ethereum's native tracers may be used")
	errMuxTracerTooDeep    = fmt.Errorf("muxTracer nesting depth exceeds maximum of %d", maxMuxTracerNestingDepth)
	errNestedTracerPadding = errors.New("nested debug tracer name must not have leading or trailing whitespace")
)

// debugAPI serves the debug_trace* methods through go-ethereum's tracer API, bounding how many run
// at once, how long each may take, and what each may retain.
type debugAPI struct {
	tracers *tracers.API
	// calls traces calls, which read a block's header and state but not its transactions.
	calls *tracers.API
	store receiptpkg.ReceiptStore
	// slots holds one entry per running trace; nil when the number is unbounded.
	slots             chan struct{}
	timeout           time.Duration
	maxStructLogBytes int
}

func newDebugAPI(backend Backend, store receiptpkg.ReceiptStore, limits gigaconfig.RPCConfig) *debugAPI {
	traces := newTraceBackend(backend, &blockAPI{backend: backend, store: store}, &txAPI{backend: backend, store: store})
	api := &debugAPI{
		tracers:           tracers.NewAPI(traces),
		calls:             tracers.NewAPI(traces.forCalls()),
		store:             store,
		timeout:           limits.TraceTimeout,
		maxStructLogBytes: limits.MaxTraceStructLogBytes,
	}
	if limits.MaxConcurrentTraceCalls > 0 {
		api.slots = make(chan struct{}, limits.MaxConcurrentTraceCalls)
	}
	return api
}

// TraceTransaction replays the finalized transaction hash with a tracer and returns its trace.
func (api *debugAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (any, error) {
	stored, err := receiptFor(ctx, api.store, hash)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, &invalidParamsError{error: errTraceTxNotFound}
	}
	return api.trace(ctx, config, func(ctx context.Context, config *tracers.TraceConfig) (any, error) {
		return api.tracers.TraceTransaction(ctx, hash, config)
	})
}

// TraceCall traces args as a call against the committed state after the block blockNrOrHash names.
func (api *debugAPI) TraceCall(ctx context.Context, args export.TransactionArgs, blockNrOrHash ethrpc.BlockNumberOrHash, config *tracers.TraceCallConfig) (any, error) {
	if config == nil {
		config = &tracers.TraceCallConfig{}
	}
	if config.StateOverrides != nil || config.BlockOverrides != nil {
		return nil, &invalidParamsError{error: errTraceOverrides}
	}
	return api.trace(ctx, &config.TraceConfig, func(ctx context.Context, _ *tracers.TraceConfig) (any, error) {
		return api.calls.TraceCall(ctx, args, blockNrOrHash, config)
	})
}

// TraceBlockByNumber traces every transaction the block at number stored a receipt for.
func (api *debugAPI) TraceBlockByNumber(ctx context.Context, number ethrpc.BlockNumber, config *tracers.TraceConfig) (any, error) {
	return api.trace(ctx, config, func(ctx context.Context, config *tracers.TraceConfig) (any, error) {
		return api.tracers.TraceBlockByNumber(ctx, number, config)
	})
}

// TraceBlockByHash traces every transaction the block with hash stored a receipt for.
func (api *debugAPI) TraceBlockByHash(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (any, error) {
	return api.trace(ctx, config, func(ctx context.Context, config *tracers.TraceConfig) (any, error) {
		return api.tracers.TraceBlockByHash(ctx, hash, config)
	})
}

// trace runs one trace under config, once the config is accepted and a trace slot is free, bounded
// by the trace timeout. A nil config is the default struct logger's.
func (api *debugAPI) trace(ctx context.Context, config *tracers.TraceConfig, run func(context.Context, *tracers.TraceConfig) (any, error)) (any, error) {
	if config == nil {
		config = &tracers.TraceConfig{}
	}
	if err := api.prepareTraceConfig(config); err != nil {
		return nil, err
	}
	ctx, done, err := api.beginTrace(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	result, err := run(ctx, config)
	// An interrupted trace can come back as a partial result, which must not be served.
	if expired := ctx.Err(); expired != nil {
		return nil, expired
	}
	return result, err
}

// beginTrace takes a trace slot and bounds the trace by the trace timeout, returning the function
// that releases both. It refuses at once when every slot is taken.
func (api *debugAPI) beginTrace(ctx context.Context) (context.Context, func(), error) {
	release := func() {}
	if api.slots != nil {
		select {
		case api.slots <- struct{}{}:
			release = func() { <-api.slots }
		default:
			return nil, nil, errTraceBusy
		}
	}
	if api.timeout <= 0 {
		return ctx, release, nil
	}
	ctx, cancel := context.WithTimeout(ctx, api.timeout)
	return ctx, func() {
		cancel()
		release()
	}, nil
}

// prepareTraceConfig refuses a tracer this node does not run, and caps the default struct logger's
// output at the configured size.
func (api *debugAPI) prepareTraceConfig(config *tracers.TraceConfig) error {
	if err := validateTracer(config); err != nil {
		return &invalidParamsError{error: err}
	}
	if config.Tracer != nil || api.maxStructLogBytes <= 0 {
		return nil
	}
	if config.Config == nil {
		config.Config = &tracerlogger.Config{}
	}
	if config.Limit <= 0 || config.Limit > api.maxStructLogBytes {
		config.Limit = api.maxStructLogBytes
	}
	return nil
}

// validateTracer refuses a tracer outside allowedTracers, including any nested in a muxTracer. It
// trims the tracer's name in place, so go-ethereum resolves the tracer that was checked rather than
// reading a padded name as JavaScript.
func validateTracer(config *tracers.TraceConfig) error {
	if config.Tracer == nil {
		return nil
	}
	name := strings.TrimSpace(*config.Tracer)
	if name == "" {
		return errTracerNameEmpty
	}
	*config.Tracer = name
	if _, ok := allowedTracers[name]; !ok {
		return fmt.Errorf("%w: %q", errTracerNotAllowed, name)
	}
	if name == muxTracerName {
		if err := validateMuxTracerConfig(config.TracerConfig, 1); err != nil {
			return fmt.Errorf("invalid muxTracer config: %w", err)
		}
	}
	return nil
}

// validateMuxTracerConfig refuses a muxTracer config naming a tracer outside allowedTracers, at any
// nesting depth up to maxMuxTracerNestingDepth.
func validateMuxTracerConfig(raw json.RawMessage, depth int) error {
	if len(raw) == 0 {
		return nil
	}
	if depth > maxMuxTracerNestingDepth {
		return errMuxTracerTooDeep
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil {
		return err
	}
	for name, cfg := range nested {
		// A nested name is forwarded to go-ethereum as written, so a padded one is refused rather
		// than trimmed.
		if strings.TrimSpace(name) == "" {
			return errTracerNameEmpty
		}
		if name != strings.TrimSpace(name) {
			return fmt.Errorf("%w: %q", errNestedTracerPadding, name)
		}
		if _, ok := allowedTracers[name]; !ok {
			return fmt.Errorf("%w: %q", errTracerNotAllowed, name)
		}
		if name == muxTracerName {
			if err := validateMuxTracerConfig(cfg, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
