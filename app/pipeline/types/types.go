package types

import (
	"math/big"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// BlockProcessRequest interface for block processing requests
type BlockProcessRequest interface {
	GetHash() []byte
	GetTxs() [][]byte
	GetByzantineValidators() []abci.Misbehavior
	GetHeight() int64
	GetTime() time.Time
}

// TransactionType indicates whether a transaction is EVM or COSMOS
type TransactionType int

const (
	TransactionTypeEVM TransactionType = iota
	TransactionTypeCOSMOS
)

// PreprocessedTx contains all preprocessed data for a transaction
type PreprocessedTx struct {
	Type TransactionType

	// EVM-specific fields (only populated if Type == TransactionTypeEVM)
	TxData            ethtx.TxData
	SenderEVMAddr     []byte
	SenderSeiAddr     sdk.AccAddress
	EVMMessage        *core.Message // Precomputed EVM message
	CodeSizeOK        bool
	IntrinsicGas      uint64
	FeeCapsOK         bool
	ChainIDOK         bool
	Priority          int64
	EffectiveGasPrice *big.Int

	// COSMOS-specific fields (only populated if Type == TransactionTypeCOSMOS)
	CosmosTx sdk.Tx

	// Common fields
	TxIndex int // Original index in block
}

// PreprocessedBlock contains all preprocessed data for a block
type PreprocessedBlock struct {
	// Block request data
	Height              int64
	Hash                []byte
	Time                time.Time
	ByzantineValidators []abci.Misbehavior
	LastCommit          abci.LastCommitInfo

	// Preprocessed transactions
	PreprocessedTxs []*PreprocessedTx

	// Block-level info
	BaseFee         *big.Int
	ChainConfig     *params.ChainConfig // Ethereum chain config
	ConsensusParams *tmproto.ConsensusParams
	BlockMaxGas     int64

	// Context and transaction bytes (needed for execution)
	Ctx sdk.Context
	Txs [][]byte // Original transaction bytes

	// Decoded transactions (to avoid re-decoding during execution)
	TypedTxs []sdk.Tx

	// Error field - if set, preprocessing failed and the block should not be executed
	PreprocessError error
}

// TransactionResult contains the result of executing a transaction
type TransactionResult struct {
	GasUsed    int64
	ReturnData []byte
	Logs       []*ethtypes.Log
	VmError    string
	Events     []abci.Event
	Code       uint32
	Codespace  string
	Log        string
	Surplus    sdk.Int // Surplus from fee handling (for EVM transactions)
}

// ExecutedBlock contains the result of executing a block
type ExecutedBlock struct {
	// Original preprocessed block
	PreprocessedBlock *PreprocessedBlock

	// Transaction results
	TxResults []*TransactionResult

	// Block-level state
	Events       []abci.Event
	AppHash      []byte
	EndBlockResp abci.ResponseEndBlock

	// Context for finalization (needed for receipt writing)
	Ctx sdk.Context
}

// ProcessedBlock contains the final processed block with all data
type ProcessedBlock struct {
	ExecutedBlock *ExecutedBlock
	// Additional metadata can be added here if needed
}

// PreprocessedBlockWithContext wraps a preprocessed block with its execution context
type PreprocessedBlockWithContext struct {
	Block *PreprocessedBlock
	Ctx   sdk.Context
	Txs   [][]byte // Original transaction bytes (needed for COSMOS transactions)
}

// BlockRequest wraps the block processing request
type BlockRequest struct {
	Ctx        sdk.Context
	Req        BlockProcessRequest
	LastCommit abci.CommitInfo
	Txs        [][]byte
}

// PreprocessorHelper provides access to keeper methods needed for preprocessing
type PreprocessorHelper interface {
	GetBaseFee(ctx sdk.Context) *big.Int
	GetMinimumFeePerGas(ctx sdk.Context) sdk.Dec
	GetPriorityNormalizer(ctx sdk.Context) sdk.Dec
	ChainID(ctx sdk.Context) *big.Int
	EthBlockTestConfigEnabled() bool
}

// ExecutionHelper provides access to keeper methods needed for execution
type ExecutionHelper interface {
	PreprocessorHelper // Includes preprocessing helper methods

	// EVM execution methods
	GetGasPool() core.GasPool
	GetVMBlockContext(ctx sdk.Context, gp core.GasPool) (*vm.BlockContext, error)
	GetParams(ctx sdk.Context) evmtypes.Params
	ChainID(ctx sdk.Context) *big.Int
	CustomPrecompiles(ctx sdk.Context) map[common.Address]vm.PrecompiledContract
	ApplyEVMMessage(ctx sdk.Context, msg *core.Message, stateDB *state.DBImpl, gp core.GasPool, shouldIncrementNonce bool) (*core.ExecutionResult, error)

	// State access methods
	GetNonce(ctx sdk.Context, addr common.Address) uint64
	GetBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int
	AddAnteSurplus(ctx sdk.Context, hash common.Hash, surplus sdk.Int) error

	// Cosmos transaction execution
	DeliverTx(ctx sdk.Context, req abci.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx
	
	// Batch transaction execution (for OCC)
	ProcessTXsWithOCC(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) ([]*abci.ExecTxResult, sdk.Context)
	DecodeTransactionsConcurrently(ctx sdk.Context, txs [][]byte) []sdk.Tx

	// Keeper access for stateDB creation
	GetKeeper() *evmkeeper.Keeper
}

// FinalizerHelper provides access to keeper methods needed for finalization
type FinalizerHelper interface {
	GetKeeper() *evmkeeper.Keeper
	GetPriorityNormalizer(ctx sdk.Context) sdk.Dec
	WriteReceipt(ctx sdk.Context, stateDB *state.DBImpl, msg *core.Message, txType uint32, txHash common.Hash, gasUsed uint64, vmError string) (*evmtypes.Receipt, error)
	AppendToEvmTxDeferredInfo(ctx sdk.Context, bloom ethtypes.Bloom, txHash common.Hash, surplus sdk.Int)
	FlushTransientReceipts(ctx sdk.Context) error
}

// OrderedItem interface for items that can be ordered by sequence number
type OrderedItem interface {
	GetSequenceNumber() int64
}

// GetSequenceNumber implements OrderedItem for PreprocessedBlock
func (pb *PreprocessedBlock) GetSequenceNumber() int64 {
	return pb.Height
}
