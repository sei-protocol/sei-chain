package evmonly

import "github.com/sei-protocol/sei-chain/sei-db/archive"

// archiveBlock returns the finalized block as the archive stores it.
func archiveBlock(block finalizedBlock) *archive.ExecutedBlock {
	txs := make([]archive.Tx, len(block.txs))
	for i, tx := range block.txs {
		txs[i] = archive.Tx{
			Tx:      tx.Transaction(),
			Sender:  tx.Sender(),
			Receipt: block.receipts[i],
		}
	}
	return &archive.ExecutedBlock{
		Number:     block.context.Number,
		Hash:       block.context.BlockHash,
		ParentHash: block.context.ParentHash,
		Time:       block.context.Time,
		GasLimit:   block.context.GasLimit,
		GasUsed:    block.gasUsed,
		BaseFee:    block.context.BaseFee,
		Txs:        txs,
		Changes:    block.changes,
	}
}
