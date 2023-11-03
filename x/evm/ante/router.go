package ante

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMRouterDecorator struct {
	defaultAnteHandler sdk.AnteHandler
	evmAnteHandler     sdk.AnteHandler

	defaultAnteDepGenerator sdk.AnteDepGenerator
	evmAnteDepGenerator     sdk.AnteDepGenerator
}

func NewEVMRouterDecorator(defaultAnteHandler sdk.AnteHandler, evmAnteHandler sdk.AnteHandler, defaultAnteDepGenerator sdk.AnteDepGenerator, evmAnteDepGenerator sdk.AnteDepGenerator) *EVMRouterDecorator {
	return &EVMRouterDecorator{
		defaultAnteHandler:      defaultAnteHandler,
		evmAnteHandler:          evmAnteHandler,
		defaultAnteDepGenerator: defaultAnteDepGenerator,
		evmAnteDepGenerator:     evmAnteDepGenerator,
	}
}

func (r EVMRouterDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	if isEVM, err := r.isEVMMessage(tx); err != nil {
		return ctx, err
	} else if isEVM {
		return r.evmAnteHandler(ctx, tx, simulate)
	}
	return r.defaultAnteHandler(ctx, tx, simulate)
}

func (r EVMRouterDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	if isEVM, err := r.isEVMMessage(tx); err != nil {
		return nil, err
	} else if isEVM {
		return r.evmAnteDepGenerator(txDeps, tx, txIndex)
	}
	return r.defaultAnteDepGenerator(txDeps, tx, txIndex)
}

func (r EVMRouterDecorator) isEVMMessage(tx sdk.Tx) (bool, error) {
	hasEVMMsg := false
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *types.MsgEVMTransaction:
			hasEVMMsg = true
		default:
			continue
		}
	}

	if hasEVMMsg && len(tx.GetMsgs()) != 1 {
		return false, errors.New("EVM tx must have exactly one message")
	}

	return hasEVMMsg, nil
}
