package ante

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMRouterDecorator struct {
	defaultAnteHandler sdk.AnteHandler
	evmAnteHandler     sdk.AnteHandler
}

func NewEVMRouterDecorator(defaultAnteHandler sdk.AnteHandler, evmAnteHandler sdk.AnteHandler) *EVMRouterDecorator {
	return &EVMRouterDecorator{
		defaultAnteHandler: defaultAnteHandler,
		evmAnteHandler:     evmAnteHandler,
	}
}

func (r EVMRouterDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	// check if any message is towards evm module
	hasEVMMsg := false
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *types.MsgEVMTransaction:
			hasEVMMsg = true
		default:
			continue
		}
	}

	if !hasEVMMsg {
		return r.defaultAnteHandler(ctx, tx, simulate)
	}

	// A tx that has EVM message must have exactly one message
	if len(tx.GetMsgs()) != 1 {
		return ctx, errors.New("EVM tx must have exactly one message")
	}

	return r.evmAnteHandler(ctx, tx, simulate)
}
