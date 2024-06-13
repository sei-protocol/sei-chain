package keeper

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMCallFunc func(caller vm.ContractRef, addr *common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error)

var MaxUint64BigInt = new(big.Int).SetUint64(math.MaxUint64)

func (k *Keeper) HandleInternalEVMCall(ctx sdk.Context, req *types.MsgInternalEVMCall) (sdk.Context, *sdk.Result, error) {
	var to *common.Address
	if req.To != "" {
		addr := common.HexToAddress(req.To)
		to = &addr
	}
	senderAddr, err := sdk.AccAddressFromBech32(req.Sender)
	if err != nil {
		return ctx, nil, err
	}
	retctx, ret, err := k.CallEVM(ctx, k.GetEVMAddressOrDefault(ctx, senderAddr), to, req.Value, req.Data)
	if err != nil {
		return ctx, nil, err
	}
	return retctx, &sdk.Result{Data: ret}, nil
}

func (k *Keeper) HandleInternalEVMDelegateCall(ctx sdk.Context, req *types.MsgInternalEVMDelegateCall) (sdk.Context, *sdk.Result, error) {
	var to *common.Address
	if req.To != "" {
		addr := common.HexToAddress(req.To)
		to = &addr
	} else {
		return ctx, nil, errors.New("cannot use a CosmWasm contract to delegate-create an EVM contract")
	}
	addr, _, exists := k.GetPointerInfo(ctx, types.PointerReverseRegistryKey(common.BytesToAddress([]byte(req.FromContract))))
	if !exists || common.BytesToAddress(addr).Cmp(*to) != 0 {
		return ctx, nil, errors.New("only pointer contract can make delegatecalls")
	}
	zeroInt := sdk.ZeroInt()
	senderAddr, err := sdk.AccAddressFromBech32(req.Sender)
	if err != nil {
		return ctx, nil, err
	}
	// delegatecall caller must be associated; otherwise any state change on EVM contract will be lost
	// after they asssociate.
	senderEvmAddr, found := k.GetEVMAddress(ctx, senderAddr)
	if !found {
		err := types.NewAssociationMissingErr(req.Sender)
		metrics.IncrementAssociationError("evm_handle_internal_evm_delegate_call", err)
		return ctx, nil, err
	}
	retctx, ret, err := k.CallEVM(ctx, senderEvmAddr, to, &zeroInt, req.Data)
	if err != nil {
		return ctx, nil, err
	}
	return retctx, &sdk.Result{Data: ret}, nil
}

func (k *Keeper) CallEVM(ctx sdk.Context, from common.Address, to *common.Address, val *sdk.Int, data []byte) (retctx sdk.Context, retdata []byte, reterr error) {
	if to == nil && len(data) > params.MaxInitCodeSize {
		return ctx, nil, fmt.Errorf("%w: code size %v, limit %v", core.ErrMaxInitCodeSizeExceeded, len(data), params.MaxInitCodeSize)
	}
	value := utils.Big0
	if val != nil {
		if val.IsNegative() {
			return ctx, nil, sdkerrors.ErrInvalidCoins
		}
		value = val.BigInt()
	}
	evm := types.GetCtxEVM(ctx)
	if evm != nil {
		// This call is part of an existing StateTransition, so directly invoking `Call`
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
		ret, err := k.callEVM(ctx, from, to, val, data, f)
		if err != nil {
			return ctx, ret, err
		}
		return evm.StateDB.(*state.DBImpl).Ctx(), ret, err
	}
	// This call was not part of an existing StateTransition, so it should trigger one
	executionCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)).WithIsEVM(true)
	stateDB := state.NewDBImpl(executionCtx, k, false)
	gp := k.GetGasPool()
	evmMsg := &core.Message{
		Nonce:             stateDB.GetNonce(from), // replay attack is prevented by the AccountSequence number set on the CW transaction that triggered this call
		GasLimit:          k.getEvmGasLimitFromCtx(ctx),
		GasPrice:          utils.Big0, // fees are already paid on the CW transaction
		GasFeeCap:         utils.Big0,
		GasTipCap:         utils.Big0,
		To:                to,
		Value:             value,
		Data:              data,
		SkipAccountChecks: false,
		From:              from,
	}
	res, err := k.applyEVMMessage(ctx, evmMsg, stateDB, gp)
	if err != nil {
		return ctx, nil, err
	}
	k.consumeEvmGas(ctx, res.UsedGas)
	if res.Err != nil {
		return ctx, nil, res.Err
	}
	surplus, err := stateDB.Finalize()
	if err != nil {
		return ctx, nil, err
	}
	vmErr := ""
	if res.Err != nil {
		vmErr = res.Err.Error()
	}
	receipt, err := k.WriteReceipt(ctx, stateDB, evmMsg, ethtypes.LegacyTxType, ctx.TxSum(), res.UsedGas, vmErr)
	if err != nil {
		return ctx, nil, err
	}
	bloom := ethtypes.Bloom{}
	bloom.SetBytes(receipt.LogsBloom)
	k.AppendToEvmTxDeferredInfo(ctx, bloom, ctx.TxSum(), surplus)
	ctx.EVMEventManager().EmitEvents(stateDB.GetAllLogs())
	return ctx, res.ReturnData, nil
}

