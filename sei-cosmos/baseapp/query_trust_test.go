package baseapp

import (
	"net"
	"testing"

	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/peer"
)

func TestTrustedCIDRMatcher(t *testing.T) {
	matcher := newTrustedCIDRMatcher([]string{"127.0.0.1/32", "10.0.0.0/8"})
	require.True(t, matcher.contains("127.0.0.1:54321"))
	require.True(t, matcher.contains("10.1.2.3"))
	require.False(t, matcher.contains("203.0.113.1"))
}

func TestTrustedCIDRMatcherSkipsInvalidEntries(t *testing.T) {
	matcher := newTrustedCIDRMatcher([]string{"not-a-cidr", "127.0.0.1/32"})
	require.True(t, matcher.contains("127.0.0.1"))
	require.False(t, matcher.contains("203.0.113.1"))
}

func TestQueryOriginIPFromGRPCPeer(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.1:9090")
	require.NoError(t, err)
	ctx := peer.NewContext(t.Context(), &peer.Peer{Addr: addr})
	require.Equal(t, "192.0.2.1:9090", queryOriginIP(ctx))
}

func TestValidateQueryConfigWarnsOnBroadCIDR(t *testing.T) {
	warnings := srvconfig.ValidateQueryConfig(srvconfig.QueryConfig{
		TrustedCIDRs: []string{"0.0.0.0/0"},
	})
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "overly broad")
}

func TestStripHostPort(t *testing.T) {
	require.Equal(t, "127.0.0.1", stripHostPort("127.0.0.1:9090"))
	require.Equal(t, "2001:db8::1", stripHostPort("[2001:db8::1]:9090"))
}

func TestEnrichABCIQueryContextTrustedOriginUnlimitedScan(t *testing.T) {
	app := newTestBaseApp(t, configtest.AppOpts{
		FlagChainID:               "sei-test",
		FlagQueryTrustedCIDRs:     []string{"127.0.0.1/32"},
		FlagQueryTrustedScanLimit: uint64(0),
	})

	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:9090")
	require.NoError(t, err)
	grpcCtx := peer.NewContext(t.Context(), &peer.Peer{Addr: addr})

	sdkCtx := app.enrichABCIQueryContext(grpcCtx, sdk.Context{})
	require.True(t, sdkCtx.IsABCIQuery())
	require.True(t, sdkCtx.IsTrustedQueryOrigin())
	require.False(t, sdkCtx.EnforceQueryScanLimit())
}

func TestEnrichABCIQueryContextTrustedOriginUsesConfiguredLimit(t *testing.T) {
	const trustedLimit = uint64(250_000)

	app := newTestBaseApp(t, configtest.AppOpts{
		FlagChainID:               "sei-test",
		FlagQueryTrustedCIDRs:     []string{"10.0.0.0/8"},
		FlagQueryTrustedScanLimit: trustedLimit,
	})

	addr, err := net.ResolveTCPAddr("tcp", "10.1.2.3:9090")
	require.NoError(t, err)
	grpcCtx := peer.NewContext(t.Context(), &peer.Peer{Addr: addr})

	sdkCtx := app.enrichABCIQueryContext(grpcCtx, sdk.Context{})
	require.True(t, sdkCtx.IsTrustedQueryOrigin())
	require.True(t, sdkCtx.EnforceQueryScanLimit())
	require.Equal(t, trustedLimit, sdkCtx.QueryScanLimit())
}
