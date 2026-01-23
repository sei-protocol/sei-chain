package executor

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	gigaevmkeeper "github.com/sei-protocol/sei-chain/giga/deps/xevm/keeper"
	gigatypes "github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
)

// GigaEvmKeeperWrapper wraps the GigaEvmKeeper to satisfy GigaExecutorKeeper.
// This allows the executor to use a unified interface for both keeper implementations.
type GigaEvmKeeperWrapper struct {
	keeper *gigaevmkeeper.Keeper
}

// NewGigaEvmKeeperWrapper creates a new wrapper around the GigaEvmKeeper.
func NewGigaEvmKeeperWrapper(k *gigaevmkeeper.Keeper) *GigaEvmKeeperWrapper {
	return &GigaEvmKeeperWrapper{keeper: k}
}

// EvmoneVM returns the evmone VM instance for use by the executor.
func (w *GigaEvmKeeperWrapper) EvmoneVM() *evmc.VM {
	return w.keeper.EvmoneVM
}

// GetVMBlockContext returns the VM block context for transaction execution.
func (w *GigaEvmKeeperWrapper) GetVMBlockContext(ctx sdk.Context, gp core.GasPool) (*vm.BlockContext, error) {
	return w.keeper.GetVMBlockContext(ctx, gp)
}

// GetGasPool returns the gas pool for the block.
func (w *GigaEvmKeeperWrapper) GetGasPool() core.GasPool {
	return w.keeper.GetGasPool()
}

// GetBaseFee returns the base fee for EIP-1559 transactions.
func (w *GigaEvmKeeperWrapper) GetBaseFee(ctx sdk.Context) *big.Int {
	return w.keeper.GetBaseFee(ctx)
}

// ChainID returns the EVM chain ID.
func (w *GigaEvmKeeperWrapper) ChainID(ctx sdk.Context) *big.Int {
	return w.keeper.ChainID(ctx)
}

// GetChainConfig returns the Ethereum chain config for EVM execution.
func (w *GigaEvmKeeperWrapper) GetChainConfig(ctx sdk.Context) *params.ChainConfig {
	sstore := w.keeper.GetParams(ctx).SeiSstoreSetGasEip2200
	return gigatypes.DefaultChainConfig().EthereumConfigWithSstore(w.keeper.ChainID(ctx), &sstore)
}

// CustomPrecompiles returns the custom precompiled contracts.
func (w *GigaEvmKeeperWrapper) CustomPrecompiles(ctx sdk.Context) map[common.Address]vm.PrecompiledContract {
	return w.keeper.CustomPrecompiles(ctx)
}

// GetEVMAddress returns the EVM address for a given Sei address.
func (w *GigaEvmKeeperWrapper) GetEVMAddress(ctx sdk.Context, seiAddr sdk.AccAddress) (common.Address, bool) {
	return w.keeper.GetEVMAddress(ctx, seiAddr)
}

// SetAddressMapping sets the bidirectional mapping between Sei and EVM addresses.
func (w *GigaEvmKeeperWrapper) SetAddressMapping(ctx sdk.Context, seiAddr sdk.AccAddress, evmAddr common.Address) {
	w.keeper.SetAddressMapping(ctx, seiAddr, evmAddr)
}

// BankKeeper returns the bank keeper.
func (w *GigaEvmKeeperWrapper) BankKeeper() bankkeeper.Keeper {
	return w.keeper.BankKeeper()
}

// AccountKeeper returns the account keeper.
func (w *GigaEvmKeeperWrapper) AccountKeeper() *authkeeper.AccountKeeper {
	return w.keeper.AccountKeeper()
}

// NewStateDB creates a new state DB using the giga/deps/xevm/state package.
// This uses ctx.GigaKVStore() instead of ctx.KVStore().
func (w *GigaEvmKeeperWrapper) NewStateDB(ctx sdk.Context, simulation bool) StateDBWithFinalize {
	return w.keeper.NewStateDB(ctx, simulation)
}

// WriteReceipt writes the transaction receipt using the GigaEvmKeeper.
// Returns ReceiptResult containing the bloom and serialized receipt.
func (w *GigaEvmKeeperWrapper) WriteReceipt(ctx sdk.Context, stateDB StateDBWithFinalize, msg *core.Message, txType uint32, txHash common.Hash, gasUsed uint64, vmError string) (*ReceiptResult, error) {
	receipt, err := w.keeper.WriteReceiptFromInterface(ctx, stateDB, msg, txType, txHash, gasUsed, vmError)
	if err != nil {
		return nil, err
	}
	receiptBytes, _ := receipt.Marshal()
	return &ReceiptResult{
		LogsBloom:    receipt.LogsBloom,
		ReceiptBytes: receiptBytes,
	}, nil
}

// Verify GigaEvmKeeperWrapper implements GigaExecutorKeeper at compile time
var _ GigaExecutorKeeper = (*GigaEvmKeeperWrapper)(nil)
