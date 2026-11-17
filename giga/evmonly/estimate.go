package evmonly

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/gasestimator"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// estimateGasErrorRatio is the search's allowed overestimation tolerance.
const estimateGasErrorRatio = 0.015

// EstimateGas returns the lowest gas limit that lets msg execute successfully
// against the current committed state. Like Call it persists no state
// change; a revert's data is returned alongside the error.
func (e *Executor) EstimateGas(ctx context.Context, blockCtx BlockContext, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	snapshot, err := e.OpenCurrentView(ctx, blockCtx)
	if err != nil {
		return 0, nil, err
	}
	defer snapshot.Close()
	return e.EstimateGasAt(ctx, snapshot, blockCtx, msg, gasCap, CallOverrides{})
}

// EstimateGasAt returns the lowest gas limit that lets msg execute
// successfully against the state view holds, with overrides applied. Like
// CallAt it persists no state change; a revert's data is returned alongside
// the error.
func (e *Executor) EstimateGasAt(ctx context.Context, view gigatypes.EVMStateView, blockCtx BlockContext, msg *core.Message, gasCap uint64, overrides CallOverrides) (uint64, []byte, error) {
	return e.EstimateGasAfterCallsAt(ctx, view, blockCtx, nil, msg, gasCap, overrides)
}

// EstimateGasAfterCalls returns what EstimateGas returns for msg against the current committed
// state with calls applied to it first, in order.
func (e *Executor) EstimateGasAfterCalls(ctx context.Context, blockCtx BlockContext, calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	snapshot, err := e.OpenCurrentView(ctx, blockCtx)
	if err != nil {
		return 0, nil, err
	}
	defer snapshot.Close()
	return e.EstimateGasAfterCallsAt(ctx, snapshot, blockCtx, calls, msg, gasCap, CallOverrides{})
}

// EstimateGasAfterCallsAt returns what EstimateGasAt returns for msg against the state view holds,
// with overrides applied and then calls applied, in order. A call that fails in the EVM leaves the
// state as a failed transaction would; one that cannot be applied at all fails the estimate.
func (e *Executor) EstimateGasAfterCallsAt(ctx context.Context, view gigatypes.EVMStateView, blockCtx BlockContext, calls []*core.Message, msg *core.Message, gasCap uint64, overrides CallOverrides) (uint64, []byte, error) {
	chainConfig := e.chainConfig(blockCtx)
	if err := validateBlockContext(chainConfig, blockCtx); err != nil {
		return 0, nil, err
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	evmCtx, err := overrides.blockContext(blockCtx)
	if err != nil {
		return 0, nil, err
	}

	// Unpooled: an estimate can hold this stateDB open far longer than a Call.
	stateDB := newNativeStateDB(gigaSnapshotStateReader{snapshot: view, missingState: e.missingState})
	if len(overrides.State) > 0 {
		// As in go-ethereum, a precompile cannot be moved for an estimate.
		if err := overrides.State.Apply(stateDB, nil); err != nil {
			return 0, nil, err
		}
	}

	estimateCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := e.applyCalls(estimateCtx, evmCtx, chainConfig, stateDB, calls); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, nil, ctxErr
		}
		return 0, nil, err
	}

	opts := &gasestimator.Options{
		Config:            chainConfig,
		Chain:             estimateChainContext{config: chainConfig},
		Header:            estimateHeader(blockCtx, overrides),
		State:             stateDB,
		ErrorRatio:        estimateGasErrorRatio,
		BlobBaseFee:       estimateBlobBaseFee(blockCtx, overrides),
		CustomPrecompiles: e.customContracts,
	}
	estimate, revert, err := gasestimator.Estimate(estimateCtx, resolveBlobGasFeeCap(msg), opts, gasCap)
	if ctxErr := ctx.Err(); ctxErr != nil {
		// Distinct from the timeout below: this is the caller's own cancellation.
		return 0, nil, ctxErr
	}
	if estimateCtx.Err() != nil {
		// A probe cut off by this deadline can read back as a success.
		return 0, nil, fmt.Errorf("EVM-only gas estimate exceeded %s execution timeout", callTimeout)
	}
	return estimate, revert, err
}

