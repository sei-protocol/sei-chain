package utils_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

func TestAtomicWatch(t *testing.T) {
	ctx := t.Context()
	v := 5
	w := utils.NewAtomicWatch(&v)
	require.Equal(t, 5, *w.Load())

	want := 10
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			for i := 0; i <= want; i++ {
				w.Store(&i)
			}
			return nil
		})

		got, err := w.Wait(ctx, func(v *int) bool { return *v >= want })
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
