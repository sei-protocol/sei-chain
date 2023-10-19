package evmrpc

import (
	"context"
	"errors"
	"math/big"
	"slices"

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
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	txDecoder   sdk.TxDecoder
}

func NewInfoAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder) *InfoAPI {
	return &InfoAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder}
}

type FeeHistoryResult struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas,omitempty"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
}

func (i *InfoAPI) BlockNumber() hexutil.Uint64 {
	return hexutil.Uint64(i.ctxProvider(LatestCtxHeight).BlockHeight())
}

//nolint:revive
func (i *InfoAPI) ChainId() *hexutil.Big {
	return (*hexutil.Big)(i.keeper.ChainID())
}

func (i *InfoAPI) Coinbase() (common.Address, error) {
	return i.keeper.GetFeeCollectorAddress(i.ctxProvider(LatestCtxHeight))
}

func (i *InfoAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	// get fee history of the most recent block with 50% reward percentile
	feeHist, err := i.FeeHistory(ctx, 1, rpc.LatestBlockNumber, []float64{0.5})
	if err != nil {
		return nil, err
	}
	if len(feeHist.Reward) == 0 || len(feeHist.Reward[0]) == 0 {
		// if there is no EVM tx in the most recent block, return the minimum fee param
		return (*hexutil.Big)(i.keeper.GetMinimumFeePerGas(i.ctxProvider(LatestCtxHeight)).RoundInt().BigInt()), nil
	}
	return (*hexutil.Big)(new(big.Int).Add(
		feeHist.Reward[0][0].ToInt(),
		i.keeper.GetBaseFeePerGas(i.ctxProvider(LatestCtxHeight)).RoundInt().BigInt(),
	)), nil
}

// lastBlock is inclusive
func (i *InfoAPI) FeeHistory(ctx context.Context, blockCount math.HexOrDecimal64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	result := FeeHistoryResult{}

	// validate reward percentiles
	for i, p := range rewardPercentiles {
		if p < 0 || p > 100 || (i > 0 && p < rewardPercentiles[i-1]) {
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

	// Potentially parallelize the following logic
	for blockNum := result.OldestBlock.ToInt().Int64(); blockNum <= lastBlockNumber; blockNum++ {
		result.GasUsedRatio = append(result.GasUsedRatio, GasUsedRatio)
		sdkCtx := i.ctxProvider(blockNum)
		baseFee := i.keeper.GetBaseFeePerGas(sdkCtx).BigInt()
		result.BaseFee = append(result.BaseFee, (*hexutil.Big)(baseFee))
		height := blockNum
		block, err := i.tmClient.Block(ctx, &height)
		if err != nil {
			return nil, err
		}
		rewards, err := i.getRewards(block, baseFee, rewardPercentiles)
		if err != nil {
			return nil, err
		}
		result.Reward = append(result.Reward, rewards)
	}
	return &result, nil
}

type gasAndReward struct {
	gasUsed uint64
	reward  uint64
}

func (i *InfoAPI) getRewards(block *coretypes.ResultBlock, baseFee *big.Int, rewardPercentiles []float64) ([]*hexutil.Big, error) {
	gasAndRewards := []gasAndReward{}
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
		gasAndRewards = append(gasAndRewards, gasAndReward{gasUsed: receipt.GasUsed, reward: receipt.EffectiveGasPrice - baseFee.Uint64()})
		totalEVMGasUsed += receipt.GasUsed
	}
	return calculatePercentiles(rewardPercentiles, gasAndRewards, totalEVMGasUsed), nil
}

// Following go-ethereum implementation
// Specifically, the reward value at a percentile of p% will be the reward value of the
// lowest-rewarded transaction such that the sum of its gasUsed value and gasUsed values
// of all lower-rewarded transactions is no less than (total gasUsed * p%).
func calculatePercentiles(rewardPercentiles []float64, gasAndRewards []gasAndReward, totalEVMGasUsed uint64) []*hexutil.Big {
	slices.SortStableFunc(gasAndRewards, func(a, b gasAndReward) int {
		return int(a.reward) - int(b.reward)
	})
	res := []*hexutil.Big{}
	if len(gasAndRewards) == 0 {
		return res
	}
	var txIndex int
	sumGasUsed := gasAndRewards[0].gasUsed
	for _, p := range rewardPercentiles {
		thresholdGasUsed := uint64(float64(totalEVMGasUsed) * p / 100)
		for sumGasUsed < thresholdGasUsed && txIndex < len(gasAndRewards)-1 {
			txIndex++
			sumGasUsed += gasAndRewards[txIndex].gasUsed
		}
		res = append(res, (*hexutil.Big)(big.NewInt(int64(gasAndRewards[txIndex].reward))))
	}
	return res
}
