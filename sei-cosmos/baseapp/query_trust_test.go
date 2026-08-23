package baseapp

import (
	"net"
	"testing"

	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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

	ctx := app.enrichABCIQueryContext(t.Context(), sdk.Context{})
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
	require.False(t, ctx.PaginationLimits().Enforce)
}

func TestEnrichABCIQueryContextKillSwitch(t *testing.T) {
	app := &BaseApp{
		queryConfig: srvconfig.QueryConfig{
			DisableLimits: true,
		},
		trustedOriginMatcher: newTrustedCIDRMatcher(nil),
	}

	ctx := app.enrichABCIQueryContext(t.Context(), sdk.Context{})
	require.False(t, ctx.PaginationLimits().Enforce)
}

func TestStripHostPort(t *testing.T) {
	require.Equal(t, "127.0.0.1", stripHostPort("127.0.0.1:9090"))
	require.Equal(t, "2001:db8::1", stripHostPort("[2001:db8::1]:9090"))
	require.Equal(t, "203.0.113.1", stripHostPort("203.0.113.1"))
}
