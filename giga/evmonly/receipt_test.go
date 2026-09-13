package evmonly

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestReceiptRecordsConvertExecutorResults(t *testing.T) {
	txHash := common.Hash{1}
	sender := common.Address{2}
	recipient := common.Address{3}
	contract := common.Address{4}
	topic := common.Hash{5}
	vmErr := errors.New("execution reverted")
	ethReceipt := &ethtypes.Receipt{
		Type:              ethtypes.DynamicFeeTxType,
		Status:            ethtypes.ReceiptStatusFailed,
		CumulativeGasUsed: 43_000,
		Bloom:             ethtypes.Bloom{6},
		Logs: []*ethtypes.Log{{
			Address: recipient,
			Topics:  []common.Hash{topic},
			Data:    []byte{7},
			Index:   8,
		}},
		TxHash:            txHash,
		ContractAddress:   contract,
		GasUsed:           22_000,
		EffectiveGasPrice: big.NewInt(9),
		TransactionIndex:  10,
	}
	result := &BlockResult{
		Receipts: ethtypes.Receipts{ethReceipt},
		Txs: []TxResult{{
			Hash:            txHash,
			Sender:          sender,
			To:              &recipient,
			ContractAddress: contract,
			Err:             vmErr,
		}},
	}

	records, err := receiptRecords(11, result)

	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, txHash, records[0].TxHash)
	stored := records[0].Receipt
	require.Equal(t, uint32(ethtypes.DynamicFeeTxType), stored.TxType)
	require.Equal(t, uint64(43_000), stored.CumulativeGasUsed)
	require.Equal(t, contract.Hex(), stored.ContractAddress)
	require.Equal(t, txHash.Hex(), stored.TxHashHex)
	require.Equal(t, uint64(22_000), stored.GasUsed)
	require.Equal(t, uint64(9), stored.EffectiveGasPrice)
	require.Equal(t, uint64(11), stored.BlockNumber)
	require.Equal(t, uint32(10), stored.TransactionIndex)
	require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), stored.Status)
	require.Equal(t, sender.Hex(), stored.From)
	require.Equal(t, recipient.Hex(), stored.To)
	require.Equal(t, vmErr.Error(), stored.VmError)
	require.Equal(t, ethReceipt.Bloom[:], stored.LogsBloom)
	require.Len(t, stored.Logs, 1)
	require.Equal(t, recipient.Hex(), stored.Logs[0].Address)
	require.Equal(t, []string{topic.Hex()}, stored.Logs[0].Topics)
	require.Equal(t, []byte{7}, stored.Logs[0].Data)
	require.Equal(t, uint32(8), stored.Logs[0].Index)
}

func TestReceiptRecordsRejectMalformedBlockResult(t *testing.T) {
	_, err := receiptRecords(1, &BlockResult{Receipts: ethtypes.Receipts{{}}})
	require.ErrorContains(t, err, "does not match")

	_, err = receiptRecords(1, &BlockResult{Receipts: ethtypes.Receipts{nil}, Txs: []TxResult{{}}})
	require.ErrorContains(t, err, "receipt 0 is nil")

	_, err = receiptRecords(1, &BlockResult{
		Receipts: ethtypes.Receipts{{Status: uint64(math.MaxUint32) + 1}},
		Txs:      []TxResult{{}},
	})
	require.ErrorContains(t, err, "status")
}
