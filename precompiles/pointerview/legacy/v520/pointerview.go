package v520

import (
	"bytes"
	"embed"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common/legacy/v520"
)

const (
	GetNativePointer = "getNativePointer"
	GetCW20Pointer   = "getCW20Pointer"
	GetCW721Pointer  = "getCW721Pointer"
)

const PointerViewAddress = "0x000000000000000000000000000000000000100A"

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type Precompile struct {
	pcommon.Precompile
	evmKeeper pcommon.EVMKeeper
	address   common.Address

	GetNativePointerID []byte
	GetCW20PointerID   []byte
	GetCW721PointerID  []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper) (*Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the pointer ABI %s", err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	p := &Precompile{
		Precompile: pcommon.Precompile{ABI: newAbi},
		evmKeeper:  evmKeeper,
		address:    common.HexToAddress(PointerViewAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GetNativePointer:
			p.GetNativePointerID = m.ID
		case GetCW20Pointer:
			p.GetCW20PointerID = m.ID
		case GetCW721Pointer:
			p.GetCW721PointerID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	return 2000
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) GetName() string {
	return "pointerview"
}

func (p Precompile) Run(evm *vm.EVM, _ common.Address, _ common.Address, input []byte, _ *big.Int, _ bool, _ bool) (ret []byte, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case GetNativePointer:
		return p.GetNative(ctx, method, args)
	case GetCW20Pointer:
		return p.GetCW20(ctx, method, args)
	case GetCW721Pointer:
		return p.GetCW721(ctx, method, args)
	default:
		err = fmt.Errorf("unknown method %s", method.Name)
	}
	return
}

func (p Precompile) GetNative(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	token := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC20NativePointer(ctx, token)
	return method.Outputs.Pack(existingAddr, existingVersion, exists)
}

func (p Precompile) GetCW20(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	addr := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC20CW20Pointer(ctx, addr)
	return method.Outputs.Pack(existingAddr, existingVersion, exists)
}

func (p Precompile) GetCW721(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, err error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	addr := args[0].(string)
	existingAddr, existingVersion, exists := p.evmKeeper.GetERC721CW721Pointer(ctx, addr)
	return method.Outputs.Pack(existingAddr, existingVersion, exists)
}
