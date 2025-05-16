package v562

import (
	"errors"
	"fmt"
	"math/big"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

const UnknownMethodCallGas uint64 = 3000

type Contexter interface {
	Ctx() sdk.Context
}

type PrecompileExecutor interface {
	RequiredGas([]byte, *abi.Method) uint64
	Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) ([]byte, error)
}

type Precompile struct {
	abi.ABI
	address  common.Address
	name     string
	executor PrecompileExecutor
}

var _ vm.PrecompiledContract = &Precompile{}

func NewPrecompile(a abi.ABI, executor PrecompileExecutor, address common.Address, name string) *Precompile {
	return &Precompile{ABI: a, executor: executor, address: address, name: name}
}

func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID, err := ExtractMethodID(input)
	if err != nil {
		return UnknownMethodCallGas
	}

	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return UnknownMethodCallGas
	}
	return p.executor.RequiredGas(input[4:], method)
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, value *big.Int, readOnly bool, _ bool, hooks *tracing.Hooks) (bz []byte, err error) {
	operation := fmt.Sprintf("%s_unknown", p.name)
	defer func() {
		if hooks != nil {
			// only catch for tracing
			if pe := recover(); pe != nil {
				err = fmt.Errorf("%s", pe)
				return
			}
		}
		HandlePrecompileError(err, evm, operation)
	}()
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	operation = method.Name
	em := ctx.EventManager()
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	bz, err = p.executor.Execute(ctx, method, caller, callingContract, args, value, readOnly, evm, hooks)
	if err != nil {
		return bz, err
	}
	events := ctx.EventManager().Events()
	if len(events) > 0 {
		em.EmitEvents(ctx.EventManager().Events())
	}
	return bz, err
}

func HandlePrecompileError(err error, evm *vm.EVM, operation string) {
	if err != nil {
		evm.StateDB.(*state.DBImpl).SetPrecompileError(err)
		metrics.IncrementErrorMetrics(operation, err)
	}
}

func (p Precompile) Prepare(evm *vm.EVM, input []byte) (sdk.Context, *abi.Method, []interface{}, error) {
	ctxer, ok := evm.StateDB.(Contexter)
	if !ok {
		return sdk.Context{}, nil, nil, errors.New("cannot get context from EVM")
	}
	methodID, err := ExtractMethodID(input)
	if err != nil {
		return sdk.Context{}, nil, nil, err
	}
	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		return sdk.Context{}, nil, nil, err
	}

	argsBz := input[4:]
	args, err := method.Inputs.Unpack(argsBz)
	if err != nil {
		return sdk.Context{}, nil, nil, err
	}

	return ctxer.Ctx(), method, args, nil
}

func (p Precompile) GetABI() abi.ABI {
	return p.ABI
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) GetName() string {
	return p.name
}

func (p Precompile) GetExecutor() PrecompileExecutor {
	return p.executor
}

type DynamicGasPrecompileExecutor interface {
	Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (ret []byte, remainingGas uint64, err error)
	EVMKeeper() EVMKeeper
}

type DynamicGasPrecompile struct {
	*Precompile
	executor DynamicGasPrecompileExecutor
}

var _ vm.DynamicGasPrecompiledContract = &DynamicGasPrecompile{}

func NewDynamicGasPrecompile(a abi.ABI, executor DynamicGasPrecompileExecutor, address common.Address, name string) *DynamicGasPrecompile {
	return &DynamicGasPrecompile{Precompile: NewPrecompile(a, nil, address, name), executor: executor}
}

func (d DynamicGasPrecompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, suppliedGas uint64, value *big.Int, hooks *tracing.Hooks, readOnly bool, _ bool) (ret []byte, remainingGas uint64, err error) {
	operation := fmt.Sprintf("%s_unknown", d.name)
	defer func() {
		if hooks != nil {
			// only catch for tracing
			if pe := recover(); pe != nil {
				err = fmt.Errorf("%s", pe)
				return
			}
		}
		HandlePrecompileError(err, evm, operation)
	}()
	ctx, method, args, err := d.Prepare(evm, input)
	if err != nil {
		return nil, 0, err
	}
	gasMultipler := d.executor.EVMKeeper().GetPriorityNormalizerPre580(ctx)
	gasLimitBigInt := sdk.NewDecFromInt(sdk.NewIntFromUint64(suppliedGas)).Mul(gasMultipler).TruncateInt().BigInt()
	if gasLimitBigInt.Cmp(utils.BigMaxU64) > 0 {
		gasLimitBigInt = utils.BigMaxU64
	}
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, gasLimitBigInt.Uint64()))

	operation = method.Name
	em := ctx.EventManager()
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	ret, remainingGas, err = d.executor.Execute(ctx, method, caller, callingContract, args, value, readOnly, evm, suppliedGas, hooks)
	if err != nil {
		return ret, remainingGas, err
	}
	events := ctx.EventManager().Events()
	if len(events) > 0 {
		em.EmitEvents(ctx.EventManager().Events())
	}
	return ret, remainingGas, err
}

