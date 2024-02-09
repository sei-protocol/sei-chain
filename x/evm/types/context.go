package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

type CtxEVMKeyType string

const CtxEVMKey = CtxEVMKeyType("evm")

func SetCtxEVM(ctx sdk.Context, evm *vm.EVM) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), CtxEVMKey, evm))
}

func GetCtxEVM(ctx sdk.Context) *vm.EVM {
	rawVal := ctx.Context().Value(CtxEVMKey)
	if rawVal == nil {
		return nil
	}
	evm, ok := rawVal.(*vm.EVM)
	if !ok {
		return nil
	}
	return evm
}
