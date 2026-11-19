// Package archive defines the history archive a Giga node writes its executed blocks to, and the
// block types an archive stores.
package archive

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// ArchivalStore is a history archive of executed blocks.
type ArchivalStore interface {
	// ArchiveBlock stores block, which must follow the block given before it, or LatestBlock when it
	// is the first; any other block is refused with an error. A store that has not started yet starts
	// at the first block it is given. It keeps no reference to block or anything block refers to after
	// it returns.
	ArchiveBlock(ctx context.Context, block *ExecutedBlock) error
	// LatestBlock returns the newest block the store holds, or for a store that holds none yet, the
	// block before the first it was given. It returns 0 when neither is known.
	LatestBlock() uint64
	// Close stores the blocks the store still holds and releases it.
	Close() error
}

// ExecutedBlock is a block and the result of executing it, as an archive stores it.
type ExecutedBlock struct {
	Number     uint64
	Hash       common.Hash
	ParentHash common.Hash
	Time       uint64
	GasLimit   uint64
	GasUsed    uint64
	BaseFee    *big.Int
	Txs        []Tx
	Changes    gigatypes.StateChangeSet
}

// Tx is a transaction of an ExecutedBlock with its receipt, which holds its hash and any contract
// it created.
type Tx struct {
	Tx      *ethtypes.Transaction
	Sender  common.Address
	Receipt *ethtypes.Receipt
	// Rejected reports that the executor did not run the transaction.
	Rejected bool
	// Locatable reports that the transaction is found by its hash. A hash found at several
	// locations resolves to the earliest.
	Locatable bool
}
