package receipthash

import (
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// receiptHashOf returns ComputeReceiptHash(), failing the test on an error.
func receiptHashOf(t *testing.T, receipts []*ethtypes.Receipt) apphash.ReceiptHash {
	t.Helper()

	receiptHash, err := ComputeReceiptHash(receipts)
	require.NoError(t, err)
	return receiptHash
}

func TestReceiptHashOfNoReceipts(t *testing.T) {
	require.Equal(t, computeReceiptHash(0, sha256.Sum256(nil)), receiptHashOf(t, nil))
	require.Equal(t, computeReceiptHash(0, sha256.Sum256(nil)), receiptHashOf(t, []*ethtypes.Receipt{}))
}

func TestReceiptHashCommitsToEveryField(t *testing.T) {
	original := receiptHashOf(t, specReceipts())

	mutations := map[string]func(receipts []*ethtypes.Receipt){
		"status": func(receipts []*ethtypes.Receipt) {
			receipts[1].Status = ethtypes.ReceiptStatusSuccessful
		},
		"tx hash": func(receipts []*ethtypes.Receipt) {
			receipts[1].TxHash[31] ^= 0x01
		},
		"transaction index": func(receipts []*ethtypes.Receipt) {
			receipts[1].TransactionIndex = 1
		},
		"gas used": func(receipts []*ethtypes.Receipt) {
			receipts[1].GasUsed++
		},
		"log added": func(receipts []*ethtypes.Receipt) {
			receipts[1].Logs = append(receipts[1].Logs, &ethtypes.Log{})
		},
		"log removed": func(receipts []*ethtypes.Receipt) {
			receipts[0].Logs = receipts[0].Logs[:1]
		},
		"log order": func(receipts []*ethtypes.Receipt) {
			receipts[0].Logs[0], receipts[0].Logs[1] = receipts[0].Logs[1], receipts[0].Logs[0]
		},
		"log address": func(receipts []*ethtypes.Receipt) {
			receipts[0].Logs[0].Address[19] ^= 0x01
		},
		"log topic": func(receipts []*ethtypes.Receipt) {
			receipts[0].Logs[0].Topics[0][31] ^= 0x01
		},
		"zero topic added": func(receipts []*ethtypes.Receipt) {
			receipts[0].Logs[0].Topics = append(receipts[0].Logs[0].Topics, common.Hash{})
		},
		"log data": func(receipts []*ethtypes.Receipt) {
			receipts[0].Logs[0].Data[1] ^= 0x01
		},
		"log data truncated": func(receipts []*ethtypes.Receipt) {
			receipts[0].Logs[0].Data = receipts[0].Logs[0].Data[:1]
		},
		"receipt order": func(receipts []*ethtypes.Receipt) {
			receipts[0].TxHash, receipts[1].TxHash = receipts[1].TxHash, receipts[0].TxHash
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			receipts := specReceipts()
			mutate(receipts)
			require.NotEqual(t, original, receiptHashOf(t, receipts))
		})
	}

	t.Run("count", func(t *testing.T) {
		require.NotEqual(t, original, receiptHashOf(t, specReceipts()[:2]))
	})
}

func TestReceiptHashIgnoresUncommittedFields(t *testing.T) {
	original := receiptHashOf(t, specReceipts())

	receipts := specReceipts()
	for _, receipt := range receipts {
		receipt.Type = ethtypes.DynamicFeeTxType
		receipt.CumulativeGasUsed = 1 << 40
		receipt.Bloom[0] = 0xff
		receipt.ContractAddress[0] = 0xff
		receipt.EffectiveGasPrice = big.NewInt(7)
		receipt.BlockHash[0] = 0xff
		receipt.BlockNumber = big.NewInt(9)
		for _, log := range receipt.Logs {
			log.BlockNumber = 9
			log.TxHash[0] = 0xff
			log.TxIndex = 5
			log.Index = 11
		}
	}
	require.Equal(t, original, receiptHashOf(t, receipts))
}
