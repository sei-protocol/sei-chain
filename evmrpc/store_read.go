package evmrpc

import (
	"context"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func withRequestContext(ctx context.Context, ctxProvider func(int64) sdk.Context) func(int64) sdk.Context {
	return func(height int64) sdk.Context {
		return ctxProvider(height).WithContext(ctx)
	}
}

func readStores[T any](
	ctx context.Context,
	ctxProvider func(int64) sdk.Context,
	read func(func(int64) sdk.Context) (T, error),
) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	result, err := read(withRequestContext(ctx, ctxProvider))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return zero, ctxErr
	}
	return result, err
}

// readStoreAtHeight runs a store read in a height-specific SDK context carrying
// the request's cancellation and deadline.
func readStoreAtHeight[T any](
	ctx context.Context,
	height int64,
	ctxProvider func(int64) sdk.Context,
	read func(sdk.Context) (T, error),
) (T, error) {
	return readStores(ctx, ctxProvider, func(ctxProvider func(int64) sdk.Context) (T, error) {
		var zero T
		sdkCtx, err := ctxAtHeight(ctxProvider, height)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return zero, ctxErr
		}
		if err != nil {
			return zero, err
		}
		return read(sdkCtx)
	})
}
