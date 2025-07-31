package utils_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-stream/pkg/require"
	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/utils"
)

func TestAtomicWatch(t *testing.T) {
	ctx := t.Context()
	v := 5
	w := utils.NewAtomicWatch(&v)
	require.Equal(t, 5, *w.Load())

	want := 10
	if err := service.Run(ctx, func(ctx context.Context, s service.Scope) error {
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
