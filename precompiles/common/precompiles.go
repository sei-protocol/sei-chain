package common

import (
	"errors"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"
)

type Contexter interface {
	Ctx() sdk.Context
}

type Precompile struct {
	abi.ABI
}

func (p Precompile) RequiredGas(input []byte, isTransaction bool) uint64 {
	argsBz := input[4:] // first four bytes are method ID

	if isTransaction {
		return storetypes.KVGasConfig().WriteCostFlat + (storetypes.KVGasConfig().WriteCostPerByte * uint64(len(argsBz)))
	}

	return storetypes.KVGasConfig().ReadCostFlat + (storetypes.KVGasConfig().ReadCostPerByte * uint64(len(argsBz)))
}

func (p Precompile) Prepare(evm *vm.EVM, input []byte) (sdk.Context, *abi.Method, []interface{}, error) {
	ctxer, ok := evm.StateDB.(Contexter)
	if !ok {
		return sdk.Context{}, nil, nil, errors.New("cannot get context from EVM")
	}
	methodID := input[:4]
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

func AssertArgsLength(args []interface{}, length int) {
	if len(args) != length {
		panic(fmt.Sprintf("expected %d arguments but got %d", length, len(args)))
	}
}
