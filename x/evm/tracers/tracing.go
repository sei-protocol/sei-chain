package tracers

import (
	"context"
	"fmt"
	"net/url"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtracing "github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/x/evm/tracing"
)

// BlockchainTracerFactory is a function that creates a new [BlockchainTracer].
// It's going to received the parsed URL from the `live-evm-tracer` flag.
//
// The scheme of the URL is going to be used to determine which tracer to use
// by the registry.
type BlockchainTracerFactory = func(tracerURL *url.URL) (*tracing.Hooks, error)

// NewBlockchainTracer creates a new [tracing.Hooks] struct that is used to trace blocks and transactions
// for EVM needs. The tracer is instansiated by the provided URL and registered in the registry.
func NewBlockchainTracer(registry LiveTracerRegistry, tracerIdentifier string, chainConfig *params.ChainConfig) (*tracing.Hooks, error) {
	tracerURL, err := url.Parse(tracerIdentifier)
	if err != nil {
		return nil, fmt.Errorf("tracer value %q should have been a valid URL: %w", tracerIdentifier, err)
	}

	// We accept plain string like "firehose" and URL like "firehose://...". The former form parses as
	// an URL correct with `scheme="", host="", path="firehose", so the logic below does that. Take
	// the scheme is defined otherwise.
	tracerID := tracerURL.Scheme
	if tracerID == "" && tracerURL.Host == "" && tracerURL.EscapedPath() != "" {
		tracerID = tracerURL.EscapedPath()
	}

	if tracerID == "" {
		return nil, fmt.Errorf("unable to extract tracer ID from %q", tracerID)
	}

	factory, found := registry.GetFactoryByID(tracerID)
	if !found {
		return nil, fmt.Errorf("tracer %q is not registered", tracerID)
	}

	tracer, err := factory(tracerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer: %w", err)
	}

	tracer.OnSeiBlockchainInit(chainConfig)

	return tracer, nil
}

type CtxBlockchainTracerKeyType string

const CtxBlockchainTracerKey = CtxBlockchainTracerKeyType("evm_and_state_logger")

func SetCtxBlockchainTracer(ctx sdk.Context, logger *tracing.Hooks) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), CtxBlockchainTracerKey, logger))
}

// GetCtxBlockchainTracer function to get the SEI specific [tracing.Hooks] struct
// used to trace EVM blocks and transactions.
func GetCtxBlockchainTracer(ctx sdk.Context) *tracing.Hooks {
	rawVal := ctx.Context().Value(CtxBlockchainTracerKey)
	if rawVal == nil {
		return nil
	}
	logger, ok := rawVal.(*tracing.Hooks)
	if !ok {
		return nil
	}
	return logger
}

// GetCtxEthTracingHooks is a convenience function to get the ethtracing.Hooks from the context
// avoiding nil pointer exceptions when trying to send the tracer to lower-level go-ethereum components
// that deals with *tracing.Hooks directly.
func GetCtxEthTracingHooks(ctx sdk.Context) *ethtracing.Hooks {
	if logger := GetCtxBlockchainTracer(ctx); logger != nil {
		return logger.Hooks
	}

	return nil
}

var _ sdk.TxTracer = (*TxTracerHooks)(nil)

type TxTracerHooks struct {
	Hooks *tracing.Hooks

	OnTxReset  func()
	OnTxCommit func()
}

func (h TxTracerHooks) InjectInContext(ctx sdk.Context) sdk.Context {
	return SetCtxBlockchainTracer(ctx, h.Hooks)
}

func (h TxTracerHooks) Reset() {
	if h.OnTxReset != nil {
		h.OnTxReset()
	}
}

func (h TxTracerHooks) Commit() {
	if h.OnTxCommit != nil {
		h.OnTxCommit()
	}
}
