package ante

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMRouterDecorator struct {
	defaultRoute sdk.AnteHandler
	evmRoute     sdk.AnteHandler
}

func NewEVMRouterDecorator(defaultRoute sdk.AnteHandler, evmRoute sdk.AnteHandler) *EVMRouterDecorator {
	return &EVMRouterDecorator{
		defaultRoute: defaultRoute,
		evmRoute:     evmRoute,
	}
}

func (r EVMRouterDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	// check if any message is towards evm module
	hasEVMMsg := false
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *types.MsgEVMTransaction:
			hasEVMMsg = true
			break
		default:
			continue
		}
	}

	if !hasEVMMsg {
		return r.defaultRoute(ctx, tx, simulate)
	}

	// A tx that has EVM message must have exactly one message
	if len(tx.GetMsgs()) != 1 {
		return ctx, errors.New("EVM tx must have exactly one message")
	}

	return r.evmRoute(ctx, tx, simulate)
}
