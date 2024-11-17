package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

type InfoAPI struct {
	tmClient       rpcclient.Client
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	txDecoder      sdk.TxDecoder
	homeDir        string
	connectionType ConnectionType
	maxBlocks      int64
}

func NewInfoAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder, homeDir string, maxBlocks int64, connectionType ConnectionType) *InfoAPI {
	return &InfoAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder, homeDir: homeDir, connectionType: connectionType, maxBlocks: maxBlocks}
}

type FeeHistoryResult struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas,omitempty"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
}

func (i *InfoAPI) BlockNumber() hexutil.Uint64 {
	startTime := time.Now()
	defer recordMetrics("eth_BlockNumber", i.connectionType, startTime, true)
	return hexutil.Uint64(i.ctxProvider(LatestCtxHeight).BlockHeight())
}

//nolint:revive
func (i *InfoAPI) ChainId() *hexutil.Big {
	startTime := time.Now()
	defer recordMetrics("eth_ChainId", i.connectionType, startTime, true)
	return (*hexutil.Big)(i.keeper.ChainID(i.ctxProvider(LatestCtxHeight)))
}

func (i *InfoAPI) Coinbase() (common.Address, error) {
	startTime := time.Now()
	defer recordMetrics("eth_Coinbase", i.connectionType, startTime, true)
	return i.keeper.GetFeeCollectorAddress(i.ctxProvider(LatestCtxHeight))
}

func (i *InfoAPI) Accounts() (result []common.Address, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_Accounts", i.connectionType, startTime, returnErr == nil)
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
	defer recordMetrics("eth_GasPrice", i.connectionType, startTime, returnErr == nil)
	// get fee history of the most recent block with 50% reward percentile
	feeHist, err := i.FeeHistory(ctx, 1, rpc.LatestBlockNumber, []float64{0.5})
	if err != nil {
		return nil, err
	}
	if len(feeHist.Reward) == 0 || len(feeHist.Reward[0]) == 0 {
		// if there is no EVM tx in the most recent block, return the minimum fee param
		baseFee := i.keeper.GetMinimumFeePerGas(i.ctxProvider(LatestCtxHeight)).TruncateInt().BigInt()
		return (*hexutil.Big)(baseFee), nil
	}
	baseFee := i.keeper.GetDynamicBaseFeePerGas(i.ctxProvider(LatestCtxHeight)).TruncateInt().BigInt()
	sum := new(big.Int).Add(
		feeHist.Reward[0][0].ToInt(),
		baseFee,
	)
	return (*hexutil.Big)(sum), nil
}

// lastBlock is inclusive
func (i *InfoAPI) FeeHistory(ctx context.Context, blockCount math.HexOrDecimal64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (result *FeeHistoryResult, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_feeHistory", i.connectionType, startTime, returnErr == nil)
	result = &FeeHistoryResult{}

	// logic consistent with go-ethereum's validation (block < 1 means no block)
	if blockCount < 1 {
		return result, nil
	}

	// default go-ethereum max block history is 1024
	// https://github.com/ethereum/go-ethereum/blob/master/eth/gasprice/feehistory.go#L235
	if blockCount > math.HexOrDecimal64(i.maxBlocks) {
		blockCount = math.HexOrDecimal64(i.maxBlocks)
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
	currentHeight := i.ctxProvider(LatestCtxHeight).BlockHeight()
	switch lastBlock {
	case rpc.SafeBlockNumber, rpc.FinalizedBlockNumber, rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		lastBlockNumber = currentHeight
	case rpc.EarliestBlockNumber:
		lastBlockNumber = genesisHeight
	default:
		if lastBlockNumber > currentHeight {
			lastBlockNumber = currentHeight
		}
	}

	if lastBlockNumber < genesisHeight {
		return nil, errors.New("requested last block is before genesis height")
	}

	if uint64(lastBlockNumber-genesisHeight) < uint64(blockCount) {
		result.OldestBlock = (*hexutil.Big)(big.NewInt(genesisHeight))
	} else {
		result.OldestBlock = (*hexutil.Big)(big.NewInt(lastBlockNumber - int64(blockCount) + 1))
	}

	result.Reward = [][]*hexutil.Big{}
	// Potentially parallelize the following logic
	for blockNum := result.OldestBlock.ToInt().Int64(); blockNum <= lastBlockNumber; blockNum++ {
		sdkCtx := i.ctxProvider(blockNum)
		if CheckVersion(sdkCtx, i.keeper) != nil {
			// either height is pruned or before EVM is introduced. Skipping
			continue
		}
		result.GasUsedRatio = append(result.GasUsedRatio, GasUsedRatio)
		baseFee := i.safeGetBaseFee(blockNum)
		if baseFee == nil {
			// the block has been pruned
			continue
		}
		result.BaseFee = append(result.BaseFee, (*hexutil.Big)(baseFee))
		height := blockNum
		block, err := blockByNumber(ctx, i.tmClient, &height)
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

func (i *InfoAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	startTime := time.Now()
	defer recordMetrics("eth_maxPriorityFeePerGas", i.connectionType, startTime, true)
	feeHist, err := i.FeeHistory(ctx, 1, rpc.LatestBlockNumber, []float64{0.5})
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
	baseFee := i.keeper.GetDynamicBaseFeePerGas(i.ctxProvider(targetHeight))
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
		ethtx := getEthTxForTxBz(txbz, i.txDecoder)
		if ethtx == nil {
			// not evm tx
			continue
		}
		// okay to get from latest since receipt is immutable
		receipt, err := i.keeper.GetReceipt(i.ctxProvider(LatestCtxHeight), ethtx.Hash())
		if err != nil {
			return nil, err
		}
		reward := new(big.Int).Sub(new(big.Int).SetUint64(receipt.EffectiveGasPrice), baseFee)
		GasAndRewards = append(GasAndRewards, GasAndReward{GasUsed: receipt.GasUsed, Reward: reward})
		totalEVMGasUsed += receipt.GasUsed
	}
	return CalculatePercentiles(rewardPercentiles, GasAndRewards, totalEVMGasUsed), nil
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
