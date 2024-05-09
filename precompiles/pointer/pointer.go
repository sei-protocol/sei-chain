package pointer

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	AddNativePointer = "addNativePointer"
	AddCW20Pointer   = "addCW20Pointer"
	AddCW721Pointer  = "addCW721Pointer"
)

const PointerAddress = "0x000000000000000000000000000000000000100b"

var _ vm.PrecompiledContract = &Precompile{}
var _ vm.DynamicGasPrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type Precompile struct {
	pcommon.Precompile
	evmKeeper   pcommon.EVMKeeper
	bankKeeper  pcommon.BankKeeper
	wasmdKeeper pcommon.WasmdViewKeeper
	address     common.Address

	AddNativePointerID []byte
	AddCW20PointerID   []byte
	AddCW721PointerID  []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper, bankKeeper pcommon.BankKeeper, wasmdKeeper pcommon.WasmdViewKeeper) (*Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the pointer ABI %s", err)
	}

	newAbi, err := ethabi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	p := &Precompile{
		Precompile:  pcommon.Precompile{ABI: newAbi},
		evmKeeper:   evmKeeper,
		bankKeeper:  bankKeeper,
		wasmdKeeper: wasmdKeeper,
		address:     common.HexToAddress(PointerAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case AddNativePointer:
			p.AddNativePointerID = m.ID
		case AddCW20Pointer:
			p.AddCW20PointerID = m.ID
		case AddCW721Pointer:
			p.AddCW721PointerID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	// gas is calculated dynamically
	return pcommon.UnknownMethodCallGas
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) GetName() string {
	return "pointer"
}

func (p Precompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, suppliedGas uint64, value *big.Int, _ *tracing.Hooks, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	defer func() {
		if err != nil {
			evm.StateDB.(*state.DBImpl).SetPrecompileError(err)
		}
	}()
	if readOnly {
		return nil, 0, errors.New("cannot call pointer precompile from staticcall")
	}
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, 0, err
	}
	if caller.Cmp(callingContract) != 0 {
		return nil, 0, errors.New("cannot delegatecall pointer")
	}

	switch method.Name {
	case AddNativePointer:
		return p.AddNative(ctx, method, caller, args, value, evm, suppliedGas)
	case AddCW20Pointer:
		return p.AddCW20(ctx, method, caller, args, value, evm, suppliedGas)
	case AddCW721Pointer:
		return p.AddCW721(ctx, method, caller, args, value, evm, suppliedGas)
	default:
		err = fmt.Errorf("unknown method %s", method.Name)
	}
	return
}

func (p Precompile) Run(*vm.EVM, common.Address, common.Address, []byte, *big.Int, bool) ([]byte, error) {
	panic("static gas Run is not implemented for dynamic gas precompile")
}

func (p Precompile) AddNative(ctx sdk.Context, method *ethabi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	token := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC20NativePointer(ctx, token)
	if exists && existingVersion >= native.CurrentVersion {
		return nil, 0, fmt.Errorf("pointer at %s with version %d exists when trying to set pointer for version %d", existingAddr.Hex(), existingVersion, native.CurrentVersion)
	}
	metadata, metadataExists := p.bankKeeper.GetDenomMetaData(ctx, token)
	if !metadataExists {
		return nil, 0, fmt.Errorf("denom %s does not have metadata stored and thus can only have its pointer set through gov proposal", token)
	}
	name := metadata.Name
	symbol := metadata.Symbol
	var decimals uint8
	for _, denomUnit := range metadata.DenomUnits {
		if denomUnit.Exponent > uint32(decimals) && denomUnit.Exponent <= math.MaxUint8 {
			decimals = uint8(denomUnit.Exponent)
			name = denomUnit.Denom
			symbol = denomUnit.Denom
			if len(denomUnit.Aliases) > 0 {
				name = denomUnit.Aliases[0]
			}
		}
	}
	constructorArguments := []interface{}{
		token, name, symbol, decimals,
	}

	packedArgs, err := native.GetParsedABI().Pack("", constructorArguments...)
	if err != nil {
		panic(err)
	}
	bin := append(native.GetBin(), packedArgs...)
	if value == nil {
		value = utils.Big0
	}
	ret, contractAddr, remainingGas, err := evm.Create(vm.AccountRef(caller), bin, suppliedGas, value)
	if err != nil {
		return
	}
	err = p.evmKeeper.SetERC20NativePointer(ctx, token, contractAddr)
	if err != nil {
		return
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "native"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, contractAddr.Hex()), sdk.NewAttribute(types.AttributeKeyPointee, token),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", native.CurrentVersion))))
	ret, err = method.Outputs.Pack(contractAddr)
	return
}

