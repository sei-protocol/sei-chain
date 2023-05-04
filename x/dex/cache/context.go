package dex

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type CtxKeyType string

const (
	CtxKeyExecTermSignal    = CtxKeyType("execution-termination-signals")
	CtxKeyExecutingContract = CtxKeyType("executing-contract")
)

func GetExecutingContract(ctx sdk.Context) *types.ContractInfoV2 {
	if ctx.Context() == nil {
		return nil
	}
	executingContract := ctx.Context().Value(CtxKeyExecutingContract)
	if executingContract == nil {
		return nil
	}
	contract, ok := executingContract.(types.ContractInfoV2)
	if !ok {
		return nil
	}
	return &contract
}

func GetTerminationSignals(ctx sdk.Context) *datastructures.TypedSyncMap[string, chan struct{}] {
	if ctx.Context() == nil {
		return nil
	}
	signals := ctx.Context().Value(CtxKeyExecTermSignal)
	if signals == nil {
		return nil
	}
	typedSignals, ok := signals.(*datastructures.TypedSyncMap[string, chan struct{}])
	if !ok {
		return nil
	}
	return typedSignals
}
