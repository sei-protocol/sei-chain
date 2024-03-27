package antedecorators

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type AuthzNestedMessageDecorator struct{}

func NewAuthzNestedMessageDecorator() AuthzNestedMessageDecorator {
	return AuthzNestedMessageDecorator{}
}

func (ad AuthzNestedMessageDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *authz.MsgExec:
			// find nested evm messages
			containsEvm, err := ad.CheckAuthzContainsEvm(ctx, m)
			if err != nil {
				return ctx, err
			}
			if containsEvm {
				return ctx, errors.New("permission denied, authz tx contains evm message")
			}
		default:
			continue
		}
	}

	return next(ctx, tx, simulate)
}

func (ad AuthzNestedMessageDecorator) CheckAuthzContainsEvm(ctx sdk.Context, authzMsg *authz.MsgExec) (bool, error) {
	msgs, err := authzMsg.GetMessages()
	if err != nil {
		return false, err
	}
	for _, msg := range msgs {
		// check if message type is authz exec or evm
		switch m := msg.(type) {
		case *evmtypes.MsgEVMTransaction:
			return true, nil
		case *authz.MsgExec:
			// find nested to check for evm
			valid, err := ad.CheckAuthzContainsEvm(ctx, m)

			if err != nil {
				return false, err
			}

			if valid {
				return true, nil
			}
		default:
			continue
		}
	}
	return false, nil
}
