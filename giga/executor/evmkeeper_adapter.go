package executor

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmstate "github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// EvmKeeperAdapter wraps the original EvmKeeper to satisfy GigaExecutorKeeper.
// This allows the Giga executor to use either the GigaEvmKeeper or the original
// EvmKeeper for comparison testing.
type EvmKeeperAdapter struct {
	keeper   *evmkeeper.Keeper
	evmoneVM *evmc.VM
}

// NewEvmKeeperAdapter creates a new adapter wrapping the original EvmKeeper.
// The evmoneVM is passed separately since the original EvmKeeper doesn't have it.
func NewEvmKeeperAdapter(k *evmkeeper.Keeper, vm *evmc.VM) *EvmKeeperAdapter {
	return &EvmKeeperAdapter{keeper: k, evmoneVM: vm}
}

// EvmoneVM returns the evmone VM instance for use by the executor.
func (a *EvmKeeperAdapter) EvmoneVM() *evmc.VM {
	return a.evmoneVM
}

// GetVMBlockContext returns the VM block context for transaction execution.
func (a *EvmKeeperAdapter) GetVMBlockContext(ctx sdk.Context, gp core.GasPool) (*vm.BlockContext, error) {
	return a.keeper.GetVMBlockContext(ctx, gp)
}

// GetGasPool returns the gas pool for the block.
func (a *EvmKeeperAdapter) GetGasPool() core.GasPool {
	return a.keeper.GetGasPool()
}

// GetBaseFee returns the base fee for EIP-1559 transactions.
func (a *EvmKeeperAdapter) GetBaseFee(ctx sdk.Context) *big.Int {
	return a.keeper.GetBaseFee(ctx)
}

// ChainID returns the EVM chain ID.
func (a *EvmKeeperAdapter) ChainID(ctx sdk.Context) *big.Int {
	return a.keeper.ChainID(ctx)
}

// GetChainConfig returns the Ethereum chain config for EVM execution.
func (a *EvmKeeperAdapter) GetChainConfig(ctx sdk.Context) *params.ChainConfig {
	sstore := a.keeper.GetParams(ctx).SeiSstoreSetGasEip2200
	return evmtypes.DefaultChainConfig().EthereumConfigWithSstore(a.keeper.ChainID(ctx), &sstore)
}

// CustomPrecompiles returns the custom precompiled contracts.
func (a *EvmKeeperAdapter) CustomPrecompiles(ctx sdk.Context) map[common.Address]vm.PrecompiledContract {
	return a.keeper.CustomPrecompiles(ctx)
}

// GetEVMAddress returns the EVM address for a given Sei address.
func (a *EvmKeeperAdapter) GetEVMAddress(ctx sdk.Context, seiAddr sdk.AccAddress) (common.Address, bool) {
	return a.keeper.GetEVMAddress(ctx, seiAddr)
}

// SetAddressMapping sets the bidirectional mapping between Sei and EVM addresses.
func (a *EvmKeeperAdapter) SetAddressMapping(ctx sdk.Context, seiAddr sdk.AccAddress, evmAddr common.Address) {
	a.keeper.SetAddressMapping(ctx, seiAddr, evmAddr)
}

// BankKeeper returns the bank keeper.
func (a *EvmKeeperAdapter) BankKeeper() bankkeeper.Keeper {
	return a.keeper.BankKeeper()
}

// AccountKeeper returns the account keeper.
func (a *EvmKeeperAdapter) AccountKeeper() *authkeeper.AccountKeeper {
	return a.keeper.AccountKeeper()
}

// NewStateDB creates a new state DB using the original x/evm/state package.
// This uses ctx.KVStore() instead of ctx.GigaKVStore().
func (a *EvmKeeperAdapter) NewStateDB(ctx sdk.Context, simulation bool) StateDBWithFinalize {
	return evmstate.NewDBImpl(ctx, a.keeper, simulation)
}

// WriteReceipt writes the transaction receipt using the original EvmKeeper.
// Returns ReceiptResult containing the bloom and serialized receipt.
func (a *EvmKeeperAdapter) WriteReceipt(ctx sdk.Context, stateDB StateDBWithFinalize, msg *core.Message, txType uint32, txHash common.Hash, gasUsed uint64, vmError string) (*ReceiptResult, error) {
	// Cast to the concrete type expected by the original keeper
	dbImpl, ok := stateDB.(*evmstate.DBImpl)
	if !ok {
		return nil, fmt.Errorf("expected *evmstate.DBImpl, got %T", stateDB)
	}
	receipt, err := a.keeper.WriteReceipt(ctx, dbImpl, msg, txType, txHash, gasUsed, vmError)
	if err != nil {
		return nil, err
	}
	receiptBytes, _ := receipt.Marshal()
	return &ReceiptResult{
		LogsBloom:    receipt.LogsBloom,
		ReceiptBytes: receiptBytes,
	}, nil
}

// Verify EvmKeeperAdapter implements GigaExecutorKeeper at compile time
var _ GigaExecutorKeeper = (*EvmKeeperAdapter)(nil)
