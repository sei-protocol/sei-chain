package utils

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type notAbortingError struct{}

func (notAbortingError) Error() string      { return "not aborting" }
func (notAbortingError) IsAbortError() bool { return false }

func TestShouldExecutionAbort(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "validation failed", err: ErrValidationFailed, want: true},
		{name: "execution failed", err: ErrExecutionFailed, want: true},
		{name: "wrapped abort error", err: fmt.Errorf("apply: %w", ErrValidationFailed), want: true},
		{name: "joined abort error", err: errors.Join(errors.New("boom"), ErrExecutionFailed), want: true},
		{name: "abort error reporting false", err: notAbortingError{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ShouldExecutionAbort(tt.err))
		})
	}
}
