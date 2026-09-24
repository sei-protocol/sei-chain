package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/gasestimator"
	"github.com/ethereum/go-ethereum/params"
)

// estimateGasErrorRatio matches the standard eth_estimateGas library's own
// tolerance: the search stops once the gap between the highest failing and
// lowest passing gas limit is within this fraction of the true minimum,
// rather than pinpointing it.
const estimateGasErrorRatio = 0.015

// EstimateGas returns the lowest gas limit that lets msg execute successfully
// against the current committed state, using the standard gasestimator
// library directly rather than a hand-rolled search. Like Call it creates no
// transaction and persists no state change. A message that still fails at
// gasCap returns its execution error, unwrapped; a revert carries its data in
// the second return value.
func (e *Executor) EstimateGas(ctx context.Context, blockCtx BlockContext, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	chainConfig := e.chainConfig(blockCtx)
	if err := validateBlockContext(chainConfig, blockCtx); err != nil {
		return 0, nil, err
	}
	if e.stateStore == nil {
		return 0, nil, errMissingStateStore
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}

	snapshot := e.stateStore.OpenView()
	if snapshot == nil {
		return 0, nil, errors.New("giga store returned a nil snapshot")
	}
	defer snapshot.Close()

	// gasestimator.Estimate copies this state per probe and holds it for the
	// whole search rather than one execution, so it bypasses acquireStateDB's
	// pool: that pool is sized for the hot block-execution/OCC path, and an
	// estimate can hold a stateDB open far longer than a single Call.
	stateDB := newNativeStateDB(gigaSnapshotStateReader{snapshot: snapshot, missingState: e.missingState})

	estimateCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	opts := &gasestimator.Options{
		Config:            chainConfig,
		Chain:             estimateChainContext{config: chainConfig},
		Header:            buildEstimateHeader(blockCtx),
		State:             stateDB,
		ErrorRatio:        estimateGasErrorRatio,
		CustomPrecompiles: customPrecompileMap(e.cfg.CustomPrecompiles),
	}
	// A nil BlobGasFeeCap (ToMessage leaves it nil for a non-blob call) leaves
	// BlobBaseFee nil too, since buildEstimateHeader sets no ExcessBlobGas for
	// NewEVMBlockContext to derive it from; BLOBBASEFEE against a nil
	// BlobBaseFee then panics, and gasestimator.execute's recover turns that
	// into "not enough gas" forever. Defaulting it to zero here gives
	// BLOBBASEFEE the same literal zero buildBlockContext already gives Call.
	estimate, revert, err := gasestimator.Estimate(estimateCtx, resolveBlobGasFeeCap(msg), opts, gasCap)
	if ctxErr := ctx.Err(); ctxErr != nil {
		// The caller's own context (not just our internal deadline below) is
		// done: propagate that as-is so a client disconnect reads as
		// context.Canceled rather than the timeout message below.
		return 0, nil, ctxErr
	}
	if estimateCtx.Err() != nil {
		// gasestimator.Estimate never checks whether a probe's EVM was
		// cancelled: this fork's interpreter only samples cancellation at
		// JUMP/JUMPI, clearing the resulting errStopToken to nil just like a
		// normal halt, so a probe cut off by this deadline can read back as a
		// successful halt at less gas than the call actually needs. Once the
		// deadline has fired, nothing Estimate returned can be trusted.
		return 0, nil, fmt.Errorf("EVM-only gas estimate exceeded %s execution timeout", callTimeout)
	}
	return estimate, revert, err
}

// resolveBlobGasFeeCap returns msg, or a copy of it with a zero (not nil)
// BlobGasFeeCap, without mutating the caller's message.
func resolveBlobGasFeeCap(msg *core.Message) *core.Message {
	if msg.BlobGasFeeCap != nil {
		return msg
	}
	clone := *msg
	clone.BlobGasFeeCap = new(big.Int)
	return &clone
}

// buildEstimateHeader translates blockCtx into the *types.Header shape
// core.NewEVMBlockContext expects, reproducing buildBlockContext's own
// semantics: BLOCKHASH resolves only the immediate parent (see
// estimateChainContext.GetHeader), and Coinbase is always the zero address
// (see estimateEngine.Author).
func buildEstimateHeader(ctx BlockContext) *ethtypes.Header {
	return &ethtypes.Header{
		ParentHash: ctx.ParentHash,
		Number:     new(big.Int).SetUint64(ctx.Number),
		GasLimit:   ctx.GasLimit,
		Time:       ctx.Time,
		Difficulty: new(big.Int),
		MixDigest:  ctx.PrevRandao,
		BaseFee:    cloneOptionalBig(ctx.BaseFee),
	}
}

// estimateChainContext is the minimal core.ChainContext gasestimator.Estimate
// needs. It exposes no header history beyond the immediate parent, matching
// the limits buildBlockContext already applies to block execution: only the
// current block's parent hash is tracked outside of block execution.
type estimateChainContext struct {
	config *params.ChainConfig
}

func (c estimateChainContext) Engine() consensus.Engine { return estimateEngine{} }

func (estimateChainContext) GetHeader(common.Hash, uint64) *ethtypes.Header { return nil }

func (c estimateChainContext) Config() *params.ChainConfig { return c.config }

// estimateEngine supplies only Author, the one consensus.Engine method
// core.NewEVMBlockContext calls, returning the zero address to match
// buildBlockContext's own Coinbase (never set outside FinalizeBlock). Every
// other method is left on a nil *ethash.Ethash embed and must not be called.
type estimateEngine struct {
	*ethash.Ethash
}

func (estimateEngine) Author(*ethtypes.Header) (common.Address, error) {
	return common.Address{}, nil
}
