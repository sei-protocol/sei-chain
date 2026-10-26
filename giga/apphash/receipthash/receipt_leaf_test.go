package receipthash

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// randomReceipts returns count random receipts with strictly increasing transaction indices, leaving random gaps for
// rejected transactions.
func randomReceipts(rng *rand.Rand, count int) []*ethtypes.Receipt {
	receipts := make([]*ethtypes.Receipt, 0, count)
	blockIndex := uint(rng.Intn(3))
	for range count {
		receipt := &ethtypes.Receipt{
			Status:           uint64(rng.Intn(2)),
			TransactionIndex: blockIndex,
			GasUsed:          rng.Uint64() >> rng.Intn(64),
			Logs:             randomLogs(rng, rng.Intn(5)),
		}
		_, _ = rng.Read(receipt.TxHash[:])
		receipts = append(receipts, receipt)
		blockIndex += 1 + uint(rng.Intn(3))
	}
	return receipts
}

// randomLogs returns count random logs, each with 0 to 4 topics and 0 to 64 bytes of data.
func randomLogs(rng *rand.Rand, count int) []*ethtypes.Log {
	logs := make([]*ethtypes.Log, 0, count)
	for range count {
		log := &ethtypes.Log{
			Topics: make([]common.Hash, rng.Intn(maxLogTopics+1)),
			Data:   make([]byte, rng.Intn(65)),
		}
		_, _ = rng.Read(log.Address[:])
		for i := range log.Topics {
			_, _ = rng.Read(log.Topics[i][:])
		}
		_, _ = rng.Read(log.Data)
		logs = append(logs, log)
	}
	return logs
}

// specLogs returns the two logs of the test vectors in the receipt hash specification.
func specLogs() (*ethtypes.Log, *ethtypes.Log) {
	log0 := &ethtypes.Log{
		Address: common.BytesToAddress(bytes.Repeat([]byte{0x11}, 20)),
		Topics:  []common.Hash{common.BytesToHash(bytes.Repeat([]byte{0x22}, 32))},
		Data:    []byte{0xbe, 0xef},
	}
	log1 := &ethtypes.Log{
		Address: common.BytesToAddress(bytes.Repeat([]byte{0x33}, 20)),
		Topics:  []common.Hash{},
		Data:    []byte{},
	}
	return log0, log1
}

// specReceipts returns the three receipts of the test vectors in the receipt hash specification. Each call returns new
// receipts and logs, so a test may mutate them.
func specReceipts() []*ethtypes.Receipt {
	log0, log1 := specLogs()
	_, log1Copy := specLogs()
	return []*ethtypes.Receipt{
		{
			Status:           ethtypes.ReceiptStatusSuccessful,
			TxHash:           common.BytesToHash(bytes.Repeat([]byte{0xaa}, 32)),
			TransactionIndex: 0,
			GasUsed:          21000,
			Logs:             []*ethtypes.Log{log0, log1},
		},
		{
			Status:           ethtypes.ReceiptStatusFailed,
			TxHash:           common.BytesToHash(bytes.Repeat([]byte{0xbb}, 32)),
			TransactionIndex: 2,
			GasUsed:          250000,
			Logs:             []*ethtypes.Log{},
		},
		{
			Status:           ethtypes.ReceiptStatusSuccessful,
			TxHash:           common.BytesToHash(bytes.Repeat([]byte{0xcc}, 32)),
			TransactionIndex: 3,
			GasUsed:          50000,
			Logs:             []*ethtypes.Log{log1Copy},
		},
	}
}

func TestComputeReceiptHashRejectsInvalidReceipts(t *testing.T) {
	testCases := map[string]func(receipts []*ethtypes.Receipt) []*ethtypes.Receipt{
		"nil receipt": func(receipts []*ethtypes.Receipt) []*ethtypes.Receipt {
			receipts[1] = nil
			return receipts
		},
		"status above 1": func(receipts []*ethtypes.Receipt) []*ethtypes.Receipt {
			receipts[1].Status = 2
			return receipts
		},
		"nil log": func(receipts []*ethtypes.Receipt) []*ethtypes.Receipt {
			receipts[0].Logs[1] = nil
			return receipts
		},
		"five topics": func(receipts []*ethtypes.Receipt) []*ethtypes.Receipt {
			receipts[2].Logs[0].Topics = make([]common.Hash, maxLogTopics+1)
			return receipts
		},
		"repeated transaction index": func(receipts []*ethtypes.Receipt) []*ethtypes.Receipt {
			receipts[2].TransactionIndex = receipts[1].TransactionIndex
			return receipts
		},
		"decreasing transaction index": func(receipts []*ethtypes.Receipt) []*ethtypes.Receipt {
			receipts[0], receipts[1] = receipts[1], receipts[0]
			return receipts
		},
	}
	for name, corrupt := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := ComputeReceiptHash(corrupt(specReceipts()))
			require.Error(t, err)
		})
	}
}

func TestComputeReceiptHashAcceptsFourTopics(t *testing.T) {
	receipts := specReceipts()
	receipts[2].Logs[0].Topics = make([]common.Hash, maxLogTopics)
	_, err := ComputeReceiptHash(receipts)
	require.NoError(t, err)
}
