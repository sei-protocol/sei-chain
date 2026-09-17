package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	gmath "github.com/ethereum/go-ethereum/common/math"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// maxFeeHistoryBlockCount caps a single eth_feeHistory request, matching
// go-ethereum's own default block-count cap.
const maxFeeHistoryBlockCount = 1024

// earliestCommittedHeight is the first height this executor ever commits.
// eth_feeHistory resolves "earliest" here rather than the literal height 0
// this go-ethereum fork uses for the sentinel, which this executor never
// commits and would otherwise report as unavailable.
const earliestCommittedHeight = ethrpc.BlockNumber(1)

// gasPriceSuggestionNumerator and gasPriceSuggestionDenominator scale the
// admission gas-price floor up for eth_gasPrice, so a client using the
// suggested price sits above the rejection boundary rather than on it.
const (
	gasPriceSuggestionNumerator   = 110
	gasPriceSuggestionDenominator = 100
)

type infoAPI struct {
	backend Backend
	store   receiptpkg.ReceiptStore
}

// BlockNumber returns the height of the most recently committed block.
func (api *infoAPI) BlockNumber(_ context.Context) hexutil.Uint64 {
	return hexutil.Uint64(api.backend.EvmBlockNumber())
}

// ChainId returns the EVM chain ID this Autobahn shard is configured for.
//
//nolint:revive // matches the go-ethereum RPC method name eth_chainId.
func (api *infoAPI) ChainId(_ context.Context) *hexutil.Big {
	return (*hexutil.Big)(new(big.Int).SetUint64(api.backend.EvmChainID()))
}

// GasPrice returns a suggested gas price above this application's admission
// floor, so a transaction priced at the suggestion is not sitting on the
// rejection boundary.
func (api *infoAPI) GasPrice(_ context.Context) (*hexutil.Big, error) {
	floor, err := api.backend.EvmMinGasPrice()
	if err != nil {
		return nil, err
	}
	return (*hexutil.Big)(suggestedGasPrice(floor)), nil
}

// suggestedGasPrice scales floor up by the gas-price suggestion margin,
// returning a new *big.Int the caller owns.
func suggestedGasPrice(floor *big.Int) *big.Int {
	price := new(big.Int).Mul(floor, big.NewInt(gasPriceSuggestionNumerator))
	return price.Div(price, big.NewInt(gasPriceSuggestionDenominator))
}

// FeeHistoryResult is the eth_feeHistory response, matching go-ethereum's
// wire shape.
type FeeHistoryResult struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas,omitempty"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
}

