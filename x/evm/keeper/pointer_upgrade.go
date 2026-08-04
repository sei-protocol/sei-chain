package keeper

import (
	"math"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts"
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
	evmInstance := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, k.CustomPrecompiles(ctx))
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

func (k *Keeper) UpsertERCNativePointer(
	ctx sdk.Context, evm *vm.EVM, token string, metadata utils.ERCMetadata,
) (contractAddr common.Address, err error) {
	return k.UpsertERCPointer(
		ctx, evm, "native", []interface{}{
			token, metadata.Name, metadata.Symbol, metadata.Decimals,
		}, k.GetERC20NativePointer, k.SetERC20NativePointer,
	)
}

func (k *Keeper) UpsertERCCW20Pointer(
	ctx sdk.Context, evm *vm.EVM, cw20Addr string, metadata utils.ERCMetadata,
) (contractAddr common.Address, err error) {
	return k.UpsertERCPointer(
		ctx, evm, "cw20", []interface{}{
			cw20Addr, metadata.Name, metadata.Symbol,
		}, k.GetERC20CW20Pointer, k.SetERC20CW20Pointer,
	)
}

func (k *Keeper) UpsertERCCW721Pointer(
	ctx sdk.Context, evm *vm.EVM, cw721Addr string, metadata utils.ERCMetadata,
) (contractAddr common.Address, err error) {
	return k.UpsertERCPointer(
		ctx, evm, "cw721", []interface{}{
			cw721Addr, metadata.Name, metadata.Symbol,
		}, k.GetERC721CW721Pointer, k.SetERC721CW721Pointer,
	)
}

func (k *Keeper) UpsertERCCW1155Pointer(
	ctx sdk.Context, evm *vm.EVM, cw1155Addr string, metadata utils.ERCMetadata,
) (contractAddr common.Address, err error) {
	return k.UpsertERCPointer(
		ctx, evm, "cw1155", []interface{}{
			cw1155Addr, metadata.Name, metadata.Symbol,
		}, k.GetERC1155CW1155Pointer, k.SetERC1155CW1155Pointer,
	)
}

func (k *Keeper) UpsertERCPointer(
	ctx sdk.Context, evm *vm.EVM, typ string, args []interface{}, getter PointerGetter, setter PointerSetter,
) (contractAddr common.Address, err error) {
	pointee := args[0].(string)
	evmModuleAddress := k.GetEVMAddressOrDefault(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName))

	var bin []byte
	bin, err = artifacts.GetParsedABI(typ).Pack("", args...)
	if err != nil {
		panic(err)
	}
	bin = append(artifacts.GetBin(typ), bin...)
	// GetDeploymentCode / Create take EVM snapshots that Freeze() Multistore layers.
	// Exists-lookup and commits must use the live unfrozen top (sdb.Ctx): cachekv
	// forbids writing a frozen layer, and same-tx readers that skip frozen-empty
	// parents would miss those writes. The precompile Prepare `ctx` is that top at
	// Prepare time, but is frozen once this Upsert snapshots. Always attach the
	// caller's gas meter (finite precompile meter in deliver) — sdb.Ctx() alone
	// carries the infinite EVM meter.
	sdb := state.GetDBImpl(evm.StateDB)
	liveCtx := func() sdk.Context {
		if sdb == nil {
			return ctx
		}
		return sdb.Ctx().WithGasMeter(ctx.GasMeter())
	}
	existingAddr, _, exists := getter(liveCtx(), pointee)
	suppliedGas := k.getEvmGasLimitFromCtx(ctx)
	var remainingGas uint64
	if exists {
		var ret []byte
		contractAddr = existingAddr
		ret, remainingGas, err = evm.GetDeploymentCode(evmModuleAddress, bin, suppliedGas, utils.Big0, existingAddr)
		if err != nil {
			return
		}
		// Only write on success: a failed GetDeploymentCode can leave ret as nil or
		// revert data, which must not clobber live pointer bytecode (even transiently).
		writeCtx := liveCtx()
		k.SetCode(writeCtx, contractAddr, ret)
		if sdb != nil {
			sdb.RefreshCodeCache(contractAddr, ret)
		}
	} else {
		_, contractAddr, remainingGas, err = evm.Create(evmModuleAddress, bin, suppliedGas, uint256.NewInt(0))
	}
	if err != nil {
		return
	}
	ctx.GasMeter().ConsumeGas(k.GetCosmosGasLimitFromEVMGas(ctx, suppliedGas-remainingGas), "ERC pointer deployment")
	if err = setter(liveCtx(), pointee, contractAddr); err != nil {
		return
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, typ),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, contractAddr.Hex()), sdk.NewAttribute(types.AttributeKeyPointee, pointee)))
	return
}
