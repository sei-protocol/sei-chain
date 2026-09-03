package utils_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestWaitEither(t *testing.T) {
	ctx := t.Context()
	nums := utils.NewAtomicSend(0)
	strs := utils.NewAtomicSend("")
	recvNums, recvStrs := nums.Subscribe(), strs.Subscribe()

	require.NoError(t, utils.WaitEither(ctx, recvNums, recvStrs, func() bool { return true }))

	for _, store := range []func(){
		func() { nums.Store(1) },
		func() { strs.Store("go") },
	} {
		require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.SpawnBg(func() error {
				return utils.WaitEither(ctx, recvNums, recvStrs, func() bool {
					return recvNums.Load() == 1 || recvStrs.Load() == "go"
				})
			})
			store()
			return nil
		}))
		nums.Store(0)
		strs.Store("")
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.Equal(t, context.Canceled, utils.WaitEither(canceled, recvNums, recvStrs, func() bool { return false }))
}

func TestAtomicSend(t *testing.T) {
	ctx := t.Context()
	v := 5
	send := utils.NewAtomicSend(&v)
	recv := send.Subscribe()
	require.Equal(t, 5, *recv.Load())

	want := 10
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			for i := 0; i <= want; i++ {
				send.Store(&i)
			}
			return nil
		})

		got, err := recv.Wait(ctx, func(v *int) bool { return *v >= want })
		if err != nil {
			return err
		}
		if *got != want {
			return fmt.Errorf("got %v, want %v", *got, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
