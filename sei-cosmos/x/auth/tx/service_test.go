package tx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gogo/gateway"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
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

func TestSimulateGatewayAcceptsOnlyTxBytes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "tx_bytes", body: `{"tx_bytes":"dHg="}`, wantStatus: http.StatusOK},
		{name: "tx", body: `{"tx":{"body":{}}}`, wantStatus: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var simulated int
			srv := txServer{
				simulate: func(context.Context, []byte) (sdk.GasInfo, *sdk.Result, error) {
					simulated++
					return sdk.GasInfo{}, &sdk.Result{}, nil
				},
			}
			mux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &gateway.JSONPb{OrigName: true}))
			require.NoError(t, txtypes.RegisterServiceHandlerServer(t.Context(), mux, srv))

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/cosmos/tx/v1beta1/simulate", strings.NewReader(tc.body)))
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			require.Equal(t, rec.Code == http.StatusOK, simulated == 1)
		})
	}
}
