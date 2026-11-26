package evmonly

import (
	"context"
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/receipthash"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
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

	records, err := receiptRecords(finalizeReceiptTestBlock(t, 11, result))

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
	require.Equal(t, uint32(0), stored.TransactionIndex)
	require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), stored.Status)
	require.Equal(t, sender.Hex(), stored.From)
	require.Equal(t, recipient.Hex(), stored.To)
	require.Equal(t, vmErr.Error(), stored.VmError)
	require.Equal(t, ethReceipt.Bloom[:], stored.LogsBloom)
	require.Len(t, stored.Logs, 1)
	require.Equal(t, recipient.Hex(), stored.Logs[0].Address)
	require.Equal(t, []string{topic.Hex()}, stored.Logs[0].Topics)
	require.Equal(t, []byte{7}, stored.Logs[0].Data)
	require.Equal(t, uint32(0), stored.Logs[0].Index)
	// Reward is the tx's effective gas price unfiltered: giga's base fee is always zero.
	require.Equal(t, big.NewInt(9), records[0].Reward)
}

func TestReceiptRecordsLeaveRewardNilWithoutAnEffectiveGasPrice(t *testing.T) {
	txHash := common.Hash{1}
	ethReceipt := &ethtypes.Receipt{TxHash: txHash, GasUsed: 1}
	result := &BlockResult{
		Receipts: ethtypes.Receipts{ethReceipt},
		Txs:      []TxResult{{Hash: txHash, Sender: common.Address{2}}},
	}

	records, err := receiptRecords(finalizeReceiptTestBlock(t, 1, result))
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Nil(t, records[0].Reward)
}

func TestReceiptRecordsRejectInvalidStatus(t *testing.T) {
	block := finalizeReceiptTestBlock(t, 1, &BlockResult{
		Receipts: ethtypes.Receipts{{Status: uint64(math.MaxUint32) + 1}},
		Txs:      []TxResult{{}},
	})
	_, err := receiptRecords(block)
	require.ErrorContains(t, err, "status")
}

// The pooled and serial paths must encode the same finalized transactions and indexes.
func TestParallelReceiptRecordsMatchSerial(t *testing.T) {
	const txs = 3 * occParallelReceiptThreshold
	result := &BlockResult{}
	rejected := 0
	for i := range txs {
		hash := common.BigToHash(big.NewInt(int64(i) + 1))
		result.Receipts = append(result.Receipts, &ethtypes.Receipt{
			TxHash:            hash,
			Status:            ethtypes.ReceiptStatusSuccessful,
			EffectiveGasPrice: big.NewInt(1),
			TransactionIndex:  uint(i),
		})
		tx := TxResult{Hash: hash, Sender: common.Address{byte(i)}, Status: ethtypes.ReceiptStatusSuccessful}
		switch i % 3 {
		case 1:
			tx.Rejected, tx.Err = true, core.ErrNonceTooHigh
			rejected++
		case 2:
			tx.Rejected, tx.Err = true, core.ErrNonceTooLow
			rejected++
		}
		result.Txs = append(result.Txs, tx)
	}
	executor := NewExecutor(Config{OCCWorkers: 4})
	defer executor.Close()

	block := finalizeReceiptTestBlock(t, 7, result)
	serial, err := receiptRecords(block)
	require.NoError(t, err)
	parallel, err := executor.receiptRecordsParallel(t.Context(), block)
	require.NoError(t, err)

	require.Equal(t, serial, parallel)
	require.Len(t, parallel, txs-rejected, "rejected attempts must store no receipts")
	for _, record := range parallel {
		require.NotEqual(t, core.ErrNonceTooHigh.Error(), record.Receipt.VmError)
	}
}

// The receipt hash submitted for a block commits to the receipts of every transaction but the rejected ones.
func TestExecutorSubmitsTheReceiptsOfNonceConsumingTransactions(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa6)
	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(testFundedBalanceWei))

	type report struct {
		blockHeight uint64
		receiptHash apphash.ReceiptHash
	}
	reported := make(chan report, 1)
	hasher, err := receipthash.NewReceiptHasher(t.Context(), receipthash.DefaultReceiptHasherConfig(),
		func(_ context.Context, blockHeight uint64, receiptHash apphash.ReceiptHash) error {
			reported <- report{blockHeight: blockHeight, receiptHash: receiptHash}
			return nil
		})
	require.NoError(t, err)
	executor := NewExecutor(Config{}, withTestState(state))
	executor.receiptHasher = hasher

	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
		Context:    blockContext(chainID),
		TxsOrdered: true,
		Txs: []abevm.RawTx{
			signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(5), nil),
			signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(9), nil),
			signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(6), nil),
		},
	})
	require.NoError(t, err)
	defer result.Release()
	require.NoError(t, hasher.Close())

	require.True(t, result.Txs[1].Rejected)
	second := *result.Receipts[2]
	second.TransactionIndex = 1
	want, err := receipthash.ComputeReceiptHash([]*ethtypes.Receipt{result.Receipts[0], &second})
	require.NoError(t, err)
	require.Equal(t, report{blockHeight: 1, receiptHash: want}, <-reported)
}

func finalizeReceiptTestBlock(t *testing.T, number uint64, result *BlockResult) finalizedBlock {
	t.Helper()
	input := OrderedBlock{Context: BlockContext{Number: number}}
	for i, tx := range result.Txs {
		input.Txs = append(input.Txs, abevm.TestEvmTx(ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: uint64(i)}), tx.Sender))
	}
	block, err := finalizeBlock(input, result)
	require.NoError(t, err)
	return block
}
