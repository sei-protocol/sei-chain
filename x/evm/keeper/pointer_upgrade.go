package keeper

import (
	"math"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) RunWithOneOffEVMInstance(
	ctx sdk.Context, runner func(*vm.EVM) error, logger func(string, string),
) error {
	stateDB := state.NewDBImpl(ctx, k, false)
	evmModuleAddress := k.GetEVMAddressOrDefault(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName))
	gp := core.GasPool(math.MaxUint64)
	blockCtx, err := k.GetVMBlockContext(ctx, gp)
	if err != nil {
		logger("get block context", err.Error())
		return err
	}
	sstore := k.GetSstoreSetGasEIP2200(ctx)
	cfg := types.DefaultChainConfig().EthereumConfigWithSstore(k.ChainID(ctx), &sstore)
	txCtx := core.NewEVMTxContext(&core.Message{From: evmModuleAddress, GasPrice: utils.Big0})
	evmInstance := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, k.CustomPrecompiles())
	evmInstance.SetTxContext(txCtx)
	err = runner(evmInstance)
	if err != nil {
		logger("upserting pointer", err.Error())
		return err
	}
	surplus, err := stateDB.Finalize()
	if err != nil {
		logger("finalizing", err.Error())
		return err
	}
	if !surplus.IsZero() {
		logger("non-zero surplus", surplus.String())
	}
	return nil
}
