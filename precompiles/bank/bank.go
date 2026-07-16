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
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
)

const (
	SendMethod              = "send"
	SendNativeMethod        = "sendNative"
	BalanceMethod           = "balance"
	AllBalancesMethod       = "all_balances"
	NameMethod              = "name"
	SymbolMethod            = "symbol"
	DecimalsMethod          = "decimals"
	SupplyMethod            = "supply"
	SpendableBalancesMethod = "spendableBalances"
	TotalSupplyMethod       = "totalSupply"
	ParamsMethod            = "params"
	DenomMetadataMethod     = "denomMetadata"
	DenomsMetadataMethod    = "denomsMetadata"
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
	bankQuerier   putils.BankQuerier
	evmKeeper     putils.EVMKeeper
	address       common.Address

	SendID              []byte
	SendNativeID        []byte
	BalanceID           []byte
	AllBalancesID       []byte
	NameID              []byte
	SymbolID            []byte
	DecimalsID          []byte
	SupplyID            []byte
	SpendableBalancesID []byte
	TotalSupplyID       []byte
	ParamsID            []byte
	DenomMetadataID     []byte
	DenomsMetadataID    []byte
}

type CoinBalance struct {
	Amount *big.Int
	Denom  string
}

type SendEnabled struct {
	Denom   string
	Enabled bool
}

type Params struct {
	SendEnabled        []SendEnabled
	DefaultSendEnabled bool
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

func GetABI() abi.ABI {
	return pcommon.MustGetABI(f, "abi.json")
}

func NewPrecompile(keepers putils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := GetABI()
	p := &PrecompileExecutor{
		bankKeeper:    keepers.BankK(),
		bankMsgServer: keepers.BankMS(),
		bankQuerier:   keepers.BankQ(),
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
		case SpendableBalancesMethod:
			p.SpendableBalancesID = m.ID
		case TotalSupplyMethod:
			p.TotalSupplyID = m.ID
		case ParamsMethod:
			p.ParamsID = m.ID
		case DenomMetadataMethod:
			p.DenomMetadataID = m.ID
		case DenomsMetadataMethod:
			p.DenomsMetadataID = m.ID
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
	case AllBalancesMethod:
		return p.all_balances(ctx, method, args, value)
	case NameMethod:
		return p.name(ctx, method, args, value)
	case SymbolMethod:
		return p.symbol(ctx, method, args, value)
	case DecimalsMethod:
		return p.decimals(ctx, method, args, value)
	case SupplyMethod:
		return p.supply(ctx, method, args, value)
	case SpendableBalancesMethod:
		return p.spendableBalances(ctx, method, args, value)
	case TotalSupplyMethod:
		return p.totalSupply(ctx, method, args, value)
	case ParamsMethod:
		return p.params(ctx, method, args, value)
	case DenomMetadataMethod:
		return p.denomMetadata(ctx, method, args, value)
	case DenomsMetadataMethod:
		return p.denomsMetadata(ctx, method, args, value)
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

	coins := p.bankKeeper.GetAllBalances(ctx, addr)

	// convert to coin balance structs
	coinBalances := make([]CoinBalance, 0, len(coins))

	for _, coin := range coins {
		coinBalances = append(coinBalances, CoinBalance{
			Amount: coin.Amount.BigInt(),
			Denom:  coin.Denom,
		})
	}

	bz, err := method.Outputs.Pack(coinBalances)
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

func (p PrecompileExecutor) spendableBalances(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	seiAddr, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	req := &banktypes.QuerySpendableBalancesRequest{
		Address: seiAddr.String(),
		Pagination: &query.PageRequest{
			Key: args[1].([]byte),
		},
	}

	resp, err := p.bankQuerier.SpendableBalances(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	balances := make([]CoinBalance, 0, len(resp.Balances))
	for _, coin := range resp.Balances {
		balances = append(balances, CoinBalance{
			Amount: coin.Amount.BigInt(),
			Denom:  coin.Denom,
		})
	}
	var nextKey []byte
	if resp.Pagination != nil {
		nextKey = resp.Pagination.NextKey
	}

	bz, err := method.Outputs.Pack(balances, nextKey)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) totalSupply(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	req := &banktypes.QueryTotalSupplyRequest{
		Pagination: &query.PageRequest{
			Key: args[0].([]byte),
		},
	}

	resp, err := p.bankQuerier.TotalSupply(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	supply := make([]CoinBalance, 0, len(resp.Supply))
	for _, coin := range resp.Supply {
		supply = append(supply, CoinBalance{
			Amount: coin.Amount.BigInt(),
			Denom:  coin.Denom,
		})
	}
	var nextKey []byte
	if resp.Pagination != nil {
		nextKey = resp.Pagination.NextKey
	}

	bz, err := method.Outputs.Pack(supply, nextKey)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) params(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	resp, err := p.bankQuerier.Params(sdk.WrapSDKContext(ctx), &banktypes.QueryParamsRequest{})
	if err != nil {
		return nil, 0, err
	}

	sendEnabled := make([]SendEnabled, 0, len(resp.Params.SendEnabled))
	for _, se := range resp.Params.SendEnabled {
		if se == nil {
			continue
		}
		sendEnabled = append(sendEnabled, SendEnabled{
			Denom:   se.Denom,
			Enabled: se.Enabled,
		})
	}
	params := Params{
		SendEnabled:        sendEnabled,
		DefaultSendEnabled: resp.Params.DefaultSendEnabled,
	}

	bz, err := method.Outputs.Pack(params)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) denomMetadata(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	req := &banktypes.QueryDenomMetadataRequest{
		Denom: args[0].(string),
	}

	resp, err := p.bankQuerier.DenomMetadata(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(convertMetadataToPrecompileType(resp.Metadata))
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) denomsMetadata(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	req := &banktypes.QueryDenomsMetadataRequest{
		Pagination: &query.PageRequest{
			Key: args[0].([]byte),
		},
	}

	resp, err := p.bankQuerier.DenomsMetadata(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	metadatas := make([]Metadata, 0, len(resp.Metadatas))
	for _, metadata := range resp.Metadatas {
		metadatas = append(metadatas, convertMetadataToPrecompileType(metadata))
	}
	var nextKey []byte
	if resp.Pagination != nil {
		nextKey = resp.Pagination.NextKey
	}

	bz, err := method.Outputs.Pack(metadatas, nextKey)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func convertMetadataToPrecompileType(metadata banktypes.Metadata) Metadata {
	denomUnits := make([]DenomUnit, 0, len(metadata.DenomUnits))
	for _, denomUnit := range metadata.DenomUnits {
		if denomUnit == nil {
			continue
		}
		aliases := denomUnit.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		denomUnits = append(denomUnits, DenomUnit{
			Denom:    denomUnit.Denom,
			Exponent: denomUnit.Exponent,
			Aliases:  aliases,
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
