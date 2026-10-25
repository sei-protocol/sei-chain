package antedecorators

import (
	"math"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

const (
	EVMAssociatePriority = math.MaxInt64 - 101
	// This is the max priority a non-associate tx can take.
	MaxPriority = math.MaxInt64 - 1000
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

// AnteHandle caps transaction priority below the associate transaction tier.
func (pd PriorityDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	priority := intMin(ctx.Priority(), MaxPriority)
	newCtx := ctx.WithPriority(priority)

	return next(newCtx, tx, simulate)
}
