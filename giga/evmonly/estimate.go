package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/gasestimator"
	"github.com/ethereum/go-ethereum/params"
)

// estimateGasErrorRatio is the overestimation go-ethereum's eth_estimateGas
// tolerates in exchange for fewer executions.
const estimateGasErrorRatio = 0.015

// EstimateGas returns the lowest gas limit, to within estimateGasErrorRatio,
// at which msg executes without running out of gas against the current
// committed state, never above gasCap. Every execution of the search reads the
// same state snapshot. A message that fails for a reason more gas cannot fix
// returns that failure's error and, for a revert, its return data.
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

	stateDB := e.acquireStateDB(gigaSnapshotStateReader{snapshot: snapshot, missingState: e.missingState})
	defer e.releaseStateDB(stateDB)

	// The estimator cancels its EVM when ctx ends but keeps probing, so the
	// timeout is checked after it returns rather than relying on its result.
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	header := estimateHeader(blockCtx)
	opts := &gasestimator.Options{
		Config:            chainConfig,
		Chain:             estimateChainContext{config: chainConfig, coinbase: blockCtx.Coinbase},
		Header:            header,
		State:             stateDB,
		CustomPrecompiles: customPrecompileMap(e.cfg.CustomPrecompiles),
		ErrorRatio:        estimateGasErrorRatio,
	}
	estimate, revert, err := gasestimator.Estimate(callCtx, msg, opts, gasCap)
	if errors.Is(callCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return 0, nil, fmt.Errorf("EVM-only call exceeded %s execution timeout", callTimeout)
	}
	return estimate, revert, err
}

// estimateHeader returns the header core.NewEVMBlockContext derives the same
// vm.BlockContext from as buildBlockContext does for blockCtx, except that
// BLOBBASEFEE is the EIP-4844 minimum instead of blockCtx.BlobBaseFee.
func estimateHeader(blockCtx BlockContext) *ethtypes.Header {
	// A nil ExcessBlobGas leaves vm.BlockContext.BlobBaseFee nil, which the
	// BLOBBASEFEE opcode cannot handle.
	return &ethtypes.Header{
		ExcessBlobGas: new(uint64),
		ParentHash:    blockCtx.ParentHash,
		Coinbase:      blockCtx.Coinbase,
		Number:        new(big.Int).SetUint64(blockCtx.Number),
		Time:          blockCtx.Time,
		GasLimit:      blockCtx.GasLimit,
		Difficulty:    new(big.Int),
		MixDigest:     blockCtx.PrevRandao,
		BaseFee:       cloneOptionalBig(blockCtx.BaseFee),
	}
}

// estimateChainContext is the core.ChainContext of a chain that tracks no
// header but the current block's, so BLOCKHASH resolves only the parent.
type estimateChainContext struct {
	config   *params.ChainConfig
	coinbase common.Address
}

func (c estimateChainContext) Engine() consensus.Engine {
	return coinbaseEngine{coinbase: c.coinbase}
}

func (c estimateChainContext) GetHeader(common.Hash, uint64) *ethtypes.Header {
	return nil
}

func (c estimateChainContext) Config() *params.ChainConfig {
	return c.config
}

// coinbaseEngine is a consensus.Engine that only knows the block's author;
// core.NewEVMBlockContext calls nothing else on it.
type coinbaseEngine struct {
	consensus.Engine
	coinbase common.Address
}

func (e coinbaseEngine) Author(*ethtypes.Header) (common.Address, error) {
	return e.coinbase, nil
}
