package v575

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common/legacy/v575"
	"github.com/sei-protocol/sei-chain/utils"
)

const (
	PrecompileName   = "pointer"
	AddNativePointer = "addNativePointer"
	AddCW20Pointer   = "addCW20Pointer"
	AddCW721Pointer  = "addCW721Pointer"
)

const PointerAddress = "0x000000000000000000000000000000000000100b"

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper   pcommon.EVMKeeper
	bankKeeper  pcommon.BankKeeper
	wasmdKeeper pcommon.WasmdViewKeeper

	AddNativePointerID []byte
	AddCW20PointerID   []byte
	AddCW721PointerID  []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper, bankKeeper pcommon.BankKeeper, wasmdKeeper pcommon.WasmdViewKeeper) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:   evmKeeper,
		bankKeeper:  bankKeeper,
		wasmdKeeper: wasmdKeeper,
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

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(PointerAddress), PrecompileName), nil
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *ethabi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if readOnly {
		return nil, 0, errors.New("cannot call pointer precompile from staticcall")
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

func (p PrecompileExecutor) EVMKeeper() pcommon.EVMKeeper {
	return p.evmKeeper
}

func (p PrecompileExecutor) AddNative(ctx sdk.Context, method *ethabi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	token := args[0].(string)
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
	contractAddr, remainingGas, err := p.evmKeeper.UpsertERCNativePointer(ctx, evm, suppliedGas, token, utils.ERCMetadata{Name: name, Symbol: symbol, Decimals: decimals})
	if err != nil {
		return nil, 0, err
	}
	ret, err = method.Outputs.Pack(contractAddr)
	return
}

func (p PrecompileExecutor) AddCW20(ctx sdk.Context, method *ethabi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	cwAddr := args[0].(string)
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
	contractAddr, remainingGas, err := p.evmKeeper.UpsertERCCW20Pointer(ctx, evm, suppliedGas, cwAddr, utils.ERCMetadata{Name: name, Symbol: symbol})
	if err != nil {
		return nil, 0, err
	}
	ret, err = method.Outputs.Pack(contractAddr)
	return
}

func (p PrecompileExecutor) AddCW721(ctx sdk.Context, method *ethabi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	cwAddr := args[0].(string)
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
	contractAddr, remainingGas, err := p.evmKeeper.UpsertERCCW721Pointer(ctx, evm, suppliedGas, cwAddr, utils.ERCMetadata{Name: name, Symbol: symbol})
	if err != nil {
		return nil, 0, err
	}
	ret, err = method.Outputs.Pack(contractAddr)
	return
}
