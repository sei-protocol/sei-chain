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
)

// maxFeeHistoryBlockCount caps a single eth_feeHistory request, matching
// go-ethereum's own default block-count cap.
const maxFeeHistoryBlockCount = 1024

// earliestCommittedHeight is what "earliest" resolves to for eth_feeHistory. It is a fixed
// contract of this RPC surface, not a read of the running chain's actual genesis: every giga
// deployment today starts at height 1, and InitChain's InitialHeight isn't recoverable after a
// restart to check that assumption at runtime. A chain genesis'd above height 1 would have
// "earliest" resolve to a height that never existed.
const earliestCommittedHeight = int64(1)

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

// gasPriceCongestionTiers maps the latest block's gasUsedRatio to the reward percentile GasPrice
// escalates toward under load, in descending order of minRatio: a fuller block suggests a price
// closer to what its higher-paying transactions actually paid, rather than a flat margin over the
// admission floor.
var gasPriceCongestionTiers = []struct {
	minRatio   float64
	percentile float64
}{
	{minRatio: 0.8, percentile: 90},
	{minRatio: 0.5, percentile: 75},
	{minRatio: 0, percentile: 25},
}

// GasPrice returns a suggested gas price: the latest block's gasUsedRatio picks a reward
// percentile from gasPriceCongestionTiers, escalating the suggestion as the chain gets busier.
// It falls back to a fixed margin over the admission floor when there is no latest block yet, its
// gas limit is unknown, or the tier's percentile was never precomputed for it.
func (api *infoAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	floor, err := api.backend.EvmMinGasPrice()
	if err != nil {
		return nil, err
	}
	if reward, ok := api.congestionReward(ctx); ok {
		return (*hexutil.Big)(reward), nil
	}
	return (*hexutil.Big)(suggestedGasPrice(floor)), nil
}

