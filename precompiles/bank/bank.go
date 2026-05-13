package bank

import (
	"embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
)

const (
	SendMethod                        = "send"
	SendNativeMethod                  = "sendNative"
	BalanceMethod                     = "balance"
	BalanceForAddressMethod           = "balance_for_address"
	AllBalancesMethod                 = "all_balances"
	AllBalancesForAddressMethod       = "all_balances_for_address"
	SpendableBalancesMethod           = "spendable_balances"
	SpendableBalancesForAddressMethod = "spendable_balances_for_address"
	TotalSupplyMethod                 = "total_supply"
	NameMethod                        = "name"
	SymbolMethod                      = "symbol"
	DecimalsMethod                    = "decimals"
	SupplyMethod                      = "supply"
	DenomMetadataMethod               = "denom_metadata"
	DenomsMetadataMethod              = "denoms_metadata"
	ParamsMethod                      = "params"
)

const (
	BankAddress = "0x0000000000000000000000000000000000001001"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	accountKeeper putils.AccountKeeper
	bankKeeper    putils.BankKeeper
	bankMsgServer putils.BankMsgServer
	evmKeeper     putils.EVMKeeper
	address       common.Address

	SendID                        []byte
	SendNativeID                  []byte
	BalanceID                     []byte
	BalanceForAddressID           []byte
	AllBalancesID                 []byte
	AllBalancesForAddressID       []byte
	SpendableBalancesID           []byte
	SpendableBalancesForAddressID []byte
	TotalSupplyID                 []byte
	NameID                        []byte
	SymbolID                      []byte
	DecimalsID                    []byte
	SupplyID                      []byte
	DenomMetadataID               []byte
	DenomsMetadataID              []byte
	ParamsID                      []byte
}

type CoinBalance struct {
	Amount *big.Int
	Denom  string
}

type DenomUnit struct {
	Denom    string
	Exponent uint32
	Aliases  []string
}

type Metadata struct {
	Description string
	DenomUnits  []DenomUnit
	Base        string
	Display     string
	Name        string
	Symbol      string
}

type SendEnabled struct {
	Denom   string
	Enabled bool
}

type Params struct {
	SendEnabled        []SendEnabled
	DefaultSendEnabled bool
}

func GetABI() abi.ABI {
	return pcommon.MustGetABI(f, "abi.json")
}

