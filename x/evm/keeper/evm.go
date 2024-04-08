package keeper

import (
	"errors"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMCallFunc func(caller vm.ContractRef, addr *common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error)

var MaxUint64BigInt = new(big.Int).SetUint64(math.MaxUint64)

func (k *Keeper) HandleInternalEVMCall(ctx sdk.Context, req *types.MsgInternalEVMCall) (*sdk.Result, error) {
	var to *common.Address
	if req.To != "" {
		addr := common.HexToAddress(req.To)
		to = &addr
	}
	senderAddr, err := sdk.AccAddressFromBech32(req.Sender)
	if err != nil {
		return nil, err
	}
	ret, err := k.CallEVM(ctx, senderAddr, to, req.Value, req.Data)
	if err != nil {
		return nil, err
	}
	return &sdk.Result{Data: ret}, nil
}

func (k *Keeper) HandleInternalEVMDelegateCall(ctx sdk.Context, req *types.MsgInternalEVMDelegateCall) (*sdk.Result, error) {
	if !k.IsCWCodeHashWhitelistedForEVMDelegateCall(ctx, req.CodeHash) {
		return nil, errors.New("code hash not authorized to make EVM delegate call")
	}
	var to *common.Address
	if req.To != "" {
		addr := common.HexToAddress(req.To)
		to = &addr
	}
	zeroInt := sdk.ZeroInt()
	senderAddr, err := sdk.AccAddressFromBech32(req.Sender)
	if err != nil {
		return nil, err
	}
	ret, err := k.CallEVM(ctx, senderAddr, to, &zeroInt, req.Data)
	if err != nil {
		return nil, err
	}
	return &sdk.Result{Data: ret}, nil
}

func (k *Keeper) CallEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, val *sdk.Int, data []byte) (retdata []byte, reterr error) {
	evm, finalizer, err := k.getOrCreateEVM(ctx, from)
	if err != nil {
		return nil, err
	}
	defer func() {
		if finalizer != nil {
			if err := finalizer(); err != nil {
				reterr = err
				return
			}
		}
	}()
	var f EVMCallFunc
	if to == nil {
		// contract creation
		f = func(caller vm.ContractRef, _ *common.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
			ret, _, leftoverGas, err := evm.Create(caller, input, gas, value)
			return ret, leftoverGas, err
		}
	} else {
		f = func(caller vm.ContractRef, addr *common.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
			return evm.Call(caller, *addr, input, gas, value)
		}
	}
	return k.callEVM(ctx, from, to, val, data, f)
}

func (k *Keeper) StaticCallEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, data []byte) ([]byte, error) {
	evm, _, err := k.getOrCreateEVM(ctx, from)
	if err != nil {
		return nil, err
	}
	return k.callEVM(ctx, from, to, nil, data, func(caller vm.ContractRef, addr *common.Address, input []byte, gas uint64, _ *big.Int) ([]byte, uint64, error) {
		return evm.StaticCall(caller, *addr, input, gas)
	})
}

func (k *Keeper) callEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, val *sdk.Int, data []byte, f EVMCallFunc) ([]byte, error) {
	sender := k.GetEVMAddressOrDefault(ctx, from)
	seiGasRemaining := ctx.GasMeter().Limit() - ctx.GasMeter().GasConsumedToLimit()
	if ctx.GasMeter().Limit() <= 0 {
		// infinite gas meter (used in queries)
		seiGasRemaining = math.MaxUint64
	}
	multiplier := k.GetPriorityNormalizer(ctx)
	evmGasRemaining := sdk.NewDecFromInt(sdk.NewIntFromUint64(seiGasRemaining)).Quo(multiplier).TruncateInt().BigInt()
	if evmGasRemaining.Cmp(MaxUint64BigInt) > 0 {
		evmGasRemaining = MaxUint64BigInt
	}
	value := utils.Big0
	if val != nil {
		value = val.BigInt()
	}
	ret, leftoverGas, err := f(vm.AccountRef(sender), to, data, evmGasRemaining.Uint64(), value)
	ctx.GasMeter().ConsumeGas(ctx.GasMeter().Limit()-sdk.NewDecFromInt(sdk.NewIntFromUint64(leftoverGas)).Mul(multiplier).TruncateInt().Uint64(), "call EVM")
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (k *Keeper) getOrCreateEVM(ctx sdk.Context, from sdk.AccAddress) (*vm.EVM, func() error, error) {
	evm := types.GetCtxEVM(ctx)
	if evm != nil {
		return evm, nil, nil
	}
	executionCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
	stateDB := state.NewDBImpl(executionCtx, k, false)
	executionCtx, gp := k.getGasPool(executionCtx)
	blockCtx, err := k.GetVMBlockContext(executionCtx, gp)
	if err != nil {
		return nil, nil, err
	}
	cfg := types.DefaultChainConfig().EthereumConfig(k.ChainID(executionCtx))
	txCtx := vm.TxContext{Origin: k.GetEVMAddressOrDefault(ctx, from)}
	evm = vm.NewEVM(*blockCtx, txCtx, stateDB, cfg, vm.Config{})
	stateDB.SetEVM(evm)
	return evm, func() error {
		_, err := stateDB.Finalize()
		return err
	}, nil
}
