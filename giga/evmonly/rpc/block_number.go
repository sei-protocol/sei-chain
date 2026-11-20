package rpc

import (
	"fmt"
	"math/big"

	ethrpc "github.com/ethereum/go-ethereum/rpc"

	genesistypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/genesis"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
)

// blockNumberKind is what a requested block number names.
type blockNumberKind int

const (
	// headBlock is a tag naming the newest block: latest, safe, finalized or pending.
	headBlock blockNumberKind = iota
	// earliestBlock is the earliest tag or 0x0, naming the earliest block this node serves.
	earliestBlock
	// heightBlock is a positive block height.
	heightBlock
)

// classifyBlockNumber returns what number names. It rejects a negative number that is not a tag.
func classifyBlockNumber(number ethrpc.BlockNumber) (blockNumberKind, error) {
	switch number {
	case ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber:
		return headBlock, nil
	case ethrpc.EarliestBlockNumber, 0:
		return earliestBlock, nil
	}
	if number < 0 {
		return 0, &invalidParamsError{error: fmt.Errorf("%w: %d", errBlockTagUnsupported, number)}
	}
	return heightBlock, nil
}

// classifyFilterBound returns what a log filter bound names, with an omitted bound naming the
// head. It rejects a bound beyond int64 or a negative bound that is not a tag.
func classifyFilterBound(bound *big.Int) (blockNumberKind, error) {
	if bound == nil {
		return headBlock, nil
	}
	if !bound.IsInt64() {
		return 0, &invalidParamsError{error: fmt.Errorf("filter block number %s exceeds int64", bound)}
	}
	return classifyBlockNumber(ethrpc.BlockNumber(bound.Int64()))
}

// firstBlockHeight returns the height of the chain's first block.
func firstBlockHeight(backend Backend) int64 {
	return max(backend.GenesisInitialHeight(), genesistypes.DefaultGenesisInitialHeight)
}

// earliestRetainedHeight returns the lowest block height store retains, never below the chain's
// first block.
func earliestRetainedHeight(backend Backend, store receiptdb.ReceiptStore) int64 {
	first := firstBlockHeight(backend)
	if store == nil {
		return first
	}
	return max(first, store.EarliestVersion())
}
