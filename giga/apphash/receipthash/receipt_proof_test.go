package receipthash

import (
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestProofsRecomputeReceiptHash(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for count := 0; count <= 17; count++ {
		receipts := randomReceipts(rng, count)
		receiptHash := receiptHashOf(t, receipts)

		for i, receipt := range receipts {
			proof, err := BuildReceiptProof(receipts, i)
			require.NoError(t, err, "count %d receipt %d", count, i)
			require.Equal(t, receiptHash, proof.ComputeReceiptHash(), "count %d receipt %d", count, i)
			require.Equal(t, receipt.TxHash, proof.TxHash())
			require.Equal(t, uint64(receipt.TransactionIndex), proof.BlockIndex())
			require.Equal(t, uint8(receipt.Status), proof.Status())
			require.Equal(t, receipt.GasUsed, proof.GasUsed())
			require.Equal(t, uint64(len(receipt.Logs)), proof.LogCount())
			require.Equal(t, uint64(i), proof.Index())

			for j, log := range receipt.Logs {
				logProof, err := BuildLogProof(receipts, i, j)
				require.NoError(t, err, "count %d receipt %d log %d", count, i, j)
				require.Equal(t, receiptHash, logProof.ComputeReceiptHash(),
					"count %d receipt %d log %d", count, i, j)
				require.Equal(t, log.Address, logProof.Address())
				require.Equal(t, log.Topics, logProof.Topics())
				require.Equal(t, log.Data, logProof.Data())
				require.Equal(t, uint64(j), logProof.LogIndex())
				require.Equal(t, receipt.TxHash, logProof.TxHash())
				require.Equal(t, uint64(receipt.TransactionIndex), logProof.BlockIndex())
			}
		}
	}
}

func TestReceiptProofCommitsToEveryField(t *testing.T) {
	receipts := specReceipts()
	receiptHash := receiptHashOf(t, receipts)

	mutations := map[string]func(proof *ReceiptProof){
		"status":      func(proof *ReceiptProof) { proof.fields.status ^= 1 },
		"tx hash":     func(proof *ReceiptProof) { proof.fields.txHash[0] ^= 0x01 },
		"block index": func(proof *ReceiptProof) { proof.fields.blockIndex++ },
		"gas used":    func(proof *ReceiptProof) { proof.fields.gasUsed++ },
		"log count":   func(proof *ReceiptProof) { proof.fields.logCount++ },
		"logs root":   func(proof *ReceiptProof) { proof.logsRoot[0] ^= 0x01 },
		"index":       func(proof *ReceiptProof) { proof.path.index = 1 },
		"sibling":     func(proof *ReceiptProof) { proof.path.siblings[0][0] ^= 0x01 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			proof, err := BuildReceiptProof(receipts, 0)
			require.NoError(t, err)
			mutate(proof)
			require.NotEqual(t, receiptHash, proof.ComputeReceiptHash())
		})
	}
}

func TestLogProofCommitsToEveryField(t *testing.T) {
	receipts := specReceipts()
	receiptHash := receiptHashOf(t, receipts)

	mutations := map[string]func(proof *LogProof){
		"address":          func(proof *LogProof) { proof.address[0] ^= 0x01 },
		"topic":            func(proof *LogProof) { proof.topics[0][0] ^= 0x01 },
		"zero topic added": func(proof *LogProof) { proof.topics = append(proof.topics, common.Hash{}) },
		"data":             func(proof *LogProof) { proof.data[0] ^= 0x01 },
		"log index":        func(proof *LogProof) { proof.logPath.index = 1 },
		"log sibling":      func(proof *LogProof) { proof.logPath.siblings[0][0] ^= 0x01 },
		"status":           func(proof *LogProof) { proof.receiptFields.status ^= 1 },
		"tx hash":          func(proof *LogProof) { proof.receiptFields.txHash[0] ^= 0x01 },
		"block index":      func(proof *LogProof) { proof.receiptFields.blockIndex++ },
		"gas used":         func(proof *LogProof) { proof.receiptFields.gasUsed++ },
		"receipt index":    func(proof *LogProof) { proof.receiptPath.index = 1 },
		"receipt sibling":  func(proof *LogProof) { proof.receiptPath.siblings[0][0] ^= 0x01 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			proof, err := BuildLogProof(receipts, 0, 0)
			require.NoError(t, err)
			mutate(proof)
			require.NotEqual(t, receiptHash, proof.ComputeReceiptHash())
		})
	}
}

func TestBuildProofsRejectOutOfRangeIndices(t *testing.T) {
	receipts := specReceipts()
	for _, index := range []int{-1, len(receipts)} {
		_, err := BuildReceiptProof(receipts, index)
		require.Error(t, err, "receipt %d", index)
		_, err = BuildLogProof(receipts, index, 0)
		require.Error(t, err, "receipt %d", index)
	}
	for _, logIndex := range []int{-1, len(receipts[0].Logs)} {
		_, err := BuildLogProof(receipts, 0, logIndex)
		require.Error(t, err, "log %d", logIndex)
	}
	_, err := BuildLogProof(receipts, 1, 0)
	require.Error(t, err, "receipt without logs")
}

func TestBuildProofsRejectInvalidReceipts(t *testing.T) {
	receipts := specReceipts()
	receipts[1].Status = 2
	_, err := BuildReceiptProof(receipts, 0)
	require.Error(t, err)
	_, err = BuildLogProof(receipts, 0, 0)
	require.Error(t, err)
}

func TestZeroProofsDoNotPanic(t *testing.T) {
	var proof ReceiptProof
	_ = proof.ComputeReceiptHash()
	var logProof LogProof
	_ = logProof.ComputeReceiptHash()
}
