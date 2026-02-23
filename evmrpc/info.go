package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

const DefaultBlockGasLimit = 10000000
const defaultPriorityFeePerGas = 1000000000 // 1gwei
const defaultThresholdPercentage = 80       // 80%

type InfoAPI struct {
	tmClient         rpcclient.Client
	keeper           *keeper.Keeper
	ctxProvider      func(int64) sdk.Context
	txConfigProvider func(int64) client.TxConfig
	homeDir          string
	connectionType   ConnectionType
	maxBlocks        int64
	txDecoder        sdk.TxDecoder
	watermarks       *WatermarkManager
}

func NewInfoAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, homeDir string, maxBlocks int64, connectionType ConnectionType, txDecoder sdk.TxDecoder, watermarks *WatermarkManager) *InfoAPI {
	return &InfoAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txConfigProvider: txConfigProvider, homeDir: homeDir, connectionType: connectionType, maxBlocks: maxBlocks, txDecoder: txDecoder, watermarks: watermarks}
}

type FeeHistoryResult struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas,omitempty"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
}

func (i *InfoAPI) BlockNumber() hexutil.Uint64 {
	startTime := time.Now()
	defer recordMetrics("eth_BlockNumber", i.connectionType, startTime)
	height, err := i.latestHeight(context.Background())
	if err != nil {
		height = i.ctxProvider(LatestCtxHeight).BlockHeight()
	}
	return hexutil.Uint64(height) //nolint:gosec
}

//nolint:revive
func (i *InfoAPI) ChainId() *hexutil.Big {
	startTime := time.Now()
	defer recordMetrics("eth_ChainId", i.connectionType, startTime)
	return (*hexutil.Big)(i.keeper.ChainID(i.ctxProvider(LatestCtxHeight)))
}

func (i *InfoAPI) Coinbase() (addr common.Address, err error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_Coinbase", i.connectionType, startTime, err)
	return i.keeper.GetFeeCollectorAddress(i.ctxProvider(LatestCtxHeight))
}

func (i *InfoAPI) Accounts() (result []common.Address, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_Accounts", i.connectionType, startTime, returnErr)
	kb, err := getTestKeyring(i.homeDir)
	if err != nil {
		return []common.Address{}, err
	}
	for addr := range getAddressPrivKeyMap(kb) {
		result = append(result, common.HexToAddress(addr))
	}
	return result, nil
}

func (i *InfoAPI) GasPrice(ctx context.Context) (result *hexutil.Big, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_GasPrice", i.connectionType, startTime, returnErr)
	baseFee := i.keeper.GetNextBaseFeePerGas(i.ctxProvider(LatestCtxHeight)).TruncateInt().BigInt()
	totalGasUsed, err := i.getCongestionData(ctx, nil)
	if err != nil {
		return nil, err
	}
	feeHist, err := i.FeeHistory(ctx, 1, rpc.LatestBlockNumber, []float64{50})
	if err != nil {
		return nil, err
	}
	var medianRewardPrevBlock *big.Int
	if len(feeHist.Reward) == 0 || len(feeHist.Reward[0]) == 0 {
		medianRewardPrevBlock = big.NewInt(defaultPriorityFeePerGas)
	} else {
		medianRewardPrevBlock = feeHist.Reward[0][0].ToInt()
	}
	return i.GasPriceHelper(ctx, baseFee, totalGasUsed, medianRewardPrevBlock)
}

// Helper function useful for testing
func (i *InfoAPI) GasPriceHelper(ctx context.Context, baseFee *big.Int, totalGasUsedPrevBlock uint64, medianRewardPrevBlock *big.Int) (*hexutil.Big, error) {
	isChainCongested := i.isChainCongested(totalGasUsedPrevBlock)
	if !isChainCongested {
		// chain is not congested, increase base fee by 10% to get the gas price to get a tx included in a timely manner
		gasPrice := new(big.Int).Mul(baseFee, big.NewInt(110))
		gasPrice.Div(gasPrice, big.NewInt(100))
		return (*hexutil.Big)(gasPrice), nil
	}
	// chain is congested, return the 50%-tile reward as the priority fee per gas
	gasPrice := new(big.Int).Add(medianRewardPrevBlock, baseFee)
	return (*hexutil.Big)(gasPrice), nil

}