// FeeHistory returns gas-used ratios and base fees for the blockCount blocks
// ending at lastBlock. This application executes every block at a zero base
// fee, so baseFeePerGas is always zero; when rewardPercentiles is non-empty,
// reward is filled with this application's fixed admission gas-price floor
// for every percentile rather than a real per-transaction percentile, since
// this chain has no congestion-based fee market to derive one from.
func (api *infoAPI) FeeHistory(ctx context.Context, blockCount gmath.HexOrDecimal64, lastBlock ethrpc.BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	if blockCount < 1 {
		return &FeeHistoryResult{}, nil
	}
	if blockCount > maxFeeHistoryBlockCount {
		blockCount = maxFeeHistoryBlockCount
	}
	if err := validateRewardPercentiles(rewardPercentiles); err != nil {
		return nil, err
	}

	resolvedLastBlock := lastBlock
	if resolvedLastBlock == ethrpc.EarliestBlockNumber {
		resolvedLastBlock = earliestCommittedHeight
	}
	endBlock, err := resolveBlockByNumber(ctx, api.backend, resolvedLastBlock)
	if err != nil {
		return nil, err
	}
	if endBlock == nil {
		return nil, api.lastBlockUnavailableError(resolvedLastBlock)
	}

	floor, err := api.backend.EvmMinGasPrice()
	if err != nil {
		return nil, err
	}
	gasLimit, err := api.backend.EvmGasLimit()
	if err != nil {
		return nil, err
	}

	result, err := api.walkFeeHistoryRange(ctx, endBlock.Block.Height, int64(blockCount), gasLimit, floor, rewardPercentiles)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// lastBlockUnavailableError explains why requested (already past the
// "earliest" remap) failed to resolve to a block: not yet committed, or no
// longer retained.
func (api *infoAPI) lastBlockUnavailableError(requested ethrpc.BlockNumber) error {
	if requested < 0 {
		return errors.New("no committed block available for fee history")
	}
	if current := api.backend.EvmBlockNumber(); uint64(requested) > current { //nolint:gosec // G115: requested is non-negative here.
		return fmt.Errorf("requested last block %d is not yet available; latest is %d", requested, current)
	}
	return fmt.Errorf("requested last block %d is not available", requested)
}

// walkFeeHistoryRange collects gasUsedRatio, baseFee, and (when requested)
// reward entries for the blockCount blocks ending at end, oldest first.
// Heights below 1 are outside the chain and are skipped rather than erroring,
// so a blockCount larger than the chain's height yields a shorter result.
// Heights above 1 that the store has since pruned still cost one lookup
// each: there is no lower-bound accessor yet to skip them outright (same gap
// tracked in sei-tendermint/internal/rpc/core/blocks.go's autobahnCheckAndGetHeight).
func (api *infoAPI) walkFeeHistoryRange(ctx context.Context, end, blockCount int64, gasLimit uint64, floor *big.Int, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	result := &FeeHistoryResult{GasUsedRatio: []float64{}}
	start := end - blockCount + 1
	for height := start; height <= end; height++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if height < 1 {
			continue
		}
		block, err := resolveBlockByNumber(ctx, api.backend, ethrpc.BlockNumber(height))
		if err != nil {
			return nil, err
		}
		if block == nil {
			continue
		}
		if result.OldestBlock == nil {
			result.OldestBlock = (*hexutil.Big)(big.NewInt(height))
		}
		ratio, err := api.lastTxGasUsedRatio(ctx, block, gasLimit)
		if err != nil {
			return nil, err
		}
		result.GasUsedRatio = append(result.GasUsedRatio, ratio)
		result.BaseFee = append(result.BaseFee, (*hexutil.Big)(new(big.Int)))
		if len(rewardPercentiles) > 0 {
			result.Reward = append(result.Reward, fixedReward(floor, len(rewardPercentiles)))
		}
	}
	if result.OldestBlock == nil {
		return &FeeHistoryResult{}, nil
	}
	// baseFeePerGas carries one more entry than gasUsedRatio: the projected
	// fee for the block after end. Always zero here.
	result.BaseFee = append(result.BaseFee, (*hexutil.Big)(new(big.Int)))
	return result, nil
}

// fixedReward returns count independent copies of the suggested gas price
// (see suggestedGasPrice), this application's stand-in for a per-percentile
// priority-fee reward.
func fixedReward(floor *big.Int, count int) []*hexutil.Big {
	price := suggestedGasPrice(floor)
	row := make([]*hexutil.Big, count)
	for i := range row {
		row[i] = (*hexutil.Big)(new(big.Int).Set(price))
	}
	return row
}

// validateRewardPercentiles rejects a percentiles list that is not strictly
// ascending or leaves the [0, 100] range, matching go-ethereum's own
// eth_feeHistory validation.
func validateRewardPercentiles(percentiles []float64) error {
	if len(percentiles) > 100 {
		return errors.New("rewardPercentiles length must be less than or equal to 100")
	}
	previous := -1.0
	for _, p := range percentiles {
		if p < 0 || p > 100 || p <= previous {
			return fmt.Errorf("invalid reward percentiles: must be ascending and between 0 and 100")
		}
		previous = p
	}
	return nil
}

// lastTxGasUsedRatio returns block's gas-used ratio, read from its last
// transaction's receipt, or 0 for an empty block.
func (api *infoAPI) lastTxGasUsedRatio(ctx context.Context, block *coretypes.ResultBlock, gasLimit uint64) (float64, error) {
	txs := block.Block.Txs
	if len(txs) == 0 || gasLimit == 0 {
		return 0, nil
	}
	lastTx, err := decodeBlockTx(txs[len(txs)-1], block.Block.Height, len(txs)-1)
	if err != nil {
		return 0, err
	}
	stored, err := api.store.GetReceipt(receiptContext(ctx), lastTx.Hash())
	if errors.Is(err, receiptpkg.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read last transaction receipt for block %d: %w", block.Block.Height, err)
	}
	if stored.BlockNumber != uint64(block.Block.Height) { //nolint:gosec // G115: block height is positive.
		// A resubmitted tx hash can overwrite this receipt with one from a
		// later block; treat that as no receipt for this height rather than
		// misreport the later block's gas used.
		return 0, nil
	}
	return float64(stored.CumulativeGasUsed) / float64(gasLimit), nil
}
