package evmonly

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/gasestimator"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"

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
	chainConfig := e.chainConfig(blockCtx)
	if err := validateBlockContext(chainConfig, blockCtx); err != nil {
		return 0, nil, err
	}
	if err := ctx.Err(); err != nil {
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

	opts := &gasestimator.Options{
		Config:            chainConfig,
		Chain:             estimateChainContext{config: chainConfig},
		Header:            buildEstimateHeader(blockCtx),
		State:             stateDB,
		ErrorRatio:        estimateGasErrorRatio,
		BlockOverrides:    estimateBlockOverrides(blockCtx, overrides),
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

// estimateBlockOverrides returns the block overrides gas estimation applies: those overrides holds,
// and ctx's coinbase and blob base fee where overrides sets neither.
func estimateBlockOverrides(ctx BlockContext, overrides CallOverrides) *export.BlockOverrides {
	applied := overrides.Block.Or(export.BlockOverrides{})
	if applied.FeeRecipient == nil {
		coinbase := ctx.Coinbase
		applied.FeeRecipient = &coinbase
	}
	if blobBaseFee := cloneOptionalBig(ctx.BlobBaseFee); applied.BlobBaseFee == nil && blobBaseFee != nil {
		applied.BlobBaseFee = (*hexutil.Big)(blobBaseFee)
	}
	return &applied
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
	clone.BlobGasFeeCap = new(big.Int)
	return &clone
}

// buildEstimateHeader builds the *types.Header core.NewEVMBlockContext needs.
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

// estimateChainContext is the minimal core.ChainContext gasestimator.Estimate needs.
type estimateChainContext struct {
	config *params.ChainConfig
}

func (c estimateChainContext) Engine() consensus.Engine { return estimateEngine{} }

func (estimateChainContext) GetHeader(common.Hash, uint64) *ethtypes.Header { return nil }

func (c estimateChainContext) Config() *params.ChainConfig { return c.config }

// estimateEngine supplies only Author, the one method core.NewEVMBlockContext
// calls. Its other methods are unimplemented and must not be called.
type estimateEngine struct {
	*ethash.Ethash
}

// Author returns the zero address. estimateBlockOverrides always sets
// FeeRecipient, which overrides this value, so it is never observed.
func (estimateEngine) Author(*ethtypes.Header) (common.Address, error) {
	return common.Address{}, nil
}
