package baseapp

import (
	"context"
	"net"
	"testing"

	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/peer"
)

func TestTrustedCIDRMatcher(t *testing.T) {
	matcher := newTrustedCIDRMatcher([]string{"10.0.0.0/8", "203.0.113.0/24"})

	require.True(t, matcher.contains("10.1.2.3:1234"))
	require.True(t, matcher.contains("203.0.113.50"))
	require.False(t, matcher.contains("192.168.1.1"))
	require.False(t, matcher.contains("not-an-ip"))
}

func TestEnrichABCIQueryContextUntrusted(t *testing.T) {
	app := &BaseApp{
		queryConfig: srvconfig.QueryConfig{
			MaxLimit:      1000,
			MaxOffset:     10_000,
			MaxIterations: 10_000,
		},
		trustedOriginMatcher: newTrustedCIDRMatcher(nil),
	}

	callerCtx := context.WithValue(t.Context(), queryContextTestKey{}, "untrusted")
	ctx := app.enrichABCIQueryContext(callerCtx, sdk.Context{})
	require.Same(t, callerCtx, ctx.Context())
	require.True(t, ctx.IsABCIQuery())
	require.True(t, ctx.PaginationLimits().Enforce)
	require.Equal(t, uint64(1000), ctx.PaginationLimits().MaxLimit)
}

func TestEnrichABCIQueryContextTrusted(t *testing.T) {
	app := &BaseApp{
		queryConfig:          srvconfig.DefaultQueryConfig(),
		trustedOriginMatcher: newTrustedCIDRMatcher([]string{"127.0.0.0/8"}),
	}

	pctx := peer.NewContext(t.Context(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9090}})
	ctx := app.enrichABCIQueryContext(pctx, sdk.Context{})
	require.Same(t, pctx, ctx.Context())
	require.False(t, ctx.PaginationLimits().Enforce)
}

func TestEnrichABCIQueryContextKillSwitch(t *testing.T) {
	app := &BaseApp{
		queryConfig: srvconfig.QueryConfig{
			DisableLimits: true,
		},
		trustedOriginMatcher: newTrustedCIDRMatcher(nil),
	}

	callerCtx := context.WithValue(t.Context(), queryContextTestKey{}, "kill-switch")
	ctx := app.enrichABCIQueryContext(callerCtx, sdk.Context{})
	require.Same(t, callerCtx, ctx.Context())
	require.False(t, ctx.PaginationLimits().Enforce)
}

type queryContextTestKey struct{}

type abciContextQuery struct {
	testdata.QueryImpl
	ctx *context.Context
}

func (q abciContextQuery) Echo(ctx context.Context, req *testdata.EchoRequest) (*testdata.EchoResponse, error) {
	*q.ctx = sdk.UnwrapSDKContext(ctx).Context()
	return q.QueryImpl.Echo(ctx, req)
}

func TestStripHostPort(t *testing.T) {
	require.Equal(t, "127.0.0.1", stripHostPort("127.0.0.1:9090"))
	require.Equal(t, "2001:db8::1", stripHostPort("[2001:db8::1]:9090"))
	require.Equal(t, "203.0.113.1", stripHostPort("203.0.113.1"))
}

func TestCustomQueryEnrichesABCIContext(t *testing.T) {
	var captured sdk.Context
	querierOpt := func(bapp *BaseApp) {
		bapp.QueryRouter().AddRoute("capture", func(ctx sdk.Context, _ []string, _ abci.RequestQuery) ([]byte, error) {
			captured = ctx
			return []byte("ok"), nil
		})
	}

	app := setupBaseApp(t, querierOpt)
	app.InitChain(&abci.RequestInitChain{})

	callerCtx := context.WithValue(t.Context(), queryContextTestKey{}, "custom-query")
	res, err := app.Query(callerCtx, &abci.RequestQuery{
		Path: "/custom/capture/test",
	})
	require.NoError(t, err)
	require.Equal(t, []byte("ok"), res.Value)
	require.Same(t, callerCtx, captured.Context())
	require.True(t, captured.IsABCIQuery())
	require.True(t, captured.PaginationLimits().Enforce)
	require.Equal(t, srvconfig.DefaultQueryMaxLimit, captured.PaginationLimits().MaxLimit)
}

func TestGRPCABCIQueryEnrichesSDKContext(t *testing.T) {
	var captured context.Context
	grpcQueryOpt := func(app *BaseApp) {
		testdata.RegisterQueryServer(app.GRPCQueryRouter(), abciContextQuery{ctx: &captured})
	}

	app := setupBaseApp(t, grpcQueryOpt)
	app.InitChain(&abci.RequestInitChain{})
	reqBytes, err := (&testdata.EchoRequest{Message: "hello"}).Marshal()
	require.NoError(t, err)

	callerCtx := context.WithValue(t.Context(), queryContextTestKey{}, "grpc-abci-query")
	res, err := app.Query(callerCtx, &abci.RequestQuery{
		Path: "/testdata.Query/Echo",
		Data: reqBytes,
	})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.Code)
	require.Same(t, callerCtx, captured)
}
