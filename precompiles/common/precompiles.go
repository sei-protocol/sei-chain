package common

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const UnknownMethodCallGas uint64 = 3000

type Contexter interface {
	Ctx() sdk.Context
}

type StateEVMKeeperGetter interface {
	EVMKeeper() state.EVMKeeper
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

	method, err := p.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return UnknownMethodCallGas
	}
	return p.executor.RequiredGas(input[4:], method)
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, value *big.Int, readOnly bool, isFromDelegateCall bool, hooks *tracing.Hooks) (bz []byte, err error) {
	operation := fmt.Sprintf("%s_unknown", p.name)
	defer func() {
		HandlePrecompileError(err, evm, operation)
		if err != nil {
			err = vm.ErrExecutionReverted
		}
	}()
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	operation = method.Name
	em := ctx.EventManager()
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithEVMPrecompileCalledFromDelegateCall(isFromDelegateCall)
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
		if sdb := state.GetDBImpl(evm.StateDB); sdb != nil {
			sdb.SetPrecompileError(err)
		}
		metrics.IncrementErrorMetrics(operation, err)
	}
}

func (p Precompile) Prepare(evm *vm.EVM, input []byte) (sdk.Context, *abi.Method, []interface{}, error) {
	ctxer := state.GetDBImpl(evm.StateDB)
	if ctxer == nil {
		return sdk.Context{}, nil, nil, errors.New("cannot get context from EVM")
	}
	method, err := p.resolveMethod(input)
	if err != nil {
		return sdk.Context{}, nil, nil, err
	}

	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return sdk.Context{}, nil, nil, err
	}

	return ctxer.Ctx(), method, args, nil
}

// resolveMethod extracts the 4-byte selector from input and resolves the ABI
// method WITHOUT decoding the (attacker-controlled) argument payload. Decoding
// is deliberately deferred so callers can perform it under a gas gate — see
// DynamicGasPrecompile.RunAndCalculateGas.
func (p Precompile) resolveMethod(input []byte) (*abi.Method, error) {
	methodID, err := ExtractMethodID(input)
	if err != nil {
		return nil, err
	}
	return p.MethodById(methodID)
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
	EVMKeeper() putils.EVMKeeper
}

type DynamicGasPrecompile struct {
	*Precompile
	executor DynamicGasPrecompileExecutor
}

var _ vm.DynamicGasPrecompiledContract = &DynamicGasPrecompile{}

func NewDynamicGasPrecompile(a abi.ABI, executor DynamicGasPrecompileExecutor, address common.Address, name string) *DynamicGasPrecompile {
	return &DynamicGasPrecompile{Precompile: NewPrecompile(a, nil, address, name), executor: executor}
}

func (d DynamicGasPrecompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, suppliedGas uint64, value *big.Int, hooks *tracing.Hooks, readOnly bool, isFromDelegateCall bool) (ret []byte, remainingGas uint64, err error) {
	operation := fmt.Sprintf("%s_unknown", d.name)
	defer func() {
		// Turn any panic — most importantly an out-of-gas panic raised while
		// metering the calldata decode below — into a reverted precompile call,
		// mirroring how the individual executor methods recover. This keeps a
		// single precompile call's failure from aborting the enclosing EVM frame.
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
		HandlePrecompileError(err, evm, operation)
		if err != nil {
			fmt.Printf("precompile %s encountered error: %v\n", d.name, err)
			err = vm.ErrExecutionReverted
		}
	}()
	ctxer := state.GetDBImpl(evm.StateDB)
	if ctxer == nil {
		return nil, 0, errors.New("cannot get context from EVM")
	}
	// Resolve the target method from the 4-byte selector only. The argument
	// payload is intentionally NOT decoded yet: ABI decoding of attacker-
	// controlled calldata can cost far more than len(input) (a single string can
	// be referenced by many array/tuple slots), so it must be paid for out of the
	// gas the caller supplied. The static-precompile path charges RequiredGas in
	// vm.RunPrecompiledContract before running; that step is skipped for
	// dynamic-gas precompiles, so we apply the equivalent charge here.
	method, err := d.resolveMethod(input)
	if err != nil {
		return nil, 0, err
	}
	operation = method.Name

	ctx := ctxer.Ctx()
	// Install the gas meter derived from the supplied EVM gas, then charge for
	// decoding the calldata BEFORE decoding it. A call that cannot afford the
	// decode is rejected here, before the parse/allocation work is performed.
	// ConsumeGas panics on out-of-gas; the deferred recover above turns that into
	// a normal reverted precompile call.
	gasLimit := d.executor.EVMKeeper().GetCosmosGasLimitFromEVMGas(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), suppliedGas)
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, gasLimit))

	// Charge a length-proportional amount up front, before the structural scan
	// inside DecodeGasCost runs, so the scan itself is bounded by the supplied gas
	// and can never be performed for free even if it later becomes costlier.
	scanCost := DefaultGasCost(input, false)
	ctx.GasMeter().ConsumeGas(scanCost, fmt.Sprintf("%s precompile calldata scan", d.name))
	decodeCost, ok := DecodeGasCost(method.Inputs, input)
	if !ok {
		// Calldata is structurally invalid (Unpack would reject it too); reject
		// now, without attempting the decode.
		return nil, 0, fmt.Errorf("invalid calldata encoding for %s", d.name)
	}
	// DecodeGasCost already includes scanCost; charge only the remaining
	// (string-copy) portion so the decode is priced exactly once.
	ctx.GasMeter().ConsumeGas(decodeCost-scanCost, fmt.Sprintf("%s precompile calldata decode", d.name))

	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, 0, err
	}
	em := ctx.EventManager()
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithEVMPrecompileCalledFromDelegateCall(isFromDelegateCall)
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

