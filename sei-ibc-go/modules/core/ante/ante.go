package ante

import (
	"strings"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	coretypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

// AnteDecorator rejects retired IBC messages before they enter the mempool.
type AnteDecorator struct{}

func NewAnteDecorator() AnteDecorator {
	return AnteDecorator{}
}

// RejectMessages returns the retirement error when a transaction contains an IBC message.
func RejectMessages(tx sdk.Tx) error {
	for _, msg := range tx.GetMsgs() {
		if strings.HasPrefix(sdk.MsgTypeURL(msg), "/ibc.") {
			return coretypes.ErrIBCDeprecated
		}
	}
	return nil
}

func (AnteDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := RejectMessages(tx); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}
