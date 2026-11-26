package evmonly

import (
	"fmt"
	"slices"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
)

// finalizedBlock contains the transactions included in an executed EVM block.
type finalizedBlock struct {
	context  BlockContext
	txs      []abevm.EvmTx
	results  []TxResult
	receipts ethtypes.Receipts
	gasUsed  uint64
	changes  StateChangeSet
}

// finalizeBlock excludes rejected attempts and assigns final transaction and log indexes.
func finalizeBlock(block OrderedBlock, result *BlockResult) (finalizedBlock, error) {
	if len(result.Txs) != len(block.Txs) || len(result.Receipts) != len(block.Txs) {
		return finalizedBlock{}, fmt.Errorf("block has %d transactions but %d results and %d receipts",
			len(block.Txs), len(result.Txs), len(result.Receipts))
	}
	finalized := finalizedBlock{
		context:  block.Context,
		txs:      make([]abevm.EvmTx, 0, len(block.Txs)),
		results:  make([]TxResult, 0, len(block.Txs)),
		receipts: make(ethtypes.Receipts, 0, len(block.Txs)),
		gasUsed:  result.GasUsed,
		changes:  result.ChangeSet,
	}
	var logIndex uint
	for i, tx := range block.Txs {
		if result.Txs[i].Rejected {
			continue
		}
		if result.Receipts[i] == nil {
			return finalizedBlock{}, fmt.Errorf("receipt %d is nil", i)
		}
		// Execution diagnostics retain their original positions and own receipts and logs.
		receipt := *result.Receipts[i]
		receipt.TransactionIndex = uint(len(finalized.txs)) //nolint:gosec // a slice length is non-negative.
		receipt.Logs = slices.Clone(receipt.Logs)
		for j, original := range result.Receipts[i].Logs {
			log := *original
			log.TxIndex = receipt.TransactionIndex
			log.Index = logIndex
			receipt.Logs[j] = &log
			logIndex++
		}
		txResult := result.Txs[i]
		txResult.Logs = receipt.Logs
		finalized.txs = append(finalized.txs, tx)
		finalized.results = append(finalized.results, txResult)
		finalized.receipts = append(finalized.receipts, &receipt)
	}
	return finalized, nil
}
