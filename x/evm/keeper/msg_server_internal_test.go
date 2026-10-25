package keeper

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsContextCancellation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil},
		{name: "other error", err: errors.New("boom")},
		{name: "canceled", err: context.Canceled, want: true},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "wrapped cancellation", err: fmt.Errorf("execute: %w", context.Canceled), want: true},
		{name: "wrapped deadline", err: fmt.Errorf("execute: %w", context.DeadlineExceeded), want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isContextCancellation(tc.err))
		})
	}
}
