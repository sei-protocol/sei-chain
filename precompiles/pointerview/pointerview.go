package pointerview

import (
	"embed"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

const (
	GetNativePointer = "getNativePointer"
	GetCW20Pointer   = "getCW20Pointer"
	GetCW721Pointer  = "getCW721Pointer"
	GetCW1155Pointer = "getCW1155Pointer"
)

const PointerViewAddress = "0x000000000000000000000000000000000000100A"

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper utils.EVMKeeper

	GetNativePointerID []byte
	GetCW20PointerID   []byte
	GetCW721PointerID  []byte
	GetCW1155PointerID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper: keepers.EVMK(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GetNativePointer:
			p.GetNativePointerID = m.ID
		case GetCW20Pointer:
			p.GetCW20PointerID = m.ID
		case GetCW721Pointer:
			p.GetCW721PointerID = m.ID
		case GetCW1155Pointer:
			p.GetCW1155PointerID = m.ID
		}
	}

	return pcommon.NewPrecompile(newAbi, p, common.HexToAddress(PointerViewAddress), "pointerview"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas([]byte, *abi.Method) uint64 {
	return 2000
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) (ret []byte, err error) {
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
	case GetCW1155Pointer:
		return p.GetCW1155(ctx, method, args)
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

func (p PrecompileExecutor) GetCW1155(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	addr := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC1155CW1155Pointer(ctx, addr)
	return method.Outputs.Pack(existingAddr, existingVersion, exists)
}
