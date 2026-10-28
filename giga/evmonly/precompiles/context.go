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
	// EndBlockers returns the addresses whose contracts run EndBlock, in the
	// order they run.
	EndBlockers() []common.Address
}

// Contract is the sdk.Context-free custom precompile interface. Run may be
// called concurrently and must be deterministic: it reads and writes chain
// state only through the Context's State and Logs, reads block data only from
// its Block, and keeps no mutable state of its own. It reads and writes nothing
// off-chain; compiled-in constants are allowed, but changing one changes the
// app hash. RequiredGas is the flat cost of a call; storage reads and writes
// and logs are charged on top of it.
type Contract interface {
	RequiredGas(input []byte) uint64
	Run(*Context, []byte) ([]byte, error)
}

// EndBlocker is a Contract that also runs once at the end of every block, after
// the block's transactions, under the same rules as Run. Its writes land in the
// block's changeset, its storage access is not charged gas, and a returned
// error fails the block.
type EndBlocker interface {
	EndBlock(BlockContext, State) error
}

// Context is the only execution context custom precompiles should receive in
// the EVM-only path. It deliberately excludes sdk.Context and Cosmos keepers.
type Context struct {
	Caller        common.Address
	Address       common.Address
	ApparentValue *big.Int
	ReadOnly      bool
	DelegateCall  bool
	// GasRemaining is the call's gas after RequiredGas, as Run begins. Storage
	// reads and writes and logs draw on it.
	GasRemaining uint64
	Block        BlockContext
	State        State
	Logs         LogSink
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
}

// LogSink lets custom precompiles emit Ethereum logs without Cosmos events.
type LogSink interface {
	AddLog(*ethtypes.Log)
}
