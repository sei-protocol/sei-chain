package tx

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
)

// TestSimulate_DeadlineExceededIsReportedAsSuch pins that a deadline aborting
// BaseApp.Simulate reaches the client as codes.DeadlineExceeded rather than
// being folded into the generic codes.Unknown every other simulate error
// gets, so a caller can tell its request was bounded by time.
func TestSimulate_DeadlineExceededIsReportedAsSuch(t *testing.T) {
	srv := txServer{
		simulate: func(ctx context.Context, txBytes []byte) (sdk.GasInfo, *sdk.Result, error) {
			return sdk.GasInfo{}, nil, context.DeadlineExceeded
		},
	}

	_, err := srv.Simulate(t.Context(), &txtypes.SimulateRequest{TxBytes: []byte("tx")})
	require.Error(t, err)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

// TestSimulate_OtherErrorsStayUnknown pins the existing behavior for every
// non-deadline simulate error, so the new deadline branch does not widen what
// codes.Unknown covers.
func TestSimulate_OtherErrorsStayUnknown(t *testing.T) {
	srv := txServer{
		simulate: func(ctx context.Context, txBytes []byte) (sdk.GasInfo, *sdk.Result, error) {
			return sdk.GasInfo{}, nil, errors.New("boom")
		},
	}

	_, err := srv.Simulate(t.Context(), &txtypes.SimulateRequest{TxBytes: []byte("tx")})
	require.Error(t, err)
	require.Equal(t, codes.Unknown, status.Code(err))
}
