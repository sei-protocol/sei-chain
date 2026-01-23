package executor

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// GigaExecutorKeeper defines what the Giga executor needs from a keeper.
// Both GigaEvmKeeper and EvmKeeper (via adapter) can satisfy this interface.
type GigaExecutorKeeper interface {
	// Block context
	GetVMBlockContext(ctx sdk.Context, gp core.GasPool) (*vm.BlockContext, error)
	GetGasPool() core.GasPool
	GetBaseFee(ctx sdk.Context) *big.Int
	ChainID(ctx sdk.Context) *big.Int
	CustomPrecompiles(ctx sdk.Context) map[common.Address]vm.PrecompiledContract

	// Chain config - returns the Ethereum chain config for EVM execution.
	// Uses keeper's params (SeiSstoreSetGasEip2200) to configure the chain.
	GetChainConfig(ctx sdk.Context) *params.ChainConfig

	// Address operations
	GetEVMAddress(ctx sdk.Context, seiAddr sdk.AccAddress) (common.Address, bool)
	SetAddressMapping(ctx sdk.Context, seiAddr sdk.AccAddress, evmAddr common.Address)

	// Accessor keepers (for association helper)
	BankKeeper() bankkeeper.Keeper
	AccountKeeper() *authkeeper.AccountKeeper

	// EvmoneVM returns the evmone VM instance for EVM execution.
	EvmoneVM() *evmc.VM

	// StateDB creation - returns the appropriate StateDB for this keeper
	NewStateDB(ctx sdk.Context, simulation bool) StateDBWithFinalize

	// Receipt operations - writes transaction receipt and returns receipt data for response construction
	WriteReceipt(ctx sdk.Context, stateDB StateDBWithFinalize, msg *core.Message, txType uint32, txHash common.Hash, gasUsed uint64, vmError string) (*ReceiptResult, error)
}

// ReceiptResult contains the receipt data needed for constructing the transaction response.
// This allows the interface to be type-agnostic while still providing all necessary data.
type ReceiptResult struct {
	// LogsBloom is the bloom filter for the logs
	LogsBloom []byte
	// ReceiptBytes is the serialized receipt for including in response Data field
	ReceiptBytes []byte
}

// StateDBWithFinalize extends vm.StateDB with additional methods needed by the executor.
type StateDBWithFinalize interface {
	vm.StateDB

	// Finalize commits the state changes and returns any surplus.
	Finalize() (sdk.Int, error)

	// Cleanup releases resources held by the state DB.
	Cleanup()

	// GetAllLogs returns all logs emitted during execution.
	GetAllLogs() []*ethtypes.Log

	// GetPrecompileError returns any error from precompile execution.
	GetPrecompileError() error
}
