package evmonly

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/archive"
)

// archiveBlock returns block and its result as the archive stores them. The returned block refers to
// result's receipts and changes rather than copying them.
func archiveBlock(block PreparedBlock, result *BlockResult) (*archive.ExecutedBlock, error) {
	if len(result.Txs) != len(block.Txs) || len(result.Receipts) != len(block.Txs) {
		return nil, fmt.Errorf("block has %d transactions but %d results and %d receipts",
			len(block.Txs), len(result.Txs), len(result.Receipts))
	}
	txs := make([]archive.Tx, len(block.Txs))
	for i, tx := range block.Txs {
		txs[i] = archive.Tx{
			Tx:        tx.Transaction(),
			Sender:    tx.Sender(),
			Receipt:   result.Receipts[i],
			Rejected:  result.Txs[i].Rejected,
			Locatable: claimsHash(&result.Txs[i]),
		}
	}
	return &archive.ExecutedBlock{
		Number:     block.Context.Number,
		Hash:       block.Context.BlockHash,
		ParentHash: block.Context.ParentHash,
		Time:       block.Context.Time,
		GasLimit:   block.Context.GasLimit,
		GasUsed:    result.GasUsed,
		BaseFee:    block.Context.BaseFee,
		Txs:        txs,
		Changes:    result.ChangeSet,
	}, nil
}