func HandlePaymentUsei(ctx sdk.Context, precompileAddr sdk.AccAddress, payer sdk.AccAddress, value *big.Int, bankKeeper putils.BankKeeper, evmKeeper putils.EVMKeeper, hooks *tracing.Hooks, depth int) (sdk.Coin, error) {
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
		newCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
		if hooks.OnEnter != nil {
			hooks.OnEnter(depth+1, byte(vm.CALL), evmKeeper.GetEVMAddressOrDefault(newCtx, precompileAddr), evmKeeper.GetEVMAddressOrDefault(newCtx, payer), []byte{}, GetRemainingGas(newCtx, evmKeeper), value)
		}
		if hooks.OnExit != nil {
			hooks.OnExit(depth+1, []byte{}, 0, nil, false)
		}
	}
	return coin, nil
}

func HandlePaymentUseiWei(ctx sdk.Context, precompileAddr sdk.AccAddress, payer sdk.AccAddress, value *big.Int, bankKeeper putils.BankKeeper, evmKeeper putils.EVMKeeper, hooks *tracing.Hooks, depth int) (sdk.Int, sdk.Int, error) {
	usei, wei := state.SplitUseiWeiAmount(value)
	// refund payer because the following precompile logic will debit the payments from payer's account
	// this creates a new event manager to avoid surfacing these as cosmos events
	if err := bankKeeper.SendCoinsAndWei(ctx.WithEventManager(sdk.NewEventManager()), precompileAddr, payer, usei, wei); err != nil {
		return sdk.Int{}, sdk.Int{}, err
	}
	if hooks != nil {
		newCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
		if hooks.OnEnter != nil {
			hooks.OnEnter(depth+1, byte(vm.CALL), evmKeeper.GetEVMAddressOrDefault(newCtx, precompileAddr), evmKeeper.GetEVMAddressOrDefault(newCtx, payer), []byte{}, GetRemainingGas(newCtx, evmKeeper), value)
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
func GetRemainingGas(ctx sdk.Context, evmKeeper putils.EVMKeeper) uint64 {
	return evmKeeper.GetEVMGasLimitFromCtx(ctx)
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

func MustGetABI(f embed.FS, filename string) abi.ABI {
	abiBz, err := f.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		panic(err)
	}
	return newAbi
}

func GetSeiAddressByEvmAddress(ctx sdk.Context, evmAddress common.Address, evmKeeper putils.EVMKeeper) (sdk.AccAddress, error) {
	seiAddr, associated := evmKeeper.GetSeiAddress(ctx, evmAddress)
	if !associated {
		return nil, types.NewAssociationMissingErr(evmAddress.Hex())
	}
	return seiAddr, nil
}

func GetSeiAddressFromArg(ctx sdk.Context, arg interface{}, evmKeeper putils.EVMKeeper) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	return GetSeiAddressByEvmAddress(ctx, addr, evmKeeper)
}
