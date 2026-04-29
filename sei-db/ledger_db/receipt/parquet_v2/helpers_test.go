package parquet_v2

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func testReceiptInput(blockNumber uint64, txHash common.Hash) parquet.ReceiptInput {
	receiptBytes := []byte{byte(blockNumber), txHash[31]}
	return parquet.ReceiptInput{
		BlockNumber: blockNumber,
		Receipt: parquet.ReceiptRecord{
			TxHash:       txHash[:],
			BlockNumber:  blockNumber,
			ReceiptBytes: receiptBytes,
		},
		Logs: []parquet.LogRecord{{
			BlockNumber: blockNumber,
			TxHash:      txHash[:],
			Address:     common.BigToAddress(new(big.Int).SetUint64(blockNumber)).Bytes(),
		}},
		ReceiptBytes: receiptBytes,
	}
}

func replayConverterForTest(blockNumber uint64, receiptBytes []byte, _ uint) (ReplayReceipt, error) {
	txHash := common.BigToHash(new(big.Int).SetUint64(uint64(receiptBytes[0])))
	input := testReceiptInput(blockNumber, txHash)
	input.ReceiptBytes = append([]byte(nil), receiptBytes...)
	input.Receipt.ReceiptBytes = append([]byte(nil), receiptBytes...)

	return ReplayReceipt{
		Input:    input,
		TxHash:   txHash,
		Warmup:   input.Receipt,
		LogCount: uint(len(input.Logs)),
	}, nil
}

func writeReceiptFile(t *testing.T, dir string, startBlock uint64, blocks []uint64) {
	t.Helper()

	path := filepath.Join(dir, fmt.Sprintf("receipts_%d.parquet", startBlock))
	f, err := os.Create(path)
	require.NoError(t, err)

	w := parquetgo.NewGenericWriter[parquet.ReceiptRecord](f)
	for _, block := range blocks {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		_, err := w.Write([]parquet.ReceiptRecord{{
			TxHash:       txHash[:],
			BlockNumber:  block,
			ReceiptBytes: []byte{byte(block)},
		}})
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}

func writeLogFile(t *testing.T, dir string, startBlock uint64) {
	t.Helper()

	path := filepath.Join(dir, fmt.Sprintf("logs_%d.parquet", startBlock))
	f, err := os.Create(path)
	require.NoError(t, err)

	w := parquetgo.NewGenericWriter[parquet.LogRecord](f)
	txHash := common.BigToHash(new(big.Int).SetUint64(startBlock))
	_, err = w.Write([]parquet.LogRecord{{
		BlockNumber: startBlock,
		TxHash:      txHash[:],
	}})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}

func logBlockNumbers(results []parquet.LogResult) []uint64 {
	blocks := make([]uint64, 0, len(results))
	for _, result := range results {
		blocks = append(blocks, result.BlockNumber)
	}
	return blocks
}