func (k *Keeper) StaticCallEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, data []byte) ([]byte, error) {
	evm, err := k.createReadOnlyEVM(ctx, from)
	if err != nil {
		return nil, err
	}
	return k.callEVM(ctx, k.GetEVMAddressOrDefault(ctx, from), to, nil, data, func(caller vm.ContractRef, addr *common.Address, input []byte, gas uint64, _ *big.Int) ([]byte, uint64, error) {
		return evm.StaticCall(caller, *addr, input, gas)
	})
}

func (k *Keeper) callEVM(ctx sdk.Context, from common.Address, to *common.Address, val *sdk.Int, data []byte, f EVMCallFunc) ([]byte, error) {
	evmGasLimit := k.getEvmGasLimitFromCtx(ctx)
	value := utils.Big0
	if val != nil {
		value = val.BigInt()
	}
	ret, leftoverGas, err := f(vm.AccountRef(from), to, data, evmGasLimit, value)
	k.consumeEvmGas(ctx, evmGasLimit-leftoverGas)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// only used for StaticCalls
func (k *Keeper) createReadOnlyEVM(ctx sdk.Context, from sdk.AccAddress) (*vm.EVM, error) {
	executionCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	stateDB := state.NewDBImpl(executionCtx, k, true)
	gp := k.GetGasPool()
	blockCtx, err := k.GetVMBlockContext(executionCtx, gp)
	if err != nil {
		return nil, err
	}
	cfg := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	txCtx := vm.TxContext{Origin: k.GetEVMAddressOrDefault(ctx, from)}
	return vm.NewEVM(*blockCtx, txCtx, stateDB, cfg, vm.Config{}), nil
}

func (k *Keeper) getEvmGasLimitFromCtx(ctx sdk.Context) uint64 {
	seiGasRemaining := ctx.GasMeter().Limit() - ctx.GasMeter().GasConsumedToLimit()
	if ctx.GasMeter().Limit() <= 0 {
		return math.MaxUint64
	}
	evmGasBig := sdk.NewDecFromInt(sdk.NewIntFromUint64(seiGasRemaining)).Quo(k.GetPriorityNormalizer(ctx)).TruncateInt().BigInt()
	if evmGasBig.Cmp(MaxUint64BigInt) > 0 {
		evmGasBig = MaxUint64BigInt
	}
	return evmGasBig.Uint64()
}

func (k *Keeper) consumeEvmGas(ctx sdk.Context, usedEvmGas uint64) {
	ctx.GasMeter().ConsumeGas(sdk.NewDecFromInt(sdk.NewIntFromUint64(usedEvmGas)).Mul(k.GetPriorityNormalizer(ctx)).TruncateInt().Uint64(), "call EVM")
}
