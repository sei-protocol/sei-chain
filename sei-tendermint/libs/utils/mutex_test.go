package utils_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

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
