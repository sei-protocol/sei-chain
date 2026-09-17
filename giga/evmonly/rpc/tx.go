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
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type txAPI struct {
	backend Backend
	store   receiptpkg.ReceiptStore
}

// GetTransactionCount returns the address nonce from the current committed EVM state.
func (api *txAPI) GetTransactionCount(_ context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	if err := requireCurrentState(block); err != nil {
		return nil, err
	}
	nonce := hexutil.Uint64(api.backend.EvmTransactionCount(address))
	return &nonce, nil
}

// GetTransactionReceipt returns the finalized Ethereum receipt for hash.
func (api *txAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]any, error) {
	stored, block, err := api.lookupFinalizedTx(ctx, hash)
	if err != nil || stored == nil {
		return nil, err
	}
	index, err := rpcTransactionIndex(ctx, api.store, block, stored)
	if err != nil {
		return nil, err
	}
	visible := *stored
	visible.TransactionIndex = index
	return encodeReceipt(hash, &visible, common.BytesToHash(block.BlockID.Hash)), nil
}

// GetTransactionByHash returns hash's transaction as committed in a finalized
// block, decoded from the block's raw transaction bytes, or nil if hash is
// unknown or its block is not yet finalized. This server tracks no local
// mempool, so unlike a full Ethereum node it never returns a pending result.
func (api *txAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (*export.RPCTransaction, error) {
	stored, block, err := api.lookupFinalizedTx(ctx, hash)
	if err != nil || stored == nil {
		return nil, err
	}
	if int(stored.TransactionIndex) >= len(block.Block.Txs) {
		return nil, fmt.Errorf("receipt transaction index %d exceeds block %d transaction count %d",
			stored.TransactionIndex, stored.BlockNumber, len(block.Block.Txs))
	}
	ethtx, err := decodeBlockTx(block.Block.Txs[stored.TransactionIndex], block.Block.Height, int(stored.TransactionIndex))
	if err != nil {
		return nil, err
	}
	chainConfig, err := api.backend.EvmChainConfig()
	if err != nil {
		return nil, err
	}
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", stored.BlockNumber, block.Block.Time)
	}
	baseFee, err := api.backend.EvmBaseFee()
	if err != nil {
		return nil, err
	}
	index, err := rpcTransactionIndex(ctx, api.store, block, stored)
	if err != nil {
		return nil, err
	}
	// TODO: If the EVM-only base fee becomes dynamic, read the fee for
	// stored.BlockNumber here or persist it with the receipt. Using the current
	// fee would misreport a historical transaction's effective gas price.
	result := export.NewRPCTransaction(ethtx, common.BytesToHash(block.BlockID.Hash), stored.BlockNumber, blockUnix,
		uint64(index), baseFee, chainConfig)
	replaceFrom(result, stored)
	return result, nil
}

// lookupFinalizedTx resolves hash's stored receipt and finalized block. It
// returns a nil receipt with a nil error when hash is unknown or its block is
// not yet finalized.
func (api *txAPI) lookupFinalizedTx(ctx context.Context, hash common.Hash) (*evmtypes.Receipt, *coretypes.ResultBlock, error) {
	stored, err := api.store.GetReceipt(receiptContext(ctx), hash)
	if errors.Is(err, receiptpkg.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read transaction receipt: %w", err)
	}
	if stored == nil {
		return nil, nil, errors.New("receipt store returned a nil receipt")
	}
	if stored.BlockNumber > math.MaxInt64 {
		return nil, nil, fmt.Errorf("receipt block number %d exceeds int64", stored.BlockNumber)
	}

	height := coretypes.Int64(stored.BlockNumber)
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: &height})
	if errors.Is(err, coretypes.ErrHeightExceedsChainHead) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read receipt block %d: %w", stored.BlockNumber, err)
	}
	if block == nil || block.Block == nil {
		return nil, nil, nil
	}
	return stored, block, nil
}

// replaceFrom patches a decoded transaction's From field from its stored
// receipt when the tx's own signature did not resolve a sender, an edge case
// for some legacy transaction shapes.
func replaceFrom(tx *export.RPCTransaction, stored *evmtypes.Receipt) {
	if tx.From == (common.Address{}) {
		tx.From = common.HexToAddress(stored.From)
	}
}

// decodeBlockTx decodes the raw transaction bytes stored at index in block
// blockNumber, identifying the failing position in the returned error.
func decodeBlockTx(raw []byte, blockNumber int64, index int) (*ethtypes.Transaction, error) {
	ethtx := new(ethtypes.Transaction)
	if err := ethtx.UnmarshalBinary(raw); err != nil {
		return nil, fmt.Errorf("decode transaction at block %d index %d: %w", blockNumber, index, err)
	}
	return ethtx, nil
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