func (d DynamicGasPrecompile) GetExecutor() DynamicGasPrecompileExecutor {
	return d.executor
}

func ValidateArgsLength(args []interface{}, length int) error {
	if len(args) != length {
		return fmt.Errorf("expected %d arguments but got %d", length, len(args))
	}

	return nil
}

func ValidateNonPayable(value *big.Int) error {
	if value != nil && value.Sign() != 0 {
		return errors.New("sending funds to a non-payable function")
	}

	return nil
}

func HandlePaymentUsei(ctx sdk.Context, precompileAddr sdk.AccAddress, payer sdk.AccAddress, value *big.Int, bankKeeper BankKeeper, evmKeeper EVMKeeper, hooks *tracing.Hooks, depth int) (sdk.Coin, error) {
	usei, wei := state.SplitUseiWeiAmount(value)
	if !wei.IsZero() {
		return sdk.Coin{}, fmt.Errorf("selected precompile function does not allow payment with non-zero wei remainder: received %s", value)
	}
	coin := sdk.NewCoin(sdk.MustGetBaseDenom(), usei)
	// refund payer because the following precompile logic will debit the payments from payer's account
	// this creates a new event manager to avoid surfacing these as cosmos events
	if err := bankKeeper.SendCoins(ctx.WithEventManager(sdk.NewEventManager()), precompileAddr, payer, sdk.NewCoins(coin)); err != nil {
		return sdk.Coin{}, err
	}
	if hooks != nil {
		if hooks.OnEnter != nil {
			hooks.OnEnter(depth+1, byte(vm.CALL), evmKeeper.GetEVMAddressOrDefault(ctx, precompileAddr), evmKeeper.GetEVMAddressOrDefault(ctx, payer), []byte{}, GetRemainingGas(ctx, evmKeeper), value)
		}
		if hooks.OnExit != nil {
			hooks.OnExit(depth+1, []byte{}, 0, nil, false)
		}
	}
	return coin, nil
}

func HandlePaymentUseiWei(ctx sdk.Context, precompileAddr sdk.AccAddress, payer sdk.AccAddress, value *big.Int, bankKeeper BankKeeper, evmKeeper EVMKeeper, hooks *tracing.Hooks, depth int) (sdk.Int, sdk.Int, error) {
	usei, wei := state.SplitUseiWeiAmount(value)
	// refund payer because the following precompile logic will debit the payments from payer's account
	// this creates a new event manager to avoid surfacing these as cosmos events
	if err := bankKeeper.SendCoinsAndWei(ctx.WithEventManager(sdk.NewEventManager()), precompileAddr, payer, usei, wei); err != nil {
		return sdk.Int{}, sdk.Int{}, err
	}
	if hooks != nil {
		if hooks.OnEnter != nil {
			hooks.OnEnter(depth+1, byte(vm.CALL), evmKeeper.GetEVMAddressOrDefault(ctx, precompileAddr), evmKeeper.GetEVMAddressOrDefault(ctx, payer), []byte{}, GetRemainingGas(ctx, evmKeeper), value)
		}
		if hooks.OnExit != nil {
			hooks.OnExit(depth+1, []byte{}, 0, nil, false)
		}
	}
	return usei, wei, nil
}

/*
*
sei gas = evm gas * multiplier
sei gas price = fee / sei gas = fee / (evm gas * multiplier) = evm gas / multiplier
*/
func GetRemainingGas(ctx sdk.Context, evmKeeper EVMKeeper) uint64 {
	gasMultipler := evmKeeper.GetPriorityNormalizerPre580(ctx)
	seiGasRemaining := ctx.GasMeter().Limit() - ctx.GasMeter().GasConsumedToLimit()
	return sdk.NewDecFromInt(sdk.NewIntFromUint64(seiGasRemaining)).Quo(gasMultipler).TruncateInt().Uint64()
}

func ExtractMethodID(input []byte) ([]byte, error) {
	// Check if the input has at least the length needed for methodID
	if len(input) < 4 {
		return nil, errors.New("input too short to extract method ID")
	}
	return input[:4], nil
}

func DefaultGasCost(input []byte, isTransaction bool) uint64 {
	if isTransaction {
		return storetypes.KVGasConfig().WriteCostFlat + (storetypes.KVGasConfig().WriteCostPerByte * uint64(len(input)))
	}

	return storetypes.KVGasConfig().ReadCostFlat + (storetypes.KVGasConfig().ReadCostPerByte * uint64(len(input)))
}
