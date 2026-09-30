package precompiles

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

var ErrCustomPrecompilesOpen = errors.New("evm-only custom precompile address has no registered contract")

// Registry resolves native custom precompiles for the EVM-only path.
type Registry interface {
	Get(common.Address) (Contract, bool)
	Addresses() []common.Address
}

// Contract is the sdk.Context-free custom precompile interface. One instance
// serves every call, including concurrent calls from OCC workers, eth_call and
// gas estimation, so an implementation must be safe for concurrent use and must
// keep all of its state in Context.State rather than in memory.
type Contract interface {
	RequiredGas(input []byte) uint64
	Run(*Context, []byte) ([]byte, error)
}

// EndBlocker is a Contract that also runs once at the end of every block, after
// the block's transactions and before its state changes are committed. Its
// writes land in the block's changeset. A returned error fails the block.
type EndBlocker interface {
	EndBlock(BlockContext, State) error
}

// BeginBlocker is a Contract that also checks every block before its
// transactions run, reading the state the previous block left. A returned error
// fails the block.
type BeginBlocker interface {
	BeginBlock(BlockContext, StateReader) error
}

// StateReader reads account storage.
type StateReader interface {
	GetState(common.Address, common.Hash) common.Hash
}

// Context is the only execution context custom precompiles should receive in
// the EVM-only path. It deliberately excludes sdk.Context and Cosmos keepers.
//
// DelegateCall is set for DELEGATECALL and CALLCODE. Caller and ApparentValue
// then come from the delegating frame: Caller may be that contract's own
// caller, and no value reached the precompile. A contract that authorizes by
// Caller or credits ApparentValue must reject these calls.
type Context struct {
	Caller        common.Address
	Address       common.Address
	ApparentValue *big.Int
	ReadOnly      bool
	DelegateCall  bool
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
}

// LogSink lets custom precompiles emit Ethereum logs without Cosmos events. A
// log is recorded as a copy, under the precompile's own address whatever its
// Address field holds.
type LogSink interface {
	AddLog(*ethtypes.Log)
}
