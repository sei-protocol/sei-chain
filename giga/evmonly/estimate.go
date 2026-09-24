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

// estimateGasErrorRatio is the search's allowed overestimation tolerance.
const estimateGasErrorRatio = 0.015

// EstimateGas returns the lowest gas limit that lets msg execute successfully
// against the current committed state. Like Call it persists no state
// change; a revert's data is returned alongside the error.
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

	// Unpooled: an estimate can hold this stateDB open far longer than a Call.
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

// resolveBlobGasFeeCap defaults a nil BlobGasFeeCap to zero on a copy of msg.
func resolveBlobGasFeeCap(msg *core.Message) *core.Message {
	if msg.BlobGasFeeCap != nil {
		return msg
	}
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

func (estimateEngine) Author(*ethtypes.Header) (common.Address, error) {
	return common.Address{}, nil
}
