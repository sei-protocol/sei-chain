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
}

// Contract is the sdk.Context-free custom precompile interface.
type Contract interface {
	RequiredGas(input []byte) uint64
	Run(*Context, []byte) ([]byte, error)
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
	State         State
	Logs          LogSink
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

// State is the precompile-facing state API. Implementations must make these
// reads and writes visible to the executor's conflict tracking.
type State interface {
	GetBalance(common.Address) *big.Int
	AddBalance(common.Address, *big.Int)
	SubBalance(common.Address, *big.Int) error
	GetNonce(common.Address) uint64
	SetNonce(common.Address, uint64)
	GetCode(common.Address) []byte
	GetState(common.Address, common.Hash) common.Hash
	SetState(common.Address, common.Hash, common.Hash)
	GetCustom([]byte) ([]byte, bool)
	SetCustom([]byte, []byte)
}

// LogSink lets custom precompiles emit Ethereum logs without Cosmos events.
type LogSink interface {
	AddLog(*ethtypes.Log)
}
