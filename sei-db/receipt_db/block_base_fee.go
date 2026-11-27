package receiptdb

import (
	"context"
	"math/big"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// blockBaseFeeKey is the context key WithBlockBaseFee stores a base fee under.
type blockBaseFeeKey struct{}

// WithBlockBaseFee returns ctx carrying baseFee, the base fee the block whose receipts ctx writes
// ran at.
func WithBlockBaseFee(ctx sdk.Context, baseFee *big.Int) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), blockBaseFeeKey{}, baseFee))
}

// BlockBaseFee returns the base fee WithBlockBaseFee put on ctx, or nil when it holds none.
func BlockBaseFee(ctx sdk.Context) *big.Int {
	baseFee, _ := ctx.Context().Value(blockBaseFeeKey{}).(*big.Int)
	return baseFee
}
