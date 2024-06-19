package bank

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/tracers"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	SendMethod        = "send"
	SendNativeMethod  = "sendNative"
	BalanceMethod     = "balance"
	AllBalancesMethod = "all_balances"
	NameMethod        = "name"
	SymbolMethod      = "symbol"
	DecimalsMethod    = "decimals"
	SupplyMethod      = "supply"
)

const (
	BankAddress = "0x0000000000000000000000000000000000001001"
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

func GetABI() abi.ABI {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		panic(err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		panic(err)
	}
	return newAbi
}

type Precompile struct {
	pcommon.Precompile
	bankKeeper pcommon.BankKeeper
	evmKeeper  pcommon.EVMKeeper
	address    common.Address

	SendID        []byte
	SendNativeID  []byte
	BalanceID     []byte
	AllBalancesID []byte
	NameID        []byte
	SymbolID      []byte
	DecimalsID    []byte
	SupplyID      []byte
}

type CoinBalance struct {
	Amount *big.Int
	Denom  string
}

func NewPrecompile(bankKeeper pcommon.BankKeeper, evmKeeper pcommon.EVMKeeper) (*Precompile, error) {
	newAbi := GetABI()

	p := &Precompile{
		Precompile: pcommon.Precompile{ABI: newAbi},
		bankKeeper: bankKeeper,
		evmKeeper:  evmKeeper,
		address:    common.HexToAddress(BankAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case SendMethod:
			p.SendID = m.ID
		case SendNativeMethod:
			p.SendNativeID = m.ID
		case BalanceMethod:
			p.BalanceID = m.ID
		case AllBalancesMethod:
			p.AllBalancesID = m.ID
		case NameMethod:
			p.NameID = m.ID
		case SymbolMethod:
			p.SymbolID = m.ID
		case DecimalsMethod:
			p.DecimalsID = m.ID
		case SupplyMethod:
			p.SupplyID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID, err := pcommon.ExtractMethodID(input)
	if err != nil {
		return pcommon.UnknownMethodCallGas
	}

	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return pcommon.UnknownMethodCallGas
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method.Name))
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) GetName() string {
	return "bank"
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, value *big.Int, readOnly bool) (bz []byte, err error) {
	defer func() {
		if err != nil {
			evm.StateDB.(*state.DBImpl).SetPrecompileError(err)
		}
	}()
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case SendMethod:
		return p.send(ctx, caller, method, args, value, readOnly)
	case SendNativeMethod:
		return p.sendNative(ctx, method, args, caller, callingContract, value, readOnly)
	case BalanceMethod:
		return p.balance(ctx, method, args, value)
	case AllBalancesMethod:
		return p.all_balances(ctx, method, args, value)
	case NameMethod:
		return p.name(ctx, method, args, value)
	case SymbolMethod:
		return p.symbol(ctx, method, args, value)
	case DecimalsMethod:
		return p.decimals(ctx, method, args, value)
	case SupplyMethod:
		return p.totalSupply(ctx, method, args, value)
	}
	return
}

func (p Precompile) send(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}, value *big.Int, readOnly bool) ([]byte, error) {
	if readOnly {
		return nil, errors.New("cannot call send from staticcall")
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
		return nil, err
	}
	denom := args[2].(string)
	if denom == "" {
		return nil, errors.New("invalid denom")
	}
	pointer, _, exists := p.evmKeeper.GetERC20NativePointer(ctx, denom)
	if !exists || pointer.Cmp(caller) != 0 {
		return nil, fmt.Errorf("only pointer %s can send %s but got %s", pointer.Hex(), denom, caller.Hex())
	}
	amount := args[3].(*big.Int)
	if amount.Cmp(utils.Big0) == 0 {
		// short circuit
		return method.Outputs.Pack(true)
	}
	senderSeiAddr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, err
	}
	receiverSeiAddr, err := p.accAddressFromArg(ctx, args[1])
	if err != nil {
		return nil, err
	}

	if err := p.bankKeeper.SendCoins(ctx, senderSeiAddr, receiverSeiAddr, sdk.NewCoins(sdk.NewCoin(denom, sdk.NewIntFromBigInt(amount)))); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}