// lastBlock is inclusive
func (i *InfoAPI) FeeHistory(ctx context.Context, blockCount gmath.HexOrDecimal64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (result *FeeHistoryResult, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_feeHistory", i.connectionType, startTime, returnErr)
	result = &FeeHistoryResult{}

	// logic consistent with go-ethereum's validation (block < 1 means no block)
	if blockCount < 1 {
		return result, nil
	}

	// default go-ethereum max block history is 1024
	// https://github.com/ethereum/go-ethereum/blob/master/eth/gasprice/feehistory.go#L235
	maxBlocksD64 := gmath.HexOrDecimal64(i.maxBlocks) //nolint:gosec
	if blockCount > maxBlocksD64 {
		blockCount = maxBlocksD64
	}

	// if someone needs more than 100 reward percentiles, we can discuss, but it's not likely
	if len(rewardPercentiles) > 100 {
		return nil, errors.New("rewardPercentiles length must be less than or equal to 100")
	}

	// validate reward percentiles
	for i, p := range rewardPercentiles {
		if p < 0 || p > 100 || (i > 0 && p <= rewardPercentiles[i-1]) {
			return nil, errors.New("invalid reward percentiles: must be ascending and between 0 and 100")
		}
	}

	lastBlockNumber := lastBlock.Int64()
	genesis, err := i.tmClient.Genesis(ctx)
	if err != nil {
		return nil, err
	}
	genesisHeight := genesis.Genesis.InitialHeight
	latestHeight, err := i.latestHeight(ctx)
	if err != nil {
		return nil, err
	}
	earliestHeight, err := i.earliestHeight(ctx)
	if err != nil {
		// fall back to genesis height if earliest watermark unavailable
		earliestHeight = genesisHeight
	}
	if earliestHeight < genesisHeight {
		earliestHeight = genesisHeight
	}
	switch lastBlock {
	case rpc.SafeBlockNumber, rpc.FinalizedBlockNumber, rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		lastBlockNumber = latestHeight
	case rpc.EarliestBlockNumber:
		lastBlockNumber = earliestHeight
	default:
		if lastBlockNumber > latestHeight {
			return nil, fmt.Errorf("requested last block %d is not yet available; safe latest is %d", lastBlockNumber, latestHeight)
		}
	}

	if lastBlockNumber < earliestHeight {
		return nil, errors.New("requested last block is before earliest available height")
	}

	if uint64(lastBlockNumber-earliestHeight) < uint64(blockCount) { //nolint:gosec
		result.OldestBlock = (*hexutil.Big)(big.NewInt(earliestHeight))
	} else {
		result.OldestBlock = (*hexutil.Big)(big.NewInt(lastBlockNumber - int64(blockCount) + 1)) //nolint:gosec
	}

	result.Reward = [][]*hexutil.Big{}
	result.GasUsedRatio = []float64{}
	// Potentially parallelize the following logic
	for blockNum := result.OldestBlock.ToInt().Int64(); blockNum <= lastBlockNumber; blockNum++ {
		var gasUsedRatio float64

		sdkCtx := i.ctxProvider(blockNum)
		if CheckVersion(sdkCtx, i.keeper) != nil {
			// either height is pruned or before EVM is introduced
			// For non-EVM blocks or pruned blocks, use 0.0 as gas used ratio
			gasUsedRatio = 0.0
		} else {
			// Calculate actual gas used ratio for this block
			calculatedRatio, err := i.CalculateGasUsedRatio(ctx, blockNum)
			if err != nil {
				// If we can't calculate the ratio, use 0.0 as fallback
				sdkCtx.Logger().Error("Error calculating gas used ratio, falling back to 0.0", "error", err)
				gasUsedRatio = 0.0
			} else {
				gasUsedRatio = calculatedRatio
			}
		}
		result.GasUsedRatio = append(result.GasUsedRatio, gasUsedRatio)

		// Only continue with other fields if EVM state exists
		if CheckVersion(sdkCtx, i.keeper) != nil {
			continue
		}

		baseFee := i.safeGetBaseFee(blockNum)
		if baseFee == nil {
			// the block has been pruned
			continue
		}
		result.BaseFee = append(result.BaseFee, (*hexutil.Big)(baseFee))
		height := blockNum
		block, err := blockByNumberRespectingWatermarks(ctx, i.tmClient, i.watermarks, &height, 1)
		if err != nil {
			// block pruned from tendermint store. Skipping
			continue
		}
		rewards, err := i.getRewards(block, baseFee, rewardPercentiles)
		if err != nil {
			return nil, err
		}
		result.Reward = append(result.Reward, rewards)
	}
	return result, nil
}

func (i *InfoAPI) MaxPriorityFeePerGas(ctx context.Context) (fee *hexutil.Big, returnErr error) {
	// Checks the most recent block. If it has high gas used, it will return the reward of the 50% percentile.
	// Otherwise, since the previous block has low gas used, a user shouldn't need to tip a high amount to get included,
	// so a default value is returned.
	startTime := time.Now()
	defer recordMetricsWithError("eth_maxPriorityFeePerGas", i.connectionType, startTime, returnErr)
	totalGasUsed, err := i.getCongestionData(ctx, nil)
	if err != nil {
		return nil, err
	}
	isChainCongested := i.isChainCongested(totalGasUsed)
	if !isChainCongested {
		// chain is not congested, return 1gwei as the default priority fee per gas
		return (*hexutil.Big)(big.NewInt(defaultPriorityFeePerGas)), nil
	}
	// chain is congested, return the 50%-tile reward as the priority fee per gas
	feeHist, err := i.FeeHistory(ctx, 1, rpc.LatestBlockNumber, []float64{50})
	if err != nil {
		return nil, err
	}
	if len(feeHist.Reward) == 0 || len(feeHist.Reward[0]) == 0 {
		// if there is no EVM tx in the most recent block, return 0
		return (*hexutil.Big)(big.NewInt(0)), nil
	}
	return (*hexutil.Big)(feeHist.Reward[0][0].ToInt()), nil
}

func (i *InfoAPI) safeGetBaseFee(targetHeight int64) (res *big.Int) {
	defer func() {
		if err := recover(); err != nil {
			res = nil
		}
	}()
	baseFee := i.keeper.GetNextBaseFeePerGas(i.ctxProvider(targetHeight))
	res = baseFee.TruncateInt().BigInt()
	return
}

type GasAndReward struct {
	GasUsed uint64
	Reward  *big.Int
}

func (i *InfoAPI) getRewards(block *coretypes.ResultBlock, baseFee *big.Int, rewardPercentiles []float64) ([]*hexutil.Big, error) {
	GasAndRewards := []GasAndReward{}
	totalEVMGasUsed := uint64(0)
	for _, txbz := range block.Block.Txs {
		ethtx := getEthTxForTxBz(txbz, i.txConfigProvider(block.Block.Height).TxDecoder())
		if ethtx == nil {
			// not evm tx
			continue
		}
		// okay to get from latest since receipt is immutable
		receipt, err := i.keeper.GetReceipt(i.ctxProvider(LatestCtxHeight), ethtx.Hash())
		if err != nil {
			return nil, err
		}
		receiptEffectiveGasPrice := new(big.Int).SetUint64(receipt.EffectiveGasPrice)
		if receiptEffectiveGasPrice.Cmp(baseFee) < 0 {
			// if effective gas price is 0, it's expected behavior for txs that failed ante.
			// if it's not zero but still smaller than baseFee then something is wrong.
			if receiptEffectiveGasPrice.Cmp(common.Big0) != 0 {
				fmt.Printf("Error: tx %s has an unexpected gas price %s set on its receipt\n", ethtx.Hash().Hex(), receiptEffectiveGasPrice)
			}
			continue
		}
		reward := new(big.Int).Sub(new(big.Int).SetUint64(receipt.EffectiveGasPrice), baseFee)
		GasAndRewards = append(GasAndRewards, GasAndReward{GasUsed: receipt.GasUsed, Reward: reward})
		totalEVMGasUsed += receipt.GasUsed
	}
	return CalculatePercentiles(rewardPercentiles, GasAndRewards, totalEVMGasUsed), nil
}

func (i *InfoAPI) getCongestionData(ctx context.Context, height *int64) (blockGasUsed uint64, err error) {
	block, err := blockByNumberRespectingWatermarks(ctx, i.tmClient, i.watermarks, height, 1)
	if err != nil {
		// block pruned from tendermint store. Skipping
		return 0, err
	}
	totalEVMGasUsed := uint64(0)
	for _, txbz := range block.Block.Txs {
		ethtx := getEthTxForTxBz(txbz, i.txConfigProvider(block.Block.Height).TxDecoder())
		if ethtx == nil {
			// not evm tx
			continue
		}
		// okay to get from latest since receipt is immutable
		receipt, err := i.keeper.GetReceiptWithRetry(i.ctxProvider(LatestCtxHeight), ethtx.Hash(), 3)
		if err != nil {
			return 0, err
		}
		// We've had issues where is included in a block and fails but then is retried and included in a later block, overwriting the receipt.
		// This is a temporary fix to ensure we only consider receipts that are included in the block we're querying.
		if receipt.BlockNumber != uint64(block.Block.Height) { //nolint:gosec
			continue
		}
		totalEVMGasUsed += receipt.GasUsed
	}
	return totalEVMGasUsed, nil
}

// CalculateGasUsedRatio calculates the actual gas used ratio for a specific block
func (i *InfoAPI) CalculateGasUsedRatio(ctx context.Context, blockHeight int64) (float64, error) {
	block, err := blockByNumberRespectingWatermarks(ctx, i.tmClient, i.watermarks, &blockHeight, 1)
	if err != nil {
		return 0, err
	}

	// Get the gas limit from consensus params using the SDK context
	sdkCtx := i.ctxProvider(blockHeight)
	var gasLimit uint64
	if sdkCtx.ConsensusParams() != nil && sdkCtx.ConsensusParams().Block != nil {
		gasLimit = uint64(sdkCtx.ConsensusParams().Block.MaxGas) //nolint:gosec
	} else {
		// Fallback: try current context
		currentCtx := i.ctxProvider(LatestCtxHeight)
		if currentCtx.ConsensusParams() != nil && currentCtx.ConsensusParams().Block != nil {
			gasLimit = uint64(currentCtx.ConsensusParams().Block.MaxGas) //nolint:gosec
		} else {
			// Default fallback
			gasLimit = 10000000 // Default block gas limit for Sei
		}
	}

	if gasLimit == 0 {
		return 0, nil // Avoid division by zero
	}

	// Calculate total gas used by EVM transactions in this block
	totalEVMGasUsed := uint64(0)
	for _, txbz := range block.Block.Txs {
		ethtx := getEthTxForTxBz(txbz, i.txDecoder)
		if ethtx == nil {
			// not evm tx
			continue
		}
		// okay to get from latest since receipt is immutable
		receipt, err := i.keeper.GetReceiptWithRetry(i.ctxProvider(LatestCtxHeight), ethtx.Hash(), 3)
		if err != nil {
			return 0, err
		}
		// We've had issues where tx is included in a block and fails but then is retried and included in a later block, overwriting the receipt.
		// This is a temporary fix to ensure we only consider receipts that are included in the block we're querying.
		if receipt.BlockNumber != uint64(block.Block.Height) { //nolint:gosec
			continue
		}
		totalEVMGasUsed += receipt.GasUsed
	}

	// We want 4 decimal places, so multiply by 10000, do integer division, then divide by 10000
	// This preserves more precision during the integer calculation
	ratioInt := (totalEVMGasUsed * 10000) / gasLimit
	ratio := float64(ratioInt) / 10000.0
	return ratio, nil
}

func (i *InfoAPI) latestHeight(ctx context.Context) (int64, error) {
	return i.watermarks.LatestHeight(ctx)
}

func (i *InfoAPI) earliestHeight(ctx context.Context) (int64, error) {
	return i.watermarks.EarliestHeight(ctx)
}

// Following go-ethereum implementation
// Specifically, the reward value at a percentile of p% will be the reward value of the
// lowest-rewarded transaction such that the sum of its gasUsed value and gasUsed values
// of all lower-rewarded transactions is no less than (total gasUsed * p%).
func CalculatePercentiles(rewardPercentiles []float64, GasAndRewards []GasAndReward, totalEVMGasUsed uint64) []*hexutil.Big {
	slices.SortStableFunc(GasAndRewards, func(a, b GasAndReward) int {
		return a.Reward.Cmp(b.Reward)
	})
	res := []*hexutil.Big{}
	if len(GasAndRewards) == 0 {
		// Return array of zeros for each percentile when no transactions exist
		for range rewardPercentiles {
			res = append(res, (*hexutil.Big)(big.NewInt(0)))
		}
		return res
	}
	var txIndex int
	sumGasUsed := GasAndRewards[0].GasUsed
	for _, p := range rewardPercentiles {
		thresholdGasUsed := uint64(float64(totalEVMGasUsed) * p / 100)
		for sumGasUsed < thresholdGasUsed && txIndex < len(GasAndRewards)-1 {
			txIndex++
			sumGasUsed += GasAndRewards[txIndex].GasUsed
		}
		res = append(res, (*hexutil.Big)(GasAndRewards[txIndex].Reward))
	}
	return res
}

func (i *InfoAPI) isChainCongested(totalGasUsed uint64) bool {
	sdkCtx := i.ctxProvider(LatestCtxHeight)
	var gasLimit uint64
	if sdkCtx.ConsensusParams() != nil && sdkCtx.ConsensusParams().Block != nil {
		maxGas := sdkCtx.ConsensusParams().Block.MaxGas
		if maxGas <= 0 {
			gasLimit = uint64(DefaultBlockGasLimit)
		} else {
			gasLimit = uint64(maxGas)
		}
	} else {
		gasLimit = uint64(DefaultBlockGasLimit)
	}
	threshold := gasLimit * uint64(defaultThresholdPercentage) / 100
	return totalGasUsed > threshold
}
