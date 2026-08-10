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
}

type State interface {
	SetBalance(common.Address, *big.Int)
	SetCode(common.Address, []byte)
	SetState(common.Address, common.Hash, common.Hash)
}

type Workload interface {
	BuildBlock(context.Context, uint64) (evmonly.BlockRequest, error)
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
