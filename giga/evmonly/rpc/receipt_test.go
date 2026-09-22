package rpc

import (
	"context"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestGetTransactionReceipt(t *testing.T) {
	txHash := common.HexToHash("0x1234")
	blockHash := common.HexToHash("0xabcd")
	sender := common.HexToAddress("0x1000000000000000000000000000000000000001")
	recipient := common.HexToAddress("0x2000000000000000000000000000000000000002")
	logAddress := common.HexToAddress("0x3000000000000000000000000000000000000003")
	topic := common.HexToHash("0x4567")
	bloom := ethtypes.Bloom{9}
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: txHash,
		Receipt: &evmtypes.Receipt{
			TxType:            uint32(ethtypes.DynamicFeeTxType),
			CumulativeGasUsed: 43_000,
			TxHashHex:         txHash.Hex(),
			GasUsed:           22_000,
			EffectiveGasPrice: 17,
			BlockNumber:       7,
			TransactionIndex:  2,
			Status:            uint32(ethtypes.ReceiptStatusSuccessful),
			From:              sender.Hex(),
			To:                recipient.Hex(),
			Logs: []*evmtypes.Log{{
				Address: logAddress.Hex(),
				Topics:  []string{topic.Hex()},
				Data:    []byte{4, 5, 6},
				Index:   3,
			}},
			LogsBloom: bloom.Bytes(),
		},
	}}))

	backend := &testBackend{
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			require.NotNil(t, req.Height)
			require.Equal(t, coretypes.Int64(7), *req.Height)
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
				Block:   &tmtypes.Block{},
			}, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	handler, err := newHandler(backend, store)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got struct {
		BlockHash         common.Hash     `json:"blockHash"`
		BlockNumber       hexutil.Uint64  `json:"blockNumber"`
		ContractAddress   *common.Address `json:"contractAddress"`
		CumulativeGasUsed hexutil.Uint64  `json:"cumulativeGasUsed"`
		EffectiveGasPrice *hexutil.Big    `json:"effectiveGasPrice"`
		From              common.Address  `json:"from"`
		GasUsed           hexutil.Uint64  `json:"gasUsed"`
		Logs              []*ethtypes.Log `json:"logs"`
		LogsBloom         ethtypes.Bloom  `json:"logsBloom"`
		Status            hexutil.Uint64  `json:"status"`
		To                *common.Address `json:"to"`
		TransactionHash   common.Hash     `json:"transactionHash"`
		TransactionIndex  hexutil.Uint64  `json:"transactionIndex"`
		Type              hexutil.Uint64  `json:"type"`
	}
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getTransactionReceipt", txHash))
	require.Equal(t, blockHash, got.BlockHash)
	require.Equal(t, hexutil.Uint64(7), got.BlockNumber)
	require.Nil(t, got.ContractAddress)
	require.Equal(t, hexutil.Uint64(43_000), got.CumulativeGasUsed)
	require.Equal(t, big.NewInt(17), got.EffectiveGasPrice.ToInt())
	require.Equal(t, sender, got.From)
	require.Equal(t, hexutil.Uint64(22_000), got.GasUsed)
	require.Equal(t, bloom, got.LogsBloom)
	require.Equal(t, hexutil.Uint64(ethtypes.ReceiptStatusSuccessful), got.Status)
	require.NotNil(t, got.To)
	require.Equal(t, recipient, *got.To)
	require.Equal(t, txHash, got.TransactionHash)
	require.Equal(t, hexutil.Uint64(2), got.TransactionIndex)
	require.Equal(t, hexutil.Uint64(ethtypes.DynamicFeeTxType), got.Type)
	require.Len(t, got.Logs, 1)
	require.Equal(t, logAddress, got.Logs[0].Address)
	require.Equal(t, []common.Hash{topic}, got.Logs[0].Topics)
	require.Equal(t, []byte{4, 5, 6}, got.Logs[0].Data)
	require.Equal(t, uint64(7), got.Logs[0].BlockNumber)
	require.Equal(t, blockHash, got.Logs[0].BlockHash)
	require.Equal(t, txHash, got.Logs[0].TxHash)
	require.Equal(t, uint(2), got.Logs[0].TxIndex)
	require.Equal(t, uint(3), got.Logs[0].Index)

	var missing map[string]any
	require.NoError(t, client.CallContext(t.Context(), &missing, "eth_getTransactionReceipt", common.Hash{9}))
	require.Nil(t, missing)
}

func TestGetTransactionReceiptReturnsNullBeforeBlockCommit(t *testing.T) {
	txHash := common.Hash{1}
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: txHash,
		Receipt: &evmtypes.Receipt{
			TxHashHex:   txHash.Hex(),
			BlockNumber: 7,
		},
	}}))
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			return nil, fmt.Errorf("%w: 7", coretypes.ErrHeightExceedsChainHead)
		},
	}

	got, err := (&receiptAPI{backend: backend, store: store}).GetTransactionReceipt(t.Context(), txHash)

	require.NoError(t, err)
	require.Nil(t, got)
}

func TestHandlerRequiresReceiptStore(t *testing.T) {
	_, err := newHandler(&testBackend{}, nil)
	require.EqualError(t, err, "EVM-only RPC requires a receipt store")
}
