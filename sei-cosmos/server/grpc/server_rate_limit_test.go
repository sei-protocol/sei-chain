package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/api"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc/gogoreflection"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	moduletestutil "github.com/sei-protocol/sei-chain/sei-cosmos/types/module/testutil"
)

// startGRPCServerApp is the minimal types.Application StartGRPCServer needs: a
// BaseApp carrying the query router, plus stubs for the bootstrap hooks the
// gRPC path never calls.
type startGRPCServerApp struct {
	*baseapp.BaseApp
}

func (startGRPCServerApp) RegisterAPIRoutes(*api.Server, config.APIConfig)           {}
func (startGRPCServerApp) RegisterLocalServices(client.LocalClient, client.TxConfig) {}
func (startGRPCServerApp) InplaceTestnetInitialize(cryptotypes.PubKey)               {}

// startRateLimitedGRPCServer starts a real server through StartGRPCServer with
// cfg's rate-limit settings and returns a client dialled to it.
func startRateLimitedGRPCServer(t *testing.T, cfg config.GRPCConfig) testdata.QueryClient {
	t.Helper()

	srv, addr := startGRPCServerOnFreePort(t, cfg)
	t.Cleanup(srv.Stop)

	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return testdata.NewQueryClient(conn)
}

func startGRPCServerOnFreePort(t *testing.T, cfg config.GRPCConfig) (*grpc.Server, string) {
	t.Helper()

	app := baseapp.NewBaseApp(t.Name(), dbm.NewMemDB(), nil, nil, &testutil.TestAppOpts{})
	app.MountStores(sdk.NewKVStoreKey("test"))
	require.NoError(t, app.LoadLatestVersion())

	encCfg := moduletestutil.MakeTestEncodingConfig()
	testdata.RegisterInterfaces(encCfg.InterfaceRegistry)
	app.SetInterfaceRegistry(encCfg.InterfaceRegistry)
	testdata.RegisterQueryServer(app.GRPCQueryRouter(), testdata.QueryImpl{})

	clientCtx := client.Context{}.
		WithChainID("test-chain").
		WithTxConfig(encCfg.TxConfig).
		WithInterfaceRegistry(encCfg.InterfaceRegistry)

	cfg.Address = freeTCPAddr(t)
	srv, err := StartGRPCServer(clientCtx, startGRPCServerApp{app}, cfg)
	require.NoError(t, err)

	return srv, cfg.Address
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func rateLimitedGRPCConfig(rps float64, burst int) config.GRPCConfig {
	return config.GRPCConfig{
		Enable:              true,
		RateLimitingEnabled: true,
		IPRateLimitRPS:      rps,
		IPRateLimitBurst:    burst,
	}
}

// TestStartGRPCServer_RateLimitingEnabledInstallsInterceptors covers the wiring
// in StartGRPCServer itself: without the interceptor options it appends, a
// second query from the same IP would be served rather than rejected.
func TestStartGRPCServer_RateLimitingEnabledInstallsInterceptors(t *testing.T) {
	reader := collectRejectionMetrics(t)
	client := startRateLimitedGRPCServer(t, rateLimitedGRPCConfig(0.001, 1))

	res, err := client.Echo(context.Background(), &testdata.EchoRequest{Message: "hello"})
	require.NoError(t, err)
	require.Equal(t, "hello", res.Message)

	_, err = client.Echo(context.Background(), &testdata.EchoRequest{Message: "hello"})
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))

	// StartGRPCServer hands the registry its registered methods, so the
	// rejection is labelled with the method rather than "other".
	require.Contains(t, rejectionMethodLabels(t, reader), "testdata.Query/Echo")
}

func collectRejectionMetrics(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	return reader
}

func rejectionMethodLabels(t *testing.T, reader *sdkmetric.ManualReader) []string {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	labels := []string{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "rpc_rate_limit_rejected_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if v, ok := dp.Attributes.Value(attribute.Key("method_namespace")); ok {
					labels = append(labels, v.AsString())
				}
			}
		}
	}
	return labels
}

// TestStartGRPCServer_RateLimitingDisabledSkipsInterceptors pins the master
// switch: a bucket that would reject the second query never sees it.
func TestStartGRPCServer_RateLimitingDisabledSkipsInterceptors(t *testing.T) {
	cfg := rateLimitedGRPCConfig(0.001, 1)
	cfg.RateLimitingEnabled = false
	client := startRateLimitedGRPCServer(t, cfg)

	for i := 0; i < 3; i++ {
		res, err := client.Echo(context.Background(), &testdata.EchoRequest{Message: "hello"})
		require.NoError(t, err)
		require.Equal(t, "hello", res.Message)
	}
}

// TestStartGRPCServer_MalformedTrustedProxyCIDR pins that a bad CIDR fails
// startup rather than silently starting an unprotected server.
func TestStartGRPCServer_MalformedTrustedProxyCIDR(t *testing.T) {
	cfg := rateLimitedGRPCConfig(100, 100)
	cfg.TrustedProxyCIDRs = []string{"not-a-cidr"}
	cfg.Address = freeTCPAddr(t)

	_, err := StartGRPCServer(client.Context{}, startGRPCServerApp{}, cfg)
	require.ErrorContains(t, err, "grpc rate limiter")
}

func TestRegisteredMethods(t *testing.T) {
	srv := grpc.NewServer()
	testdata.RegisterQueryServer(srv, testdata.QueryImpl{})
	gogoreflection.Register(srv)

	methods := registeredMethods(srv)
	require.Contains(t, methods, "testdata.Query/Echo")
	require.Contains(t, methods, "grpc.reflection.v1.ServerReflection/ServerReflectionInfo")
}
