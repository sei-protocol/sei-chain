package rpc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type receiptAPI struct {
	backend Backend
	store   receiptpkg.ReceiptStore
}

// GetTransactionReceipt returns the finalized Ethereum receipt for hash.
func (api *receiptAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]any, error) {
	stored, err := api.store.GetReceipt(receiptContext(ctx), hash)
	if errors.Is(err, receiptpkg.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read transaction receipt: %w", err)
	}
	if stored == nil {
		return nil, errors.New("receipt store returned a nil receipt")
	}
	if stored.BlockNumber > math.MaxInt64 {
		return nil, fmt.Errorf("receipt block number %d exceeds int64", stored.BlockNumber)
	}

	height := coretypes.Int64(stored.BlockNumber)
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: &height})
	if errors.Is(err, coretypes.ErrHeightExceedsChainHead) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read receipt block %d: %w", stored.BlockNumber, err)
	}
	if block == nil || block.Block == nil {
		return nil, nil
	}
	return encodeReceipt(hash, stored, common.BytesToHash(block.BlockID.Hash)), nil
}

func encodeReceipt(hash common.Hash, stored *evmtypes.Receipt, blockHash common.Hash) map[string]any {
	logs := make([]*ethtypes.Log, 0, len(stored.Logs))
	for _, storedLog := range stored.Logs {
		if storedLog == nil {
			continue
		}
		topics := make([]common.Hash, len(storedLog.Topics))
		for i, topic := range storedLog.Topics {
			topics[i] = common.HexToHash(topic)
		}
		logs = append(logs, &ethtypes.Log{
			Address:     common.HexToAddress(storedLog.Address),
			Topics:      topics,
			Data:        append([]byte(nil), storedLog.Data...),
			BlockNumber: stored.BlockNumber,
			TxHash:      hash,
			TxIndex:     uint(stored.TransactionIndex),
			BlockHash:   blockHash,
			Index:       uint(storedLog.Index),
		})
	}

	bloom := ethtypes.Bloom{}
	bloom.SetBytes(stored.LogsBloom)
	effectiveGasPrice := new(big.Int).SetUint64(stored.EffectiveGasPrice)

	var contractAddress *common.Address
	if stored.ContractAddress != "" {
		address := common.HexToAddress(stored.ContractAddress)
		contractAddress = &address
	}
	var to *common.Address
	if stored.To != "" {
		address := common.HexToAddress(stored.To)
		to = &address
	}

	return map[string]any{
		"blockHash":         blockHash,
		"blockNumber":       hexutil.Uint64(stored.BlockNumber),
		"contractAddress":   contractAddress,
		"cumulativeGasUsed": hexutil.Uint64(stored.CumulativeGasUsed),
		"effectiveGasPrice": (*hexutil.Big)(effectiveGasPrice),
		"from":              common.HexToAddress(stored.From),
		"gasUsed":           hexutil.Uint64(stored.GasUsed),
		"logs":              logs,
		"logsBloom":         bloom,
		"status":            hexutil.Uint64(stored.Status),
		"to":                to,
		"transactionHash":   hash,
		"transactionIndex":  hexutil.Uint64(stored.TransactionIndex),
		"type":              hexutil.Uint64(stored.TxType),
	}
}

func receiptContext(ctx context.Context) sdk.Context {
	return sdk.Context{}.WithContext(ctx)
}
