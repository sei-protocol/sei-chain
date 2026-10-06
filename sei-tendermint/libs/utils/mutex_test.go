package utils_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestWaitAny(t *testing.T) {
	ctx := t.Context()
	nums := utils.NewAtomicSend(0)
	strs := utils.NewAtomicSend("")
	flags := utils.NewAtomicSend(false)
	recvNums, recvStrs, recvFlags := nums.Subscribe(), strs.Subscribe(), flags.Subscribe()

	require.NoError(t, utils.WaitAny(ctx, func() bool { return true }, recvNums, recvStrs, recvFlags))

	for _, store := range []func(){
		func() { nums.Store(1) },
		func() { strs.Store("go") },
		func() { flags.Store(true) },
	} {
		require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.SpawnBg(func() error {
				return utils.WaitAny(ctx, func() bool {
					return recvNums.Load() == 1 || recvStrs.Load() == "go" || recvFlags.Load()
				}, recvNums, recvStrs, recvFlags)
			})
			store()
			return nil
		}))
		nums.Store(0)
		strs.Store("")
		flags.Store(false)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.Equal(t, context.Canceled, utils.WaitAny(canceled, func() bool { return false }, recvNums, recvStrs, recvFlags))
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
