package tx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gogo/gateway"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
)

// TestSimulateIsNotServed pins that the tx service has no Simulate method. The gRPC
// server and the ABCI query router both register the service from the same descriptor,
// so a gRPC or ABCI query for it gets an unknown-method error, and the REST route that
// wallets post simulations to answers 404.
func TestSimulateIsNotServed(t *testing.T) {
	srv := txServer{}

	grpcSrv := grpc.NewServer()
	txtypes.RegisterServiceServer(grpcSrv, srv)
	methods := grpcSrv.GetServiceInfo()["cosmos.tx.v1beta1.Service"].Methods
	require.NotEmpty(t, methods)
	for _, m := range methods {
		require.NotEqual(t, "Simulate", m.Name)
	}

	mux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &gateway.JSONPb{OrigName: true}))
	require.NoError(t, txtypes.RegisterServiceHandlerServer(t.Context(), mux, srv))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/cosmos/tx/v1beta1/simulate", strings.NewReader(`{"tx_bytes":"dHg="}`)))
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}
