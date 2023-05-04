package antedecorators

import (
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type PriorityDecorator struct{}

func NewPriorityDecorator() PriorityDecorator {
	return PriorityDecorator{}
}

func intMin(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// Assigns higher priority to certain types of transactions including oracle
func (pd PriorityDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// Cap priority to MAXINT64 - 1000
	// Use higher priorities for tiers including oracle tx's
	priority := intMin(ctx.Priority(), math.MaxInt64-1000)

	if isOracleTx(tx) {
		priority = math.MaxInt64 - 100
	}

	newCtx := ctx.WithPriority(priority)

	return next(newCtx, tx, simulate)
}

func isOracleTx(tx sdk.Tx) bool {
	if len(tx.GetMsgs()) == 0 {
		// empty TX isn't oracle
		return false
	}
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *oracletypes.MsgAggregateExchangeRateVote:
			continue
		default:
			return false
		}
	}
	return true
}
