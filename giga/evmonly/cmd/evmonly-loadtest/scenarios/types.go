package scenarios

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

const (
	WorkloadTransfer       = "transfer"
	WorkloadERC20Transfer  = "erc20-transfer"
	WorkloadSnapshotRevert = "snapshot-revert"

	DefaultGenesisTimestamp = uint64(1_700_000_000)
)

type Config struct {
	TxsPerBlock            int
	ChainID                *big.Int
	GasPrice               *big.Int
	SenderBalance          *big.Int
	TransferValue          *big.Int
	TxGasLimit             uint64
	BlockGasLimit          uint64
	Coinbase               common.Address
	ERC20Contract          common.Address
	SnapshotRevertContract common.Address
	SnapshotRevertHelper   common.Address
	FixedRecipient         *common.Address
	RecipientConflictRate  float64
	SameSender             bool

	// FirstBlockHeight is the height of the first block a run builds, genesis having taken the ones
	// below it. Sender indices count from there, so the pool's first pass carries nonce zero. Zero
	// means one.
	FirstBlockHeight uint64

	// Accounts bounds the sender pool, so a run reuses accounts instead of minting one per
	// transaction. Zero mints a fresh account every time, which leaves no account ever read twice.
	// Must be at least TxsPerBlock when set, so a block never draws one sender twice.
	Accounts uint64
}

// senderSlot is the account a transaction is sent from and the nonce it carries.
type senderSlot struct {
	account uint64
	nonce   uint64
}

// senderFor maps a run-wide transaction index onto the pool. Both halves come from the index, so
// nothing has to be tracked per account: index i takes account i%Accounts, and that account has
// been used i/Accounts times before, which is its next nonce.
//
// Callers reserve a contiguous range of indices per block, so a block's senders are distinct as
// long as the pool is at least TxsPerBlock, and an account's nonces rise in block order.
func (c Config) senderFor(index uint64) senderSlot {
	if c.Accounts == 0 {
		return senderSlot{account: index, nonce: 0}
	}
	return senderSlot{account: index % c.Accounts, nonce: index / c.Accounts}
}

type State interface {
	SetBalance(common.Address, *big.Int)
	SetCode(common.Address, []byte)
	SetState(common.Address, common.Hash, common.Hash)
}

type Workload interface {
	BuildBlock(context.Context, uint64) (evmonly.BlockRequest, error)
}

// PoolSeeder is implemented by workloads that draw senders from a bounded pool. A run that builds
// blocks as it goes needs every sender in state before genesis is committed, which only a bounded
// pool makes possible.
type PoolSeeder interface {
	SeedAccountPool(context.Context) error
}

func NewWorkload(kind string, cfg Config, state State) (Workload, error) {
	switch kind {
	case WorkloadTransfer:
		return NewTransferWorkload(cfg, state)
	case WorkloadERC20Transfer:
		return NewERC20TransferWorkload(cfg, state)
	case WorkloadSnapshotRevert:
		return NewSnapshotRevertWorkload(cfg, state), nil
	default:
		return nil, fmt.Errorf("unsupported workload %q", kind)
	}
}
