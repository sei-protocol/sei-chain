package pointerview

import (
	"embed"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	GetNativePointer = "getNativePointer"
	GetCW20Pointer   = "getCW20Pointer"
	GetCW721Pointer  = "getCW721Pointer"
	GetNativePointee = "getNativePointee"
	GetCW20Pointee   = "getCW20Pointee"
	GetCW721Pointee  = "getCW721Pointee"
)

const PointerViewAddress = "0x000000000000000000000000000000000000100A"

//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper           pcommon.EVMKeeper
	GetNativePointerID  []byte
	GetCW20PointerID    []byte
	GetCW721PointerID   []byte
	GetNativePointeeID  []byte
	GetCW20PointeeID    []byte
	GetCW721PointeeID   []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")
	p := &PrecompileExecutor{
		evmKeeper: evmKeeper,
	}
	for name, m := range newAbi.Methods {
		switch name {
		case GetNativePointer:
			p.GetNativePointerID = m.ID
		case GetCW20Pointer:
			p.GetCW20PointerID = m.ID
		case GetCW721Pointer:
			p.GetCW721PointerID = m.ID
		case GetNativePointee:
			p.GetNativePointeeID = m.ID
		case GetCW20Pointee:
			p.GetCW20PointeeID = m.ID
		case GetCW721Pointee:
			p.GetCW721PointeeID = m.ID
		}
	}
	return pcommon.NewPrecompile(newAbi, p, common.HexToAddress(PointerViewAddress), "pointerview"), nil
}

func (p PrecompileExecutor) RequiredGas([]byte, *abi.Method) uint64 {
	return 2000
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM) (ret []byte, err error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	switch method.Name {
	case GetNativePointer:
		return p.GetNative(ctx, method, args)
	case GetCW20Pointer:
		return p.GetCW20(ctx, method, args)
	case GetCW721Pointer:
		return p.GetCW721(ctx, method, args)
	case GetNativePointee:
		return p.GetNativePointee(ctx, method, args)
	case GetCW20Pointee:
		return p.GetCW20Pointee(ctx, method, args)
	case GetCW721Pointee:
		return p.GetCW721Pointee(ctx, method, args)
	default:
		err = fmt.Errorf("unknown method %s", method.Name)
	}
	return
}

func (p PrecompileExecutor) GetNative(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	token := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC20NativePointer(ctx, token)
	return method.Outputs.Pack(existingAddr, existingVersion, exists)
}

func (p PrecompileExecutor) GetCW20(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	addr := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC20CW20Pointer(ctx, addr)
	return method.Outputs.Pack(existingAddr, existingVersion, exists)
}

func (p PrecompileExecutor) GetCW721(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	addr := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC721CW721Pointer(ctx, addr)
	return method.Outputs.Pack(existingAddr, existingVersion, exists)
}

func (p PrecompileExecutor) GetNativePointee(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	erc20Address := args[0].(string)
	token, version, exists := p.evmKeeper.GetNativePointee(ctx, common.HexToAddress(erc20Address))
	return method.Outputs.Pack(token, version, exists)
}

func (p PrecompileExecutor) GetCW20Pointee(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	erc20Address := args[0].(common.Address)
	cw20Address, version, exists := p.evmKeeper.GetCW20Pointee(ctx, erc20Address)
	return method.Outputs.Pack(cw20Address, version, exists)
}

func (p PrecompileExecutor) GetCW721Pointee(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	erc721Address := args[0].(common.Address)
	cw721Address, version, exists := p.evmKeeper.GetCW721Pointee(ctx, erc721Address)
	return method.Outputs.Pack(cw721Address, version, exists)
}