func (p Precompile) sendNative(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address, callingContract common.Address, value *big.Int, readOnly bool) ([]byte, error) {
	if readOnly {
		return nil, errors.New("cannot call sendNative from staticcall")
	}
	if caller.Cmp(callingContract) != 0 {
		return nil, errors.New("cannot delegatecall sendNative")
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	if value == nil || value.Sign() == 0 {
		return nil, errors.New("set `value` field to non-zero to send")
	}

	senderSeiAddr, ok := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !ok {
		return nil, errors.New("invalid addr")
	}

	receiverAddr, ok := (args[0]).(string)
	if !ok || receiverAddr == "" {
		return nil, errors.New("invalid addr")
	}

	receiverSeiAddr, err := sdk.AccAddressFromBech32(receiverAddr)
	if err != nil {
		return nil, err
	}

	precompiledSeiAddr := p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address)

	usei, wei, err := pcommon.HandlePaymentUseiWei(ctx, precompiledSeiAddr, senderSeiAddr, value, p.bankKeeper)
	if err != nil {
		return nil, err
	}

	if hooks := tracers.GetCtxEthTracingHooks(ctx); hooks != nil && hooks.OnBalanceChange != nil && (value.Sign() != 0) {
		tracers.TraceTransferEVMValue(ctx, hooks, p.bankKeeper, precompiledSeiAddr, p.address, senderSeiAddr, caller, value)
	}

	if err := p.bankKeeper.SendCoinsAndWei(ctx, senderSeiAddr, receiverSeiAddr, usei, wei); err != nil {
		return nil, err
	}

	if hooks := tracers.GetCtxEthTracingHooks(ctx); hooks != nil && hooks.OnBalanceChange != nil && (value.Sign() != 0) {
		// The SendCoinsAndWei function above works with Sei addresses that haven't been associated here. Hence we cannot
		// use `GetEVMAddress` and enforce to have a mapping. So we use GetEVMAddressOrDefault to get the EVM address.
		receveirEvmAddr := p.evmKeeper.GetEVMAddressOrDefault(ctx, receiverSeiAddr)

		tracers.TraceTransferEVMValue(ctx, hooks, p.bankKeeper, senderSeiAddr, caller, receiverSeiAddr, receveirEvmAddr, value)
	}

	return method.Outputs.Pack(true)
}

func (p Precompile) balance(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}

	addr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, err
	}
	denom := args[1].(string)
	if denom == "" {
		return nil, errors.New("invalid denom")
	}

	return method.Outputs.Pack(p.bankKeeper.GetBalance(ctx, addr, denom).Amount.BigInt())
}

func (p Precompile) all_balances(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	addr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, err
	}

	coins := p.bankKeeper.GetAllBalances(ctx, addr)

	// convert to coin balance structs
	coinBalances := make([]CoinBalance, 0, len(coins))

	for _, coin := range coins {
		coinBalances = append(coinBalances, CoinBalance{
			Amount: coin.Amount.BigInt(),
			Denom:  coin.Denom,
		})
	}

	return method.Outputs.Pack(coinBalances)
}

func (p Precompile) name(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, fmt.Errorf("denom %s not found", denom)
	}
	return method.Outputs.Pack(metadata.Name)
}

func (p Precompile) symbol(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, fmt.Errorf("denom %s not found", denom)
	}
	return method.Outputs.Pack(metadata.Symbol)
}

func (p Precompile) decimals(_ sdk.Context, method *abi.Method, _ []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	// all native tokens are integer-based, returns decimals for microdenom (usei)
	return method.Outputs.Pack(uint8(0))
}

func (p Precompile) totalSupply(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	denom := args[0].(string)
	coin := p.bankKeeper.GetSupply(ctx, denom)
	return method.Outputs.Pack(coin.Amount.BigInt())
}

func (p Precompile) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	seiAddr, found := p.evmKeeper.GetSeiAddress(ctx, addr)
	if !found {
		// return the casted version instead
		return sdk.AccAddress(addr[:]), nil
	}
	return seiAddr, nil
}

func (Precompile) IsTransaction(method string) bool {
	switch method {
	case SendMethod:
		return true
	case SendNativeMethod:
		return true
	default:
		return false
	}
}

func (p Precompile) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("precompile", "bank")
}