func NewPrecompile(keepers putils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := GetABI()
	p := &PrecompileExecutor{
		bankKeeper:    keepers.BankK(),
		bankMsgServer: keepers.BankMS(),
		evmKeeper:     keepers.EVMK(),
		accountKeeper: keepers.AccountK(),
		address:       common.HexToAddress(BankAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case SendMethod:
			p.SendID = m.ID
		case SendNativeMethod:
			p.SendNativeID = m.ID
		case BalanceMethod:
			p.BalanceID = m.ID
		case BalanceForAddressMethod:
			p.BalanceForAddressID = m.ID
		case AllBalancesMethod:
			p.AllBalancesID = m.ID
		case AllBalancesForAddressMethod:
			p.AllBalancesForAddressID = m.ID
		case SpendableBalancesMethod:
			p.SpendableBalancesID = m.ID
		case SpendableBalancesForAddressMethod:
			p.SpendableBalancesForAddressID = m.ID
		case TotalSupplyMethod:
			p.TotalSupplyID = m.ID
		case NameMethod:
			p.NameID = m.ID
		case SymbolMethod:
			p.SymbolID = m.ID
		case DecimalsMethod:
			p.DecimalsID = m.ID
		case SupplyMethod:
			p.SupplyID = m.ID
		case DenomMetadataMethod:
			p.DenomMetadataID = m.ID
		case DenomsMetadataMethod:
			p.DenomsMetadataID = m.ID
		case ParamsMethod:
			p.ParamsID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, p.address, "bank"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	// Needed to catch gas meter panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
		}
	}()
	switch method.Name {
	case SendMethod:
		return p.send(ctx, caller, method, args, value, readOnly)
	case SendNativeMethod:
		return p.sendNative(ctx, method, args, caller, callingContract, value, readOnly, hooks, evm)
	case BalanceMethod:
		return p.balance(ctx, method, args, value)
	case BalanceForAddressMethod:
		return p.balanceForAddress(ctx, method, args, value)
	case AllBalancesMethod:
		return p.all_balances(ctx, method, args, value)
	case AllBalancesForAddressMethod:
		return p.allBalancesForAddress(ctx, method, args, value)
	case SpendableBalancesMethod:
		return p.spendableBalances(ctx, method, args, value)
	case SpendableBalancesForAddressMethod:
		return p.spendableBalancesForAddress(ctx, method, args, value)
	case TotalSupplyMethod:
		return p.totalSupply(ctx, method, args, value)
	case NameMethod:
		return p.name(ctx, method, args, value)
	case SymbolMethod:
		return p.symbol(ctx, method, args, value)
	case DecimalsMethod:
		return p.decimals(ctx, method, args, value)
	case SupplyMethod:
		return p.supply(ctx, method, args, value)
	case DenomMetadataMethod:
		return p.denomMetadata(ctx, method, args, value)
	case DenomsMetadataMethod:
		return p.denomsMetadata(ctx, method, args, value)
	case ParamsMethod:
		return p.params(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) send(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}, value *big.Int, readOnly bool) ([]byte, uint64, error) {
	if readOnly {
		return nil, 0, errors.New("cannot call send from staticcall")
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
		return nil, 0, err
	}
	denom := args[2].(string)
	if denom == "" {
		return nil, 0, errors.New("invalid denom")
	}
	pointer, _, exists := p.evmKeeper.GetERC20NativePointer(ctx, denom)
	if !exists || pointer.Cmp(caller) != 0 {
		return nil, 0, fmt.Errorf("only pointer %s can send %s but got %s", pointer.Hex(), denom, caller.Hex())
	}
	amount := args[3].(*big.Int)
	if amount.Cmp(utils.Big0) == 0 {
		// short circuit
		bz, err := method.Outputs.Pack(true)
		return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
	}
	senderSeiAddr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, 0, err
	}
	receiverSeiAddr, err := p.accAddressFromArg(ctx, args[1])
	if err != nil {
		return nil, 0, err
	}

	msg := &banktypes.MsgSend{
		FromAddress: senderSeiAddr.String(),
		ToAddress:   receiverSeiAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, sdk.NewIntFromBigInt(amount))),
	}

	err = msg.ValidateBasic()
	if err != nil {
		return nil, 0, err
	}

	if _, err = p.bankMsgServer.Send(sdk.WrapSDKContext(ctx), msg); err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(true)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) sendNative(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address, callingContract common.Address, value *big.Int, readOnly bool, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, uint64, error) {
	if readOnly {
		return nil, 0, errors.New("cannot call sendNative from staticcall")
	}
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall sendNative")
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	if value == nil || value.Sign() == 0 {
		return nil, 0, errors.New("set `value` field to non-zero to send")
	}

	senderSeiAddr, ok := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !ok {
		return nil, 0, errors.New("invalid addr")
	}

	receiverAddr, ok := (args[0]).(string)
	if !ok || receiverAddr == "" {
		return nil, 0, errors.New("invalid addr")
	}

	receiverSeiAddr, err := sdk.AccAddressFromBech32(receiverAddr)
	if err != nil {
		return nil, 0, err
	}

	usei, wei, err := pcommon.HandlePaymentUseiWei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), senderSeiAddr, value, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
	if err != nil {
		return nil, 0, err
	}

	if err := p.bankKeeper.SendCoinsAndWei(ctx, senderSeiAddr, receiverSeiAddr, usei, wei); err != nil {
		return nil, 0, err
	}
	accExists := p.accountKeeper.HasAccount(ctx, receiverSeiAddr)
	if !accExists {
		defer metrics.SafeTelemetryIncrCounter(1, "new", "account")
		p.accountKeeper.SetAccount(ctx, p.accountKeeper.NewAccountWithAddress(ctx, receiverSeiAddr))
	}

	if hooks != nil {
		newCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
		remainingGas := pcommon.GetRemainingGas(newCtx, p.evmKeeper)
		if hooks.OnEnter != nil {
			hooks.OnEnter(evm.GetDepth()+1, byte(vm.CALL), caller, p.evmKeeper.GetEVMAddressOrDefault(newCtx, receiverSeiAddr), []byte{}, remainingGas, value)
		}
		defer func() {
			if hooks.OnExit != nil {
				hooks.OnExit(evm.GetDepth()+1, []byte{}, 0, nil, false)
			}
		}()
	}

	bz, err := method.Outputs.Pack(true)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) balance(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	addr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, 0, err
	}
	denom := args[1].(string)
	if denom == "" {
		return nil, 0, errors.New("invalid denom")
	}

	bz, err := method.Outputs.Pack(p.bankKeeper.GetBalance(ctx, addr, denom).Amount.BigInt())
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) balanceForAddress(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	addr, err := accAddressFromStringArg(args[0])
	if err != nil {
		return nil, 0, err
	}
	denom := args[1].(string)
	if denom == "" {
		return nil, 0, errors.New("invalid denom")
	}

	bz, err := method.Outputs.Pack(p.bankKeeper.GetBalance(ctx, addr, denom).Amount.BigInt())
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) all_balances(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	addr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, 0, err
	}

	coinBalances := coinsToCoinBalances(p.bankKeeper.GetAllBalances(ctx, addr))

	bz, err := method.Outputs.Pack(coinBalances)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) allBalancesForAddress(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	addr, err := accAddressFromStringArg(args[0])
	if err != nil {
		return nil, 0, err
	}

	coinBalances := coinsToCoinBalances(p.bankKeeper.GetAllBalances(ctx, addr))

	bz, err := method.Outputs.Pack(coinBalances)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) spendableBalances(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	addr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(coinsToCoinBalances(p.bankKeeper.SpendableCoins(ctx, addr)))
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) spendableBalancesForAddress(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	addr, err := accAddressFromStringArg(args[0])
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(coinsToCoinBalances(p.bankKeeper.SpendableCoins(ctx, addr)))
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) totalSupply(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	coins := sdk.NewCoins()
	p.bankKeeper.IterateTotalSupply(ctx, func(coin sdk.Coin) bool {
		coins = coins.Add(coin)
		return false
	})

	bz, err := method.Outputs.Pack(coinsToCoinBalances(coins))
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) name(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, 0, fmt.Errorf("denom %s not found", denom)
	}
	bz, err := method.Outputs.Pack(metadata.Name)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) symbol(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, 0, fmt.Errorf("denom %s not found", denom)
	}
	bz, err := method.Outputs.Pack(metadata.Symbol)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) decimals(ctx sdk.Context, method *abi.Method, _ []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	// all native tokens are integer-based, returns decimals for microdenom (usei)
	bz, err := method.Outputs.Pack(uint8(0))
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) supply(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	denom := args[0].(string)
	coin := p.bankKeeper.GetSupply(ctx, denom)
	bz, err := method.Outputs.Pack(coin.Amount.BigInt())
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) denomMetadata(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	denom := args[0].(string)
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, denom)
	if !found {
		return nil, 0, fmt.Errorf("denom %s not found", denom)
	}
	bz, err := method.Outputs.Pack(metadataToOutput(metadata))
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) denomsMetadata(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	metadatas := []Metadata{}
	p.bankKeeper.IterateAllDenomMetaData(ctx, func(metadata banktypes.Metadata) bool {
		metadatas = append(metadatas, metadataToOutput(metadata))
		return false
	})
	bz, err := method.Outputs.Pack(metadatas)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) params(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(paramsToOutput(p.bankKeeper.GetParams(ctx)))
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
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

func accAddressFromStringArg(arg interface{}) (sdk.AccAddress, error) {
	addr, ok := arg.(string)
	if !ok || addr == "" {
		return nil, errors.New("invalid addr")
	}
	return sdk.AccAddressFromBech32(addr)
}

func coinsToCoinBalances(coins sdk.Coins) []CoinBalance {
	coinBalances := make([]CoinBalance, 0, len(coins))
	for _, coin := range coins {
		coinBalances = append(coinBalances, CoinBalance{
			Amount: coin.Amount.BigInt(),
			Denom:  coin.Denom,
		})
	}
	return coinBalances
}

func metadataToOutput(metadata banktypes.Metadata) Metadata {
	denomUnits := make([]DenomUnit, 0, len(metadata.DenomUnits))
	for _, unit := range metadata.DenomUnits {
		if unit == nil {
			continue
		}
		denomUnits = append(denomUnits, DenomUnit{
			Denom:    unit.Denom,
			Exponent: unit.Exponent,
			Aliases:  append([]string(nil), unit.Aliases...),
		})
	}
	return Metadata{
		Description: metadata.Description,
		DenomUnits:  denomUnits,
		Base:        metadata.Base,
		Display:     metadata.Display,
		Name:        metadata.Name,
		Symbol:      metadata.Symbol,
	}
}

func paramsToOutput(params banktypes.Params) Params {
	sendEnabled := make([]SendEnabled, 0, len(params.SendEnabled))
	for _, sendEnabledParam := range params.SendEnabled {
		if sendEnabledParam == nil {
			continue
		}
		sendEnabled = append(sendEnabled, SendEnabled{
			Denom:   sendEnabledParam.Denom,
			Enabled: sendEnabledParam.Enabled,
		})
	}
	return Params{
		SendEnabled:        sendEnabled,
		DefaultSendEnabled: params.DefaultSendEnabled,
	}
}

func (PrecompileExecutor) IsTransaction(method string) bool {
	switch method {
	case SendMethod:
		return true
	case SendNativeMethod:
		return true
	default:
		return false
	}
}

func (p PrecompileExecutor) EVMKeeper() putils.EVMKeeper {
	return p.evmKeeper
}
