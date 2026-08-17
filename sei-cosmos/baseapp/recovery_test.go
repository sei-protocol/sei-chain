package baseapp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/stretchr/testify/require"
)

// Test that recovery chain produces expected error at specific middleware layer
func TestRecoveryChain(t *testing.T) {
	createError := func(id int) error {
		return fmt.Errorf("error from id: %d", id)
	}

	createHandler := func(id int, handle bool) RecoveryHandler {
		return func(_ interface{}) error {
			if handle {
				return createError(id)
			}
			return nil
		}
	}

	// check recovery chain [1] -> 2 -> 3
	{
		mw := newRecoveryMiddleware(createHandler(3, false), nil)
		mw = newRecoveryMiddleware(createHandler(2, false), mw)
		mw = newRecoveryMiddleware(createHandler(1, true), mw)
		receivedErr := processRecovery(nil, mw)

		require.Equal(t, createError(1), receivedErr)
	}

	// check recovery chain 1 -> [2] -> 3
	{
		mw := newRecoveryMiddleware(createHandler(3, false), nil)
		mw = newRecoveryMiddleware(createHandler(2, true), mw)
		mw = newRecoveryMiddleware(createHandler(1, false), mw)
		receivedErr := processRecovery(nil, mw)

		require.Equal(t, createError(2), receivedErr)
	}

	// check recovery chain 1 -> 2 -> [3]
	{
		mw := newRecoveryMiddleware(createHandler(3, true), nil)
		mw = newRecoveryMiddleware(createHandler(2, false), mw)
		mw = newRecoveryMiddleware(createHandler(1, false), mw)
		receivedErr := processRecovery(nil, mw)

		require.Equal(t, createError(3), receivedErr)
	}

	// check recovery chain 1 -> 2 -> 3
	{
		mw := newRecoveryMiddleware(createHandler(3, false), nil)
		mw = newRecoveryMiddleware(createHandler(2, false), mw)
		mw = newRecoveryMiddleware(createHandler(1, false), mw)
		receivedErr := processRecovery(nil, mw)

		require.Nil(t, receivedErr)
	}
}

func TestContextCancelledRecoveryOnlyWhenContextExpired(t *testing.T) {
	defaultMW := newDefaultRecoveryMiddleware()

	live := sdk.Context{}.WithContext(context.Background())
	liveMW := newContextCancelledRecoveryMiddleware(live, defaultMW)
	err := processRecovery(context.Canceled, liveMW)
	require.ErrorIs(t, err, sdkerrors.ErrPanic)
	err = processRecovery(fmt.Errorf("db: %w", context.DeadlineExceeded), liveMW)
	require.ErrorIs(t, err, sdkerrors.ErrPanic)

	zeroMW := newContextCancelledRecoveryMiddleware(sdk.Context{}, defaultMW)
	err = processRecovery(context.Canceled, zeroMW)
	require.ErrorIs(t, err, sdkerrors.ErrPanic)

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	expiredMW := newContextCancelledRecoveryMiddleware(sdk.Context{}.WithContext(expired), defaultMW)
	err = processRecovery(context.Canceled, expiredMW)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, errors.Is(err, sdkerrors.ErrPanic))

	err = processRecovery(errors.New("unrelated panic"), expiredMW)
	require.ErrorIs(t, err, sdkerrors.ErrPanic)
}
