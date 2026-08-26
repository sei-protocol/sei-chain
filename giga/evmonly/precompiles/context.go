package precompiles

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

var ErrCustomPrecompilesOpen = errors.New("evm-only custom precompiles are not implemented")

// Registry resolves native custom precompiles for the EVM-only path.
type Registry interface {
	Get(common.Address) (Contract, bool)
	Addresses() []common.Address
}

// Contract is the sdk.Context-free custom precompile interface.
type Contract interface {
	RequiredGas(input []byte) uint64
	Run(*Context, []byte) ([]byte, error)
}

// EndBlocker is implemented by custom precompiles that need per-block work
// after all transactions have executed.
type EndBlocker interface {
	EndBlock(*EndBlockContext) ([]ValidatorUpdate, error)
}

// Context is the only execution context custom precompiles should receive in
// the EVM-only path. It deliberately excludes sdk.Context and Cosmos keepers.
type Context struct {
	Caller        common.Address
	Address       common.Address
	ApparentValue *big.Int
	ReadOnly      bool
	DelegateCall  bool
	GasRemaining  uint64
	Block         BlockContext
	Store         Store
	Balances      BalanceTransfer
	Logs          LogSink
}

// EndBlockContext is the SDK-free context custom precompiles receive after all
// transactions in a block have executed.
type EndBlockContext struct {
	Address  common.Address
	Block    BlockContext
	Store    Store
	Balances BalanceTransfer
	Logs     LogSink
}

// BlockContext is the block data custom precompiles may read.
type BlockContext struct {
	Number      uint64
	Time        uint64
	ChainID     *big.Int
	BaseFee     *big.Int
	BlobBaseFee *big.Int
	Coinbase    common.Address
	PrevRandao  common.Hash
}

// ValidatorUpdate is the EVM-only validator set update shape.
type ValidatorUpdate struct {
	PubKey []byte
	Power  int64
}

// Store is the byte-keyed state boundary custom precompiles use for module-like
// data. Implementations should make Get/Set/Delete visible through the same
// read/write tracking as ordinary EVM storage.
type Store interface {
	Get([]byte) ([]byte, bool)
	Set([]byte, []byte)
	Delete([]byte)
}

// BalanceTransfer moves native EVM value for precompiles that need to forward
// payable call value or adjust native balances alongside module-like state.
type BalanceTransfer interface {
	Transfer(from common.Address, to common.Address, amount *big.Int) error
}

// LogSink lets custom precompiles emit Ethereum logs without Cosmos events.
type LogSink interface {
	AddLog(*ethtypes.Log)
}
