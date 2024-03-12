package tracers

import (
	"context"
	"fmt"
	"net/url"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// BlockchainLoggerFactory is a function that creates a new BlockchainLogger.
// It's going to received the parsed URL from the `live-evm-tracer` flag.
//
// The scheme of the URL is going to be used to determine which tracer to use
// by the registry.
type BlockchainLoggerFactory = func(tracerURL *url.URL) (BlockchainLogger, error)

// PR_REVIEW_NOTE: I defined the tracer identifier to be either a plain string or an URL of the form <tracer_id>://<tracer_specific_data>,
//
//	this way a tracer can be configured for example using some query parameters as "config" value. We use that in a lot
//	of our project and found it's a pretty good way to configure "generic" dependency.
//
//	We could switch to plain string if you prefer.
func NewBlockchainLogger(registry LiveTracerRegistry, tracerIdentifier string, chainConfig *params.ChainConfig) (BlockchainLogger, error) {
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

// BlockchainLogger is used to collect traces during chain processing. It's a similar
// interface as the go-ethereum's `core.BlockchainLogger` but adapted to Sei particularities.
//
// The method all starts with OnSei... to avoid confusion with the go-ethereum's `core.BlockchainLogger`
// interface and allow one to implement both interfaces in the same struct.
type BlockchainLogger interface {
	vm.EVMLogger
	state.StateLogger
	OnSeiBlockchainInit(chainConfig *params.ChainConfig)
	// OnSeiBlockStart is called before executing `block`.
	// `td` is the total difficulty prior to `block`.
	// `skip` indicates processing of this previously known block
	// will be skipped. OnBlockStart and OnBlockEnd will be emitted to
	// convey how chain is progressing. E.g. known blocks will be skipped
	// when node is started after a crash.
	OnSeiBlockStart(hash []byte, size uint64, b *types.Header)
	OnSeiBlockEnd(err error)

	// FIXME: What about OnSeiGenesisBlock/State, should we put something right here? It seems
	// it could be the best appealing ways to get our hands on the snapshot of the state
	// at the EVM "genesis" block (maybe the name should be different since it's not the genesis
	// block really, more the genesis state of the EVM).
}
type CtxBlockchainLoggerKeyType string

const CtxBlockchainLoggerKey = CtxBlockchainLoggerKeyType("evm_and_state_logger")

func SetCtxBlockchainLogger(ctx sdk.Context, logger BlockchainLogger) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), CtxBlockchainLoggerKey, logger))
}

func GetCtxBlockchainLogger(ctx sdk.Context) BlockchainLogger {
	rawVal := ctx.Context().Value(CtxBlockchainLoggerKey)
	if rawVal == nil {
		return nil
	}
	logger, ok := rawVal.(BlockchainLogger)
	if !ok {
		return nil
	}
	return logger
}