func (p Precompile) AddCW20(ctx sdk.Context, method *ethabi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	cwAddr := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC20CW20Pointer(ctx, cwAddr)
	if exists && existingVersion >= cw20.CurrentVersion {
		return nil, 0, fmt.Errorf("pointer at %s with version %d exists when trying to set pointer for version %d", existingAddr.Hex(), existingVersion, cw20.CurrentVersion)
	}
	cwAddress, err := sdk.AccAddressFromBech32(cwAddr)
	if err != nil {
		return nil, 0, err
	}
	res, err := p.wasmdKeeper.QuerySmart(ctx, cwAddress, []byte("{\"token_info\":{}}"))
	if err != nil {
		return nil, 0, err
	}
	formattedRes := map[string]interface{}{}
	if err := json.Unmarshal(res, &formattedRes); err != nil {
		return nil, 0, err
	}
	name := formattedRes["name"].(string)
	symbol := formattedRes["symbol"].(string)
	constructorArguments := []interface{}{
		cwAddr, name, symbol,
	}

	packedArgs, err := cw20.GetParsedABI().Pack("", constructorArguments...)
	if err != nil {
		panic(err)
	}
	bin := append(cw20.GetBin(), packedArgs...)
	if value == nil {
		value = utils.Big0
	}
	ret, contractAddr, remainingGas, err := evm.Create(vm.AccountRef(caller), bin, suppliedGas, value)
	if err != nil {
		return
	}
	err = p.evmKeeper.SetERC20CW20Pointer(ctx, cwAddr, contractAddr)
	if err != nil {
		return
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "cw20"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, contractAddr.Hex()), sdk.NewAttribute(types.AttributeKeyPointee, cwAddr),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", cw20.CurrentVersion))))
	ret, err = method.Outputs.Pack(contractAddr)
	return
}

func (p Precompile) AddCW721(ctx sdk.Context, method *ethabi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	cwAddr := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC721CW721Pointer(ctx, cwAddr)
	if exists && existingVersion >= cw721.CurrentVersion {
		return nil, 0, fmt.Errorf("pointer at %s with version %d exists when trying to set pointer for version %d", existingAddr.Hex(), existingVersion, cw721.CurrentVersion)
	}
	cwAddress, err := sdk.AccAddressFromBech32(cwAddr)
	if err != nil {
		return nil, 0, err
	}
	res, err := p.wasmdKeeper.QuerySmart(ctx, cwAddress, []byte("{\"contract_info\":{}}"))
	if err != nil {
		return nil, 0, err
	}
	formattedRes := map[string]interface{}{}
	if err := json.Unmarshal(res, &formattedRes); err != nil {
		return nil, 0, err
	}
	name := formattedRes["name"].(string)
	symbol := formattedRes["symbol"].(string)
	constructorArguments := []interface{}{
		cwAddr, name, symbol,
	}

	packedArgs, err := cw721.GetParsedABI().Pack("", constructorArguments...)
	if err != nil {
		panic(err)
	}
	bin := append(cw721.GetBin(), packedArgs...)
	if value == nil {
		value = utils.Big0
	}
	ret, contractAddr, remainingGas, err := evm.Create(vm.AccountRef(caller), bin, suppliedGas, value)
	if err != nil {
		return
	}
	err = p.evmKeeper.SetERC721CW721Pointer(ctx, cwAddr, contractAddr)
	if err != nil {
		return
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "cw721"),
		sdk.NewAttribute(types.AttributeKeyPointerAddress, contractAddr.Hex()), sdk.NewAttribute(types.AttributeKeyPointee, cwAddr),
		sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", cw721.CurrentVersion))))
	ret, err = method.Outputs.Pack(contractAddr)
	return
}