// applyCalls applies calls to stateDB, in order, as committed transactions would leave it, in evmCtx.
func (e *Executor) applyCalls(ctx context.Context, evmCtx vm.BlockContext, chainConfig *params.ChainConfig, stateDB *nativeStateDB, calls []*core.Message) error {
	for i, call := range calls {
		if err := e.applyCall(ctx, evmCtx, chainConfig, stateDB, call); err != nil {
			return fmt.Errorf("call %d: %w", i, err)
		}
	}
	return nil
}

// applyCall applies call to stateDB as a committed transaction would leave it.
func (e *Executor) applyCall(ctx context.Context, evmCtx vm.BlockContext, chainConfig *params.ChainConfig, stateDB *nativeStateDB, call *core.Message) error {
	_, cancelled, err := applyMessage(ctx, e.newEVM(evmCtx, stateDB, chainConfig, vm.Config{NoBaseFee: true}), call)
	if cancelled {
		return fmt.Errorf("EVM-only gas estimate exceeded %s execution timeout", callTimeout)
	}
	if stateErr := stateDB.Error(); stateErr != nil {
		return stateErr
	}
	if err != nil {
		return err
	}
	stateDB.clearSnapshots()
	stateDB.Finalise(params.Rules{IsEIP158: true})
	return nil
}

// estimateHeader returns the header gas estimation executes in: blockCtx's, with overrides' block
// fields applied.
func estimateHeader(blockCtx BlockContext, overrides CallOverrides) *ethtypes.Header {
	header := buildEstimateHeader(blockCtx)
	if block, ok := overrides.Block.Get(); ok {
		return block.MakeHeader(header)
	}
	return header
}

// estimateBlobBaseFee returns the blob base fee gas estimation executes with: the overridden one if
// overrides sets it, and blockCtx's otherwise.
func estimateBlobBaseFee(blockCtx BlockContext, overrides CallOverrides) *big.Int {
	if block, ok := overrides.Block.Get(); ok && block.BlobBaseFee != nil {
		return new(big.Int).Set(block.BlobBaseFee.ToInt())
	}
	return cloneOptionalBig(blockCtx.BlobBaseFee)
}

// resolveBlobGasFeeCap defaults a nil BlobGasFeeCap to zero on a copy of msg
// when msg carries blob hashes.
func resolveBlobGasFeeCap(msg *core.Message) *core.Message {
	if msg.BlobGasFeeCap != nil || len(msg.BlobHashes) == 0 {
		return msg
	}
	// gasestimator.Estimate multiplies BlobGasFeeCap unchecked once BlobHashes
	// is non-empty, so a nil value panics there. Leaving it nil otherwise lets
	// the estimate honor the block's actual blob base fee instead of forcing it
	// to zero.
	clone := *msg
	clone.BlobGasFeeCap = new(uint256.Int)
	return &clone
}

// buildEstimateHeader builds the *types.Header core.NewEVMBlockContext needs.
func buildEstimateHeader(ctx BlockContext) *ethtypes.Header {
	return &ethtypes.Header{
		ParentHash: ctx.ParentHash,
		Coinbase:   ctx.Coinbase,
		Number:     new(big.Int).SetUint64(ctx.Number),
		GasLimit:   ctx.GasLimit,
		Time:       ctx.Time,
		Difficulty: new(big.Int),
		MixDigest:  ctx.PrevRandao,
		BaseFee:    cloneOptionalBig(ctx.BaseFee),
	}
}

// estimateChainContext is the minimal core.ChainContext gasestimator.Estimate needs.
type estimateChainContext struct {
	config *params.ChainConfig
}

func (c estimateChainContext) Engine() consensus.Engine { return estimateEngine{} }

func (estimateChainContext) GetHeader(common.Hash, uint64) *ethtypes.Header { return nil }

func (estimateChainContext) CurrentHeader() *ethtypes.Header { return nil }

func (estimateChainContext) GetHeaderByNumber(uint64) *ethtypes.Header { return nil }

func (estimateChainContext) GetHeaderByHash(common.Hash) *ethtypes.Header { return nil }

func (c estimateChainContext) Config() *params.ChainConfig { return c.config }

// estimateEngine supplies only Author, the one method core.NewEVMBlockContext
// calls. Its other methods are unimplemented and must not be called.
type estimateEngine struct {
	*ethash.Ethash
}

// Author returns header.Coinbase.
func (estimateEngine) Author(header *ethtypes.Header) (common.Address, error) {
	return header.Coinbase, nil
}