// congestionReward answers GasPrice's escalated suggestion from the latest block's stored stats.
func (api *infoAPI) congestionReward(ctx context.Context) (*big.Int, bool) {
	current := api.store.LatestVersion()
	if current <= 0 {
		return nil, false
	}
	gasLimit, err := api.backend.EvmGasLimit()
	if err != nil || gasLimit == 0 {
		return nil, false
	}
	stats, err := api.store.GetBlockStats(receiptContext(ctx), uint64(current)) //nolint:gosec // G115: current is positive here.
	if err != nil {
		return nil, false
	}
	ratio := gasUsedRatio(stats.TotalGasUsed, gasLimit)
	for _, tier := range gasPriceCongestionTiers {
		if ratio < tier.minRatio {
			continue
		}
		reward, ok := stats.RewardAt(tier.percentile)
		if !ok {
			return nil, false
		}
		return new(big.Int).SetUint64(reward), true
	}
	return nil, false
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

// emptyFeeHistoryResult is the "no retrievable blocks" response: oldestBlock
// 0 and an empty gasUsedRatio, matching go-ethereum's shape rather than a
// zero-value struct's nil fields, which a strict client's unconditional
// BigInt(oldestBlock) would reject.
func emptyFeeHistoryResult() *FeeHistoryResult {
	return &FeeHistoryResult{OldestBlock: (*hexutil.Big)(new(big.Int)), GasUsedRatio: []float64{}}
}

// FeeHistory returns gas-used ratios and base fees for the blockCount blocks ending at lastBlock.
// baseFeePerGas is always zero, matching this application's fixed base fee. reward is per
// percentile: the stored aggregate (see receipt.BlockStats) when that percentile was recorded for
// the height, the suggested gas price otherwise — a real per-transaction reward needs a receipt
// per transaction across the range, which this executor's receipt store has no bulk read for.
func (api *infoAPI) FeeHistory(ctx context.Context, blockCount gmath.HexOrDecimal64, lastBlock ethrpc.BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	if blockCount < 1 {
		return emptyFeeHistoryResult(), nil
	}
	if blockCount > maxFeeHistoryBlockCount {
		blockCount = maxFeeHistoryBlockCount
	}
	if err := validateRewardPercentiles(rewardPercentiles); err != nil {
		return nil, err
	}

	end, err := api.resolveEndHeight(lastBlock)
	if err != nil {
		return nil, err
	}

	floor, err := api.backend.EvmMinGasPrice()
	if err != nil {
		return nil, err
	}
	// The current gas limit is applied to every block in the range; a gasUsedRatio for a block
	// committed under a different limit would be wrong, but this executor has no record of a
	// block's own limit to use instead.
	gasLimit, err := api.backend.EvmGasLimit()
	if err != nil {
		return nil, err
	}

	return api.walkFeeHistoryRange(ctx, end, int64(blockCount), gasLimit, floor, rewardPercentiles)
}

// resolveEndHeight turns lastBlock's tag or explicit height into a concrete, committed height.
func (api *infoAPI) resolveEndHeight(lastBlock ethrpc.BlockNumber) (int64, error) {
	current := api.store.LatestVersion()
	switch lastBlock {
	case ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.LatestBlockNumber, ethrpc.PendingBlockNumber:
		if current <= 0 {
			return 0, errors.New("no committed block available for fee history")
		}
		return current, nil
	case ethrpc.EarliestBlockNumber:
		// EarliestVersion is 0 until something has pruned the store, in which case
		// earliestCommittedHeight (this deployment's genesis) is the true earliest.
		return max(earliestCommittedHeight, api.store.EarliestVersion()), nil
	default:
		if lastBlock < 0 {
			return 0, fmt.Errorf("requested last block %d is not available", lastBlock)
		}
		if int64(lastBlock) > current {
			return 0, fmt.Errorf("requested last block %d is not yet available; latest is %d", lastBlock, current)
		}
		return int64(lastBlock), nil
	}
}

// walkFeeHistoryRange collects gasUsedRatio, baseFee, and (when requested) reward entries for the
// blockCount blocks ending at end, oldest first, skipping heights below 1 and heights with no
// recorded stats (pruned, or a block this store never wrote stats for).
func (api *infoAPI) walkFeeHistoryRange(ctx context.Context, end, blockCount int64, gasLimit uint64, floor *big.Int, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	result := &FeeHistoryResult{GasUsedRatio: []float64{}}
	// lastGoodResult is the most recent contiguous run completed before a hole. If a hole is the
	// last thing the loop sees (nothing after it has stats either), this is returned instead of
	// discarding a real, usable prefix just because it doesn't reach end.
	var lastGoodResult *FeeHistoryResult
	start := end - blockCount + 1
	for height := start; height <= end; height++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if height < 1 {
			continue
		}
		stats, err := api.store.GetBlockStats(receiptContext(ctx), uint64(height)) //nolint:gosec // G115: height is positive here.
		if err != nil {
			if errors.Is(err, receiptpkg.ErrNotFound) || errors.Is(err, receiptpkg.ErrBlockStatsNotSupported) {
				if result.OldestBlock != nil {
					// A hole after rows have already been emitted would otherwise misattribute
					// every later row to the wrong height — eth_feeHistory's row i describes block
					// oldestBlock+i, and skipping in place shifts that mapping silently. Restart
					// the accumulation instead, so the returned range stays a contiguous run.
					lastGoodResult = result
					result = &FeeHistoryResult{GasUsedRatio: []float64{}}
				}
				continue
			}
			return nil, fmt.Errorf("read block stats for block %d: %w", height, err)
		}
		if result.OldestBlock == nil {
			result.OldestBlock = (*hexutil.Big)(big.NewInt(height))
		}
		result.GasUsedRatio = append(result.GasUsedRatio, gasUsedRatio(stats.TotalGasUsed, gasLimit))
		result.BaseFee = append(result.BaseFee, (*hexutil.Big)(new(big.Int)))
		if len(rewardPercentiles) > 0 {
			result.Reward = append(result.Reward, api.rewardRow(stats, floor, rewardPercentiles))
		}
	}
	if result.OldestBlock == nil {
		// The run since the last hole (if any) never got started either: fall back to the
		// contiguous run that preceded it rather than discarding a usable prefix that just
		// doesn't happen to reach end.
		if lastGoodResult != nil {
			result = lastGoodResult
		} else {
			// end resolved successfully just before this call; only a store eviction racing that
			// resolution reaches here.
			return nil, fmt.Errorf("block %d is no longer available", end)
		}
	}
	// baseFeePerGas carries one more entry than gasUsedRatio: the projected fee for the block
	// after end. Always zero here.
	result.BaseFee = append(result.BaseFee, (*hexutil.Big)(new(big.Int)))
	return result, nil
}

// gasUsedRatio divides totalGasUsed by gasLimit to 4 decimal places: multiply by 10000, integer
// divide, then divide back down, which preserves more precision than a bare float division.
func gasUsedRatio(totalGasUsed, gasLimit uint64) float64 {
	if gasLimit == 0 {
		return 0
	}
	ratioInt := (totalGasUsed * 10000) / gasLimit
	return float64(ratioInt) / 10000.0
}

// rewardRow answers rewardPercentiles for stats, per percentile: the stored value when present,
// the suggested gas price as a placeholder when a percentile wasn't precomputed, or zero for
// every percentile when the block has no reward-eligible tx at all.
func (api *infoAPI) rewardRow(stats receiptpkg.BlockStats, floor *big.Int, rewardPercentiles []float64) []*hexutil.Big {
	row := make([]*hexutil.Big, len(rewardPercentiles))
	if len(stats.RewardPercentiles) == 0 {
		// No reward-eligible tx at all (including an empty block): every percentile is zero,
		for i := range row {
			row[i] = (*hexutil.Big)(new(big.Int))
		}
		return row
	}
	guess := suggestedGasPrice(floor)
	for i, p := range rewardPercentiles {
		reward, ok := stats.RewardAt(p)
		if !ok {
			// This percentile wasn't precomputed, but others in the same row may have been — a
			// miss on one must not discard the real values this block does have.
			row[i] = (*hexutil.Big)(new(big.Int).Set(guess))
			continue
		}
		row[i] = (*hexutil.Big)(new(big.Int).SetUint64(reward))
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
			return errors.New("invalid reward percentiles: must be ascending and between 0 and 100")
		}
		previous = p
	}
	return nil
}